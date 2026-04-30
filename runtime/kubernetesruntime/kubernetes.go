// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kubernetesruntime

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/context"

	"github.com/cenkalti/backoff"
	"github.com/distribution/reference"
	dockertypes "github.com/docker/docker/api/types"
	dockerimagetypes "github.com/docker/docker/api/types/image"
	dockerclient "github.com/docker/docker/client"
	dockerarchive "github.com/docker/docker/pkg/archive"
	log "github.com/golang/glog"
	"github.com/openconfig/monax"
	kubernetesapps "k8s.io/api/apps/v1"
	kubernetescore "k8s.io/api/core/v1"
	kubernetesmeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	appsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"

	runtimepb "github.com/openconfig/monax/runtime/kubernetesruntime/proto"
)

const (
	logNameFormat = "%s-%s-%s.json" // name-type-timestamp.json
	logTimeFormat = "2006-01-02T15:04:05Z"
)

var (
	errBuildDockerImage                    = errors.New("build Docker image")
	errCreateDockerClient                  = errors.New("create Docker client")
	errCreateKubernetesClient              = errors.New("create Kubernetes client")
	errDecodeKubernetesComponentParameters = errors.New("decode Kubernetes component parameters")
	errDependencyNotStarted                = errors.New("dependency not started")
	errDeploymentHealth                    = errors.New("deployment unhealthy")
	errGetNodeInternalIP                   = errors.New("get node internal IP")
	errInvalidIPAddress                    = errors.New("invalid IP address")
	errInvalidServiceType                  = errors.New("invalid service type")
	errNoImage                             = errors.New("no image")
	errProcessKubeconfig                   = errors.New("process kubeconfig")
	errProcessVars                         = errors.New("process vars")
	errReadDeployment                      = errors.New("read deployment")
	errReadService                         = errors.New("read service")
	errServiceHealth                       = errors.New("service unhealthy")
	errStartDeployment                     = errors.New("start deployment")
	errStartService                        = errors.New("start service")
	errStopDeployment                      = errors.New("stop deployment")
	errStopService                         = errors.New("stop service")
	errUnmarshalService                    = errors.New("unmarshal service")
	errUnmarshalDeployment                 = errors.New("unmarshal deployment")

	permanentDeploymentErrorReasons = []string{
		"ImagePullBackOff",
		"ErrImagePull",
		"ErrImageNeverPull",
	}
)

type kubernetesHandler struct {
	docker  *dockerclient.Client
	kubectl *kubernetes.Clientset

	mu               sync.Mutex
	logDir           string
	serviceType      kubernetescore.ServiceType
	stateByComponent map[*monax.Component]*kubernetesComponent
}

type kubernetesComponent struct {
	parameters *runtimepb.KubernetesComponentParameters
	deployment *kubernetesapps.Deployment
	service    *kubernetescore.Service
	started    bool
}

type kubernetesTargets struct {
	monax.UnimplementedTargets
	handler   *kubernetesHandler
	component *monax.Component
}

func newKubernetesHandler() *kubernetesHandler {
	logDir := path.Join(os.TempDir(), "monax-logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Warningf("Failed to create log directory: %v", err)
	}
	return &kubernetesHandler{
		kubectl:          nil,
		docker:           nil,
		stateByComponent: make(map[*monax.Component]*kubernetesComponent),
		logDir:           logDir,
	}
}

func (h *kubernetesHandler) setParameters(parameters *runtimepb.KubernetesRuntimeParameters) error {
	kubernetesParams := parameters.GetKubernetes()

	const masterURL = "" // no masterURL support for now
	config, err := clientcmd.BuildConfigFromFlags(masterURL, os.ExpandEnv(kubernetesParams.GetKubeconfigPath()))
	if err != nil {
		return fmt.Errorf("%w: %v", errProcessKubeconfig, err)
	}
	h.kubectl, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("%w: %v", errCreateKubernetesClient, err)
	}

	if kubernetesParams.HasDockerHostUrl() {
		h.docker, err = dockerclient.NewClientWithOpts(
			// WithHost will validate the Docker host URL value.
			dockerclient.WithHost(kubernetesParams.GetDockerHostUrl()),
			dockerclient.WithTLSClientConfig(
				filepath.Join(kubernetesParams.GetDockerCertPath(), "ca.pem"),
				filepath.Join(kubernetesParams.GetDockerCertPath(), "cert.pem"),
				filepath.Join(kubernetesParams.GetDockerCertPath(), "key.pem"),
			),
		)
		if err != nil {
			return fmt.Errorf("%w: %v", errCreateDockerClient, err)
		}
	}

	// With the default service type set to NodePort and this being called during
	// Initialize(), no other switch case using Service Type should call Fatal as
	// that can lead to orphaned jobs left running.
	switch kubernetesParams.GetServiceType() {
	case runtimepb.KubernetesHandlerParameters_SERVICE_TYPE_NODE_PORT:
		h.serviceType = kubernetescore.ServiceTypeNodePort
	case runtimepb.KubernetesHandlerParameters_SERVICE_TYPE_LOAD_BALANCER:
		h.serviceType = kubernetescore.ServiceTypeLoadBalancer
	default:
		log.Fatalf("%s: %v", errInvalidServiceType, kubernetesParams.GetServiceType())
		panic("unreachable")
	}

	return nil
}

func (h *kubernetesHandler) state(ctx context.Context, component *monax.Component) *kubernetesComponent {
	state, ok := h.stateByComponent[component]
	if !ok {
		log.FatalContextf(ctx, "Unexpected missing state: component %v", component)
	}
	return state
}

func (h *kubernetesHandler) Initialize(ctx context.Context, component *monax.Component) error {
	parameters := new(runtimepb.KubernetesComponentParameters)
	if err := component.Parameters().UnmarshalTo(parameters); err != nil {
		return fmt.Errorf("%w: %v", errDecodeKubernetesComponentParameters, err)
	}

	deploymentPath := component.ResolvePath(parameters.GetDeploymentPath())
	yamlBytes, err := os.ReadFile(deploymentPath)
	if err != nil {
		return fmt.Errorf("%w: %v", errReadDeployment, err)
	}
	deployment := &kubernetesapps.Deployment{}
	if err := yaml.Unmarshal(yamlBytes, deployment); err != nil {
		return fmt.Errorf("%w: %v", errUnmarshalDeployment, err)
	}

	servicePath := component.ResolvePath(parameters.GetServicePath())
	yamlBytes, err = os.ReadFile(servicePath)
	if err != nil {
		return fmt.Errorf("%w: %v", errReadService, err)
	}
	service := &kubernetescore.Service{}
	if err := yaml.Unmarshal(yamlBytes, service); err != nil {
		return fmt.Errorf("%w: %v", errUnmarshalService, err)
	}
	if h.serviceType != service.Spec.Type {
		log.WarningContextf(ctx, "Overriding service type to Kubernetes Runtime Parameters value: %v", h.serviceType)
	}
	service.Spec.Type = h.serviceType

	if parameters.HasDocker() {
		if h.docker == nil {
			return fmt.Errorf("%w: component %v set a Docker context dir, but no Docker credentials were set in the runtime parameters", errBuildDockerImage, component.ID())
		}
		if err := h.buildDockerImage(ctx, component, parameters.GetDocker(), deployment.Spec.Template.Spec.Containers[0].Image); err != nil {
			return fmt.Errorf("%w: %v", errBuildDockerImage, err)
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.stateByComponent[component] = &kubernetesComponent{
		parameters: parameters,
		deployment: deployment,
		service:    service,
	}

	return nil
}

func (h *kubernetesHandler) Start(ctx context.Context, component *monax.Component) error {
	state := h.state(ctx, component)

	vars, err := monax.ProcessVars(nil, state.parameters.GetVars(), component)
	if err != nil {
		return fmt.Errorf("%w: %v", errProcessVars, err)
	}

	deployment, err := h.startDeployment(ctx, state.deployment, state.parameters, vars)
	if err != nil {
		return fmt.Errorf("%w: %v", errStartDeployment, err)
	}
	state.deployment = deployment

	service, err := h.startService(ctx, state.service, state.parameters)
	if err != nil {
		return fmt.Errorf("%w: %v", errStartService, err)
	}
	state.service = service
	state.started = true

	return nil
}

func (h *kubernetesHandler) startDeployment(ctx context.Context, deployment *kubernetesapps.Deployment, params *runtimepb.KubernetesComponentParameters, vars map[string]string) (*kubernetesapps.Deployment, error) {
	log.InfoContextf(ctx, "Starting deployment %q", deployment.Name)

	configMap := &kubernetescore.ConfigMap{
		Data: vars,
	}
	configMap.SetName(deployment.Name)
	configMap.SetNamespace(deployment.Namespace)
	createdConfigMaps := h.kubectl.CoreV1().ConfigMaps(kubernetescore.NamespaceDefault)
	if _, err := createdConfigMaps.Create(ctx, configMap, kubernetesmeta.CreateOptions{}); err != nil {
		return nil, err
	}
	h.logData(ctx, createdConfigMaps, deployment.Name)

	createdDeployments := h.kubectl.AppsV1().Deployments(kubernetescore.NamespaceDefault)
	result, err := createdDeployments.Create(ctx, deployment, kubernetesmeta.CreateOptions{})
	if err != nil {
		return nil, err
	}
	h.logDeployment(ctx, createdDeployments, deployment.Name)

	if !params.GetWaitForDeployment() {
		return result, nil
	}

	retryPolicy := backoff.NewExponentialBackOff()
	retryPolicy.MaxElapsedTime = time.Duration(params.GetWaitForDeploymentTimeoutSec()) * time.Second
	if err := backoff.Retry(func() error {
		return h.checkDeploymentHealth(ctx, deployment)
	}, retryPolicy); err != nil {
		return nil, err
	}
	return result, nil
}

func (h *kubernetesHandler) checkDeploymentHealth(ctx context.Context, deployment *kubernetesapps.Deployment) error {
	deployment, err := h.kubectl.AppsV1().Deployments(kubernetescore.NamespaceDefault).Get(ctx, deployment.GetName(), kubernetesmeta.GetOptions{})
	if err != nil {
		return fmt.Errorf("%w: %v", errDeploymentHealth, err)
	}

	selector, err := kubernetesmeta.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return fmt.Errorf("%w: getting label selector for deployment %s: %v", errDeploymentHealth, deployment.GetName(), err)
	}
	pods, err := h.kubectl.CoreV1().Pods(kubernetescore.NamespaceDefault).List(ctx, kubernetesmeta.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return fmt.Errorf("%w: listing pods for deployment %s: %v", errDeploymentHealth, deployment.GetName(), err)
	}

	// Check for permanent errors that will prevent the deployment from ever
	// succeeding.
	for _, pod := range pods.Items {
		for _, status := range pod.Status.ContainerStatuses {
			if status.State.Waiting != nil {
				reason := status.State.Waiting.Reason
				if slices.Contains(permanentDeploymentErrorReasons, reason) {
					err := fmt.Errorf("%w: container %s in pod %s has image pull error: %s, message: %s", errDeploymentHealth, status.Name, pod.Name, reason, status.State.Waiting.Message)
					return &backoff.PermanentError{Err: err}
				}
			}
		}
	}

	// Replicas are only "available" after passing "ready" state, a representation
	// of health given pods must pass internal readiness and liveness checks
	// before being considered "ready".
	if deployment.Status.AvailableReplicas != *deployment.Spec.Replicas {
		return fmt.Errorf("%w: got replicas: %v, want replicas %v", errDeploymentHealth, deployment.Status.AvailableReplicas, *deployment.Spec.Replicas)
	}
	return nil
}

func (h *kubernetesHandler) startService(ctx context.Context, service *kubernetescore.Service, params *runtimepb.KubernetesComponentParameters) (*kubernetescore.Service, error) {
	log.InfoContextf(ctx, "Creating service: %s", service.Name)

	result, err := h.kubectl.CoreV1().Services(kubernetescore.NamespaceDefault).Create(ctx, service, kubernetesmeta.CreateOptions{})
	if err != nil {
		return nil, err
	}

	if !params.GetWaitForService() {
		return result, nil
	}

	if err := h.waitForServiceUp(ctx, result, params); err != nil {
		return nil, err
	}

	return result, nil
}

func (h *kubernetesHandler) Stop(ctx context.Context, component *monax.Component) error {
	state := h.state(ctx, component)

	var stopDeploymentErr, stopServiceErr error
	if err := h.stopDeployment(ctx, state.deployment); err != nil {
		stopDeploymentErr = fmt.Errorf("%w: %v", errStopDeployment, err)
	}
	state.deployment.ResourceVersion = ""

	if err := h.stopService(ctx, state.service); err != nil {
		stopServiceErr = fmt.Errorf("%w: %v", errStopService, err)
	}

	// Reset the state so that the component can be restarted.
	state.service.ResourceVersion = ""
	state.started = false

	// Handle errors.
	if stopDeploymentErr != nil && stopServiceErr != nil {
		// This gives err.Unwrap() if the error needs to be separated later.
		return fmt.Errorf("%w | %w", stopDeploymentErr, stopServiceErr)
	}
	if stopDeploymentErr != nil {
		return stopDeploymentErr
	}
	return stopServiceErr
}

func (h *kubernetesHandler) stopDeployment(ctx context.Context, deployment *kubernetesapps.Deployment) error {
	log.InfoContextf(ctx, "Deleting deployment %s", deployment.Name)
	if err := h.kubectl.CoreV1().ConfigMaps(kubernetescore.NamespaceDefault).Delete(ctx, deployment.Name, kubernetesmeta.DeleteOptions{}); err != nil {
		return err
	}
	return h.kubectl.AppsV1().Deployments(kubernetescore.NamespaceDefault).Delete(ctx, deployment.Name, kubernetesmeta.DeleteOptions{})
}

func (h *kubernetesHandler) stopService(ctx context.Context, service *kubernetescore.Service) error {
	log.InfoContextf(ctx, "Deleting service %s", service.Name)
	return h.kubectl.CoreV1().Services(kubernetescore.NamespaceDefault).Delete(ctx, service.Name, kubernetesmeta.DeleteOptions{})
}

func (h *kubernetesHandler) Status(ctx context.Context, component *monax.Component) error {
	state := h.state(ctx, component)

	if err := h.checkDeploymentHealth(ctx, state.deployment); err != nil {
		return err
	}
	if err := h.waitForServiceUp(ctx, state.service, state.parameters); err != nil {
		return err
	}

	return nil
}

func (h *kubernetesHandler) Targets(component *monax.Component) monax.Targets {
	return &kubernetesTargets{handler: h, component: component}
}

func (t *kubernetesTargets) DHCP(ctx context.Context, serviceName string) (monax.Target, error) {
	return t.getServiceIP(ctx)
}

func (t *kubernetesTargets) GRPC(ctx context.Context, serviceName string) (monax.Target, error) {
	return t.getServiceIP(ctx)
}

func (t *kubernetesTargets) HTTP(ctx context.Context, serviceName string) (monax.Target, error) {
	return t.http(ctx, false)
}

func (t *kubernetesTargets) HTTPS(ctx context.Context, serviceName string) (monax.Target, error) {
	return t.http(ctx, true)
}

func (t *kubernetesTargets) http(ctx context.Context, useHTTPS bool) (monax.Target, error) {
	ip, err := t.getServiceIP(ctx)
	if err != nil {
		return monax.EmptyTarget, err
	}
	scheme := "http"
	if useHTTPS {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s", scheme, ip)
	return monax.Target(url), nil
}

// To support Kubernetes clusters that do not have a load balancer controller,
// we need to use the internal IP of the node where the pods that the service is
// pointing to are running.
func getNodeInternalIP(ctx context.Context, service *kubernetescore.Service, kubectl kubernetes.Interface) (string, error) {
	serviceName := service.GetName()
	selector := service.Spec.Selector
	if len(selector) == 0 {
		return "", fmt.Errorf("service %s has no selector, cannot find associated pods/nodes", serviceName)
	}

	// Convert the map selector to a label selector string for listing pods.
	var kvp []string
	for key, value := range selector {
		kvp = append(kvp, fmt.Sprintf("%s=%s", key, value))
	}
	labelSelector := strings.Join(kvp, ",")

	// List the pods using the selector. This should only return the pod(s) the
	// service is using.
	pods, err := kubectl.CoreV1().Pods(kubernetescore.NamespaceDefault).List(ctx, kubernetesmeta.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return "", fmt.Errorf("list pods for service %s: %w", serviceName, err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods found for service %s with selector %v", serviceName, selector)
	}

	var nodeNames []string
	// Not likely to have more than one pod for NodePort type services.
	for _, pod := range pods.Items {
		if pod.Spec.NodeName != "" && !slices.Contains(nodeNames, pod.Spec.NodeName) {
			nodeNames = append(nodeNames, pod.Spec.NodeName)
		}
	}
	if len(nodeNames) == 0 {
		return "", fmt.Errorf("no nodes found for service %s with selector %v", serviceName, selector)
	}
	if len(nodeNames) > 1 {
		log.WarningContextf(ctx, "Multiple nodes found for service %s with selector %v.\nUsing only the first name: %s", serviceName, selector, nodeNames[0])
	}
	nodeName := nodeNames[0]

	node, err := kubectl.CoreV1().Nodes().Get(ctx, nodeName, kubernetesmeta.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get node %s: %w", nodeName, err)
	}

	for _, address := range node.Status.Addresses {
		if address.Type == kubernetescore.NodeInternalIP {
			return address.Address, nil
		}
	}

	return "", fmt.Errorf("missing node internal IP")
}

func (t *kubernetesTargets) getServiceIP(ctx context.Context) (monax.Target, error) {
	state := t.handler.state(ctx, t.component)
	if !state.started {
		// The component failed to start so don't wait for it to become healthy.
		return monax.EmptyTarget, fmt.Errorf("%w: component %v is not started", errDependencyNotStarted, t.component)
	}
	if err := t.handler.waitForServiceUp(ctx, state.service, state.parameters); err != nil {
		return monax.EmptyTarget, err
	}

	serviceUpdate, err := t.handler.kubectl.CoreV1().Services(kubernetescore.NamespaceDefault).Get(ctx, state.service.Name, kubernetesmeta.GetOptions{})
	if err != nil {
		return monax.EmptyTarget, fmt.Errorf("%w: %v: %v", errServiceHealth, t.component, err)
	}
	if err := t.handler.checkServiceUp(ctx, serviceUpdate); err != nil {
		return monax.EmptyTarget, err
	}

	var ipAddress string
	var port int32
	switch serviceUpdate.Spec.Type {
	case kubernetescore.ServiceTypeLoadBalancer:
		ipAddress = serviceUpdate.Status.LoadBalancer.Ingress[0].IP
		port = serviceUpdate.Spec.Ports[0].Port
	case kubernetescore.ServiceTypeNodePort:
		ipAddress, err = getNodeInternalIP(ctx, serviceUpdate, t.handler.kubectl)
		if err != nil {
			return monax.EmptyTarget, fmt.Errorf("%w: %v", errGetNodeInternalIP, err)
		}
		port = serviceUpdate.Spec.Ports[0].NodePort
	default:
		return monax.EmptyTarget, fmt.Errorf("%w: %v", errInvalidServiceType, serviceUpdate.Spec.Type)
	}
	finalIP, err := netip.ParseAddr(ipAddress)
	if err != nil {
		return monax.EmptyTarget, fmt.Errorf("%w: %v is not a valid IP address: %v", errInvalidIPAddress, ipAddress, err)
	}
	return monax.Target(netip.AddrPortFrom(finalIP, uint16(port)).String()), nil
}

// waitForServiceUp waits for the service to have an IP address that is usually
// created by the cluster's load balancer controller after the Service object
// is created. This is NOT a representation of actual service health.
func (h *kubernetesHandler) waitForServiceUp(ctx context.Context, service *kubernetescore.Service, params *runtimepb.KubernetesComponentParameters) error {
	retryPolicy := backoff.NewExponentialBackOff()
	retryPolicy.MaxElapsedTime = time.Duration(params.GetWaitForServiceTimeoutSec()) * time.Second
	if err := backoff.Retry(func() error {
		return h.checkServiceUp(ctx, service)
	}, retryPolicy); err != nil {
		return err
	}
	return nil
}

func (h *kubernetesHandler) checkServiceUp(ctx context.Context, service *kubernetescore.Service) error {
	service, err := h.kubectl.CoreV1().Services(kubernetescore.NamespaceDefault).Get(ctx, service.GetName(), kubernetesmeta.GetOptions{})
	if err != nil {
		return fmt.Errorf("%w: %v", errServiceHealth, err)
	}

	switch service.Spec.Type {
	case kubernetescore.ServiceTypeLoadBalancer:
		if len(service.Status.LoadBalancer.Ingress) == 0 {
			return formatServiceError(ctx, service, h.kubectl)
		}
		return nil // Service is considered up.
	case kubernetescore.ServiceTypeNodePort:
		if service.Spec.Ports[0].NodePort == 0 {
			return formatServiceError(ctx, service, h.kubectl)
		}
		return nil // Service is considered up.
	default:
		return &backoff.PermanentError{Err: fmt.Errorf("%w: %w: %v", errServiceHealth, errInvalidServiceType, service.Spec.Type)}
	}
}

func (h *kubernetesHandler) buildDockerImage(ctx context.Context, component *monax.Component, dockerParams *runtimepb.DockerBuildParameters, name string) error {
	if err := h.clearDockerImage(ctx, name); err != nil {
		return err
	}

	tar, err := dockerarchive.TarWithOptions(component.ResolvePath(dockerParams.GetContextPath()), &dockerarchive.TarOptions{})
	if err != nil {
		return err
	}

	response, err := h.docker.ImageBuild(ctx, tar, dockertypes.ImageBuildOptions{
		Dockerfile:  dockerParams.GetDockerfilePath(),
		Tags:        []string{name},
		ForceRemove: true,
	})
	if err != nil {
		return err
	}
	defer response.Body.Close()

	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		log.InfoContext(ctx, scanner.Text())
	}

	if _, err := h.findDockerImage(ctx, name); err != nil {
		return err
	}

	return nil
}

func (h *kubernetesHandler) clearDockerImage(ctx context.Context, name string) error {
	imageID, err := h.findDockerImage(ctx, name)
	if err != nil {
		if errors.Is(err, errNoImage) {
			return nil
		}
		return err
	}

	_, err = h.docker.ImageRemove(ctx, imageID, dockerimagetypes.RemoveOptions{Force: true})
	if err != nil {
		return err
	}

	return nil
}

func (h *kubernetesHandler) findDockerImage(ctx context.Context, wantName string) (string, error) {
	imageList, err := h.docker.ImageList(ctx, dockerimagetypes.ListOptions{})
	if err != nil {
		return "", err
	}

	for _, image := range imageList {
		for _, ref := range image.RepoTags {
			matches := reference.ReferenceRegexp.FindStringSubmatch(ref)
			if matches == nil {
				continue
			}

			name := matches[1]
			if name == wantName {
				return image.ID, nil
			}
		}
	}

	return "", fmt.Errorf("%w: tag %v", errNoImage, wantName)
}

func (h *kubernetesHandler) logData(ctx context.Context, configMaps corev1.ConfigMapInterface, name string) {
	configMapList, err := configMaps.List(ctx, kubernetesmeta.ListOptions{})
	if err != nil {
		log.InfoContextf(ctx, "Failed to list config maps: %v", err)
		return
	}
	if configMapList == nil {
		return
	}

	for _, item := range configMapList.Items {
		if item.Name != name {
			continue
		}
		// Only log the data because the rest is not interesting metadata and
		// field names.
		jsonBytes, err := json.MarshalIndent(item.Data, "", "  ")
		if err != nil {
			log.InfoContextf(ctx, "Failed to marshal config map %q: %v", name, err)
			continue
		}
		log.V(1).InfoContextf(ctx, "Config map %q data:\n%s", item.Name, string(jsonBytes))
		fileName := fmt.Sprintf(logNameFormat, item.Name, "configmap", time.Now().Format(logTimeFormat))
		writeLogFile(ctx, path.Join(h.logDir, fileName), jsonBytes)
	}
}

func (h *kubernetesHandler) logDeployment(ctx context.Context, deployments appsv1.DeploymentInterface, name string) {
	deploymentList, err := deployments.List(ctx, kubernetesmeta.ListOptions{})
	if err != nil {
		log.InfoContextf(ctx, "Failed to list deployments: %v", err)
		return
	}
	if deploymentList == nil {
		return
	}

	for _, item := range deploymentList.Items {
		if item.Name != name {
			continue
		}
		// Only log the spec because status is not useful here and the rest is
		// not interesting metadata and field names.
		jsonBytes, err := json.MarshalIndent(item.Spec, "", "  ")
		if err != nil {
			log.InfoContextf(ctx, "Failed to marshal deployment %q: %v", name, err)
			continue
		}
		log.V(1).InfoContextf(ctx, "Deployment %q spec:\n%s", name, string(jsonBytes))
		fileName := fmt.Sprintf(logNameFormat, item.Name, "deployment", time.Now().Format(logTimeFormat))
		writeLogFile(ctx, path.Join(h.logDir, fileName), jsonBytes)
	}
}

func writeLogFile(ctx context.Context, path string, data []byte) {
	f, err := os.Create(path)
	if err != nil {
		log.WarningContextf(ctx, "Failed to create log file: %v", err)
		return
	}
	defer f.Close()

	// Add a newline to the end of the log file to make it easier to read.
	data = append(data, []byte("\n")...)
	if _, err := f.Write(data); err != nil {
		log.WarningContextf(ctx, "Failed to write log file: %v", err)
		return
	}
	log.InfoContextf(ctx, "Wrote spec file %q", path)
}

// formatServiceError will always return an error. The only difference is that a
// PermanentError will not be retried.
func formatServiceError(ctx context.Context, service *kubernetescore.Service, kubectl kubernetes.Interface) error {
	// Check events for permanent failures, otherwise retry.
	selector := fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=Service,type=Warning", service.GetName())
	events, err := kubectl.CoreV1().Events(kubernetescore.NamespaceDefault).List(ctx, kubernetesmeta.ListOptions{
		FieldSelector: selector,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", errServiceHealth, err)
	}
	for _, event := range events.Items {
		if event.Type == kubernetescore.EventTypeNormal {
			continue
		}
		// "Failed" should cover most permanent Service errors. The rest should
		// get caught during the Deployment check. This works for both NodePort and
		// LoadBalancer type services.
		if strings.Contains(event.Reason, "Failed") {
			err := fmt.Errorf("%w: service %s failed: %s: %s", errServiceHealth, service.GetName(), event.Reason, event.Message)
			return &backoff.PermanentError{Err: err}
		}
	}
	return fmt.Errorf("%w: retryable error for service %s", errServiceHealth, service.GetName())
}
