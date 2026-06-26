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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/context"

	"github.com/cenkalti/backoff"
	"github.com/distribution/reference"
	dockerconfig "github.com/docker/cli/cli/config"
	log "github.com/golang/glog"
	dockerarchive "github.com/moby/go-archive"
	dockerclient "github.com/moby/moby/client"
	"github.com/openconfig/monax"
	kubernetesapps "k8s.io/api/apps/v1"
	kubernetescore "k8s.io/api/core/v1"
	kubernetesmeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	appsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"

	monaxpb "github.com/openconfig/monax/proto"
	runtimepb "github.com/openconfig/monax/runtime/kubernetesruntime/proto"
)

const (
	logNameFormat             = "%s-%s-%s.json" // name-type-timestamp.json
	logTimeFormat             = "2006-01-02T15:04:05Z"
	dockerBuildLogBufferLines = 50
)

var (
	errBuildDockerImage                    = errors.New("build Docker image")
	errCreateKubernetesClient              = errors.New("create Kubernetes client")
	errDecodeKubernetesComponentParameters = errors.New("decode Kubernetes component parameters")
	errDependencyNotStarted                = errors.New("dependency not started")
	errDeploymentHealth                    = errors.New("deployment unhealthy")
	errExtractKindClusterName              = errors.New("extract kind cluster name")
	errGetAuthConfig                       = errors.New("get auth config")
	errGetNodeInternalIP                   = errors.New("get node internal IP")
	errInvalidBorgComponentParameters      = errors.New("validate Borg component parameters")
	errInvalidIPAddress                    = errors.New("invalid IP address")
	errInvalidServiceType                  = errors.New("invalid service type")
	errLoadDockerConfig                    = errors.New("load Docker config")
	errLoadDockerImageToKind               = errors.New("load Docker image into kind cluster")
	errMarshalAuthConfig                   = errors.New("marshal auth config")
	errNoImage                             = errors.New("no image")
	errProcessKubeconfig                   = errors.New("process kubeconfig")
	errProcessVars                         = errors.New("process vars")
	errPushDockerImage                     = errors.New("push Docker image")
	errReadDeployment                      = errors.New("read deployment")
	errReadService                         = errors.New("read service")
	errParseReference                      = errors.New("parse reference")
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

	mu                  sync.Mutex
	logDir              string
	serviceType         kubernetescore.ServiceType
	kubeconfigPath      string
	imageRepositoryAddr string
	stateByComponent    map[*monax.Component]*kubernetesComponent
}

type kubernetesComponent struct {
	parameters          *runtimepb.KubernetesComponentParameters
	deployment          *kubernetesapps.Deployment
	service             *kubernetescore.Service
	started             bool
	interfaceByPortName map[string]*monaxpb.Interface
}

type kubernetesTargets struct {
	monax.UnimplementedTargets
	handler   *kubernetesHandler
	component *monax.Component
}

func newKubernetesHandler() *kubernetesHandler {
	logDir := filepath.Join(os.TempDir(), "monax-logs")
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
	h.kubeconfigPath = kubernetesParams.GetKubeconfigPath()
	h.imageRepositoryAddr = kubernetesParams.GetImageRepositoryAddress()

	h.newDockerClient(kubernetesParams)

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

func (h *kubernetesHandler) newDockerClient(parameters *runtimepb.KubernetesHandlerParameters) {
	// Docker client uses, in priority order: docker_host_url set in
	// KubernetesHandlerParameters, DOCKER_HOST environment variable, or the
	// default value of 'unix:///var/run/docker.sock' for Linux machines.
	if parameters.HasDockerHostUrl() {
		os.Setenv("DOCKER_HOST", parameters.GetDockerHostUrl())
	}
	if parameters.HasDockerCertPath() {
		os.Setenv("DOCKER_CERT_PATH", parameters.GetDockerCertPath())
	}
	cli, err := dockerclient.New(dockerclient.FromEnv)
	if err != nil {
		// Not returning error since it is not known if Docker is needed.
		log.Warningf("Create Docker client: %v", err)
		log.Warningf("Continuing without Docker client")
		return
	}
	h.docker = cli
}

func (h *kubernetesHandler) state(ctx context.Context, component *monax.Component) *kubernetesComponent {
	h.mu.Lock()
	defer h.mu.Unlock()
	state, ok := h.stateByComponent[component]
	if !ok {
		log.FatalContextf(ctx, "Unexpected missing state: component %v", component)
	}
	return state
}

func (h *kubernetesHandler) Initialize(ctx context.Context, component *monax.Component) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	parameters := new(runtimepb.KubernetesComponentParameters)
	if err := component.Parameters().UnmarshalTo(parameters); err != nil {
		return fmt.Errorf("%w: %v", errDecodeKubernetesComponentParameters, err)
	}

	if parameters.GetDocker().GetLoadToKind() && h.imageRepositoryAddr != "" {
		return fmt.Errorf("%w: image repository address is set to %q, but load_to_kind is true. Only one can be set at a time", errInvalidBorgComponentParameters, h.imageRepositoryAddr)
	}

	// Read and update Deployment object.
	deploymentPath := component.ResolvePath(parameters.GetDeploymentPath())
	yamlBytes, err := os.ReadFile(deploymentPath)
	if err != nil {
		return fmt.Errorf("%w: %v", errReadDeployment, err)
	}
	deployment := &kubernetesapps.Deployment{}
	if err := yaml.Unmarshal(yamlBytes, deployment); err != nil {
		return fmt.Errorf("%w: %v", errUnmarshalDeployment, err)
	}

	if parameters.HasDocker() {
		if h.docker == nil {
			// Trying to build a Docker image without a Docker client will panic.
			return fmt.Errorf("%w: Docker client is nil", errBuildDockerImage)
		}
		h.updateDeploymentContainers(ctx, deployment, parameters.GetDocker().GetLoadToKind())

		if err := h.buildAndLoadImage(ctx, component, parameters.GetDocker(), deployment.Spec.Template.Spec.Containers[0].Image); err != nil {
			return fmt.Errorf("%w: %v", errBuildDockerImage, err)
		}
	}

	// Read and update Service object.
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
	h.stateByComponent[component] = &kubernetesComponent{
		parameters:          parameters,
		deployment:          deployment,
		service:             service,
		interfaceByPortName: parameters.GetInterfaceByPortName(),
	}

	return nil
}

func (h *kubernetesHandler) updateDeploymentContainers(ctx context.Context, deployment *kubernetesapps.Deployment, loadToKind bool) {
	imagePullPolicy := kubernetescore.PullAlways
	if loadToKind {
		// `kind` is local and should already have the image.
		imagePullPolicy = kubernetescore.PullNever
	}

	for i := range deployment.Spec.Template.Spec.Containers {
		container := &deployment.Spec.Template.Spec.Containers[i]
		container.ImagePullPolicy = imagePullPolicy

		if h.imageRepositoryAddr == "" {
			continue
		}

		if container.Image == "" {
			// Not likely to happen unless the user has some complicated setup to
			// populate the image field later. Trust they know what they are doing.
			log.WarningContextf(ctx, "Deployment container %q has an empty image", container.Name)
			continue
		}

		if strings.HasPrefix(container.Image, h.imageRepositoryAddr) {
			continue
		}

		if strings.Contains(container.Image, "/") {
			log.WarningContextf(ctx, "Repository address in container %q image %q is already set. Skipping override to %q.", container.Name, container.Image, h.imageRepositoryAddr)
			continue
		}

		container.Image = fmt.Sprintf("%s/%s", strings.TrimSuffix(h.imageRepositoryAddr, "/"), container.Image)
	}
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

func (t *kubernetesTargets) portForInterface(ctx context.Context, intfSelector func(*monaxpb.Interface) bool) string {
	state := t.handler.state(ctx, t.component)
	for port, intf := range state.interfaceByPortName {
		if intfSelector(intf) {
			return port
		}
	}
	return ""
}

func (t *kubernetesTargets) DHCP(ctx context.Context, serviceName string) (monax.Target, error) {
	portName := t.portForInterface(ctx, monax.DHCPSelector(serviceName))
	return t.getServiceIP(ctx, portName)
}

func (t *kubernetesTargets) GRPC(ctx context.Context, serviceName string) (monax.Target, error) {
	portName := t.portForInterface(ctx, monax.GRPCSelector(serviceName))
	return t.getServiceIP(ctx, portName)
}

func (t *kubernetesTargets) HTTP(ctx context.Context, serviceName string) (monax.Target, error) {
	return t.http(ctx, serviceName, false)
}

func (t *kubernetesTargets) HTTPS(ctx context.Context, serviceName string) (monax.Target, error) {
	return t.http(ctx, serviceName, true)
}

func (t *kubernetesTargets) http(ctx context.Context, serviceName string, useHTTPS bool) (monax.Target, error) {
	selector := monax.HTTPSelector(serviceName)
	if useHTTPS {
		selector = monax.HTTPSSelector(serviceName)
	}
	portName := t.portForInterface(ctx, selector)
	ip, err := t.getServiceIP(ctx, portName)
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

func (t *kubernetesTargets) getServiceIP(ctx context.Context, portName string) (monax.Target, error) {
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
		var matchPort int32
		lowerPortName := strings.ToLower(portName)
		for _, p := range serviceUpdate.Spec.Ports {
			if strings.ToLower(p.Name) == lowerPortName {
				matchPort = p.Port
				break
			}
		}
		if matchPort == 0 {
			matchPort = serviceUpdate.Spec.Ports[0].Port
		}
		port = matchPort
	case kubernetescore.ServiceTypeNodePort:
		ipAddress, err = getNodeInternalIP(ctx, serviceUpdate, t.handler.kubectl)
		if err != nil {
			return monax.EmptyTarget, fmt.Errorf("%w: %v", errGetNodeInternalIP, err)
		}
		var matchPort int32
		lowerPortName := strings.ToLower(portName)
		for _, p := range serviceUpdate.Spec.Ports {
			if strings.ToLower(p.Name) == lowerPortName {
				matchPort = p.NodePort
				break
			}
		}
		if matchPort == 0 {
			matchPort = serviceUpdate.Spec.Ports[0].NodePort
		}
		port = matchPort
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
	log.InfoContextf(ctx, "Building Docker image: %s", name)

	workingDir, err := os.Getwd()
	if err != nil {
		return err
	}

	contextPath := filepath.Join(workingDir, component.ResolvePath(dockerParams.GetContextPath()))
	log.InfoContextf(ctx, "Creating tar for Docker build from context path: %s", contextPath)
	tar, err := dockerarchive.TarWithOptions(contextPath, &dockerarchive.TarOptions{})
	if err != nil {
		return err
	}

	buildArgs := make(map[string]*string)
	for _, env := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"} {
		if val, ok := os.LookupEnv(env); ok {
			buildArgs[env] = &val
		}
	}

	// Add a "latest" tag to the image in addition to the one specified in the
	// parameters, because this is the tag that will be used by the kind load
	// command.
	refName, err := reference.WithName(name)
	if err != nil {
		return fmt.Errorf("%w: %v", errParseReference, err)
	}
	latestName, err := reference.WithTag(reference.TrimNamed(refName), "latest")
	if err != nil {
		return err
	}
	// Include the name in case another tag was specified.
	tags := []string{name, latestName.String()}

	response, err := h.docker.ImageBuild(ctx, tar, dockerclient.ImageBuildOptions{
		// This path is relative to the context_path.
		Dockerfile: dockerParams.GetDockerfilePath(),
		Tags:       tags,
		BuildArgs:  buildArgs,
		Remove:     true,
	})
	if err != nil {
		return err
	}
	defer response.Body.Close()

	var lastDockerLines []string
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := scanner.Text()
		log.V(1).InfoContext(ctx, line)
		lastDockerLines = append(lastDockerLines, line)
		if len(lastDockerLines) > dockerBuildLogBufferLines {
			lastDockerLines = lastDockerLines[1:]
		}
	}

	if _, err := h.findDockerImage(ctx, name); err != nil {
		log.ErrorContextf(ctx, "Buffer of last %d lines of Docker build output:\n%s", dockerBuildLogBufferLines, strings.Join(lastDockerLines, "\n"))
		log.ErrorContextf(ctx, "To see full Docker build output, rerun test with `-args -v 1`")
		return err
	}
	log.InfoContextf(ctx, "Docker image build complete: %s", name)

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

	_, err = h.docker.ImageRemove(ctx, imageID, dockerclient.ImageRemoveOptions{Force: true})
	if err != nil {
		return err
	}

	return nil
}

func (h *kubernetesHandler) findDockerImage(ctx context.Context, wantName string) (string, error) {
	imageList, err := h.docker.ImageList(ctx, dockerclient.ImageListOptions{})
	if err != nil {
		return "", err
	}

	for _, image := range imageList.Items {
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
		writeLogFile(ctx, filepath.Join(h.logDir, fileName), jsonBytes)
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
		writeLogFile(ctx, filepath.Join(h.logDir, fileName), jsonBytes)
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

// extractKindClusterName extracts the kind cluster name from the given kubeconfig file.
func extractKindClusterName(kubeconfigPath string) (string, error) {
	config, err := clientcmd.LoadFromFile(os.ExpandEnv(kubeconfigPath))
	if err != nil {
		return "", err
	}
	contextName := config.CurrentContext
	if strings.HasPrefix(contextName, "kind-") {
		return strings.TrimPrefix(contextName, "kind-"), nil
	}
	return contextName, nil
}

// buildAndLoadImage builds a Docker image and optionally loads it to a local kind cluster.
func (h *kubernetesHandler) buildAndLoadImage(ctx context.Context, component *monax.Component, dockerParams *runtimepb.DockerBuildParameters, imageName string) error {
	if !dockerParams.GetBuildImage() {
		return nil
	}
	if err := h.buildDockerImage(ctx, component, dockerParams, imageName); err != nil {
		return err
	}

	if dockerParams.GetLoadToKind() {
		log.InfoContextf(ctx, "Loading image %s to kind cluster...", imageName)
		clusterName, err := extractKindClusterName(h.kubeconfigPath)
		if err != nil {
			return fmt.Errorf("%w: %v", errExtractKindClusterName, err)
		}
		cmd := exec.CommandContext(ctx, "kind", "load", "docker-image", imageName, "--name", clusterName)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%w: %v, output: %s", errLoadDockerImageToKind, err, string(output))
		}
		return nil
	}

	return h.pushDockerImage(ctx, imageName)
}

func (h *kubernetesHandler) registryAuth(ctx context.Context, imageName string) (string, error) {
	cfg, err := dockerconfig.Load(dockerconfig.Dir())
	if err != nil {
		return "", fmt.Errorf("%w: %v", errLoadDockerConfig, err)
	}

	// Auth is by registry only, so remove repository and path.
	repo, _, _ := strings.Cut(imageName, "/")
	if h.imageRepositoryAddr != "" {
		repo, _, _ = strings.Cut(h.imageRepositoryAddr, "/")
	}

	authConfig, err := cfg.GetAuthConfig(repo)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errGetAuthConfig, err)
	}

	authBytes, err := json.Marshal(authConfig)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errMarshalAuthConfig, err)
	}

	return base64.URLEncoding.EncodeToString(authBytes), nil
}

func (h *kubernetesHandler) pushDockerImage(ctx context.Context, imageName string) error {
	log.InfoContextf(ctx, "Pushing Docker image: %s", imageName)

	authStr, err := h.registryAuth(ctx, imageName)
	if err != nil {
		return err
	}

	response, err := h.docker.ImagePush(ctx, imageName, dockerclient.ImagePushOptions{
		RegistryAuth: authStr,
	})
	if err != nil {
		return fmt.Errorf("%w: %s: %v", errPushDockerImage, imageName, err)
	}
	defer response.Close()

	// Consume the response body.
	scanner := bufio.NewScanner(response)
	for scanner.Scan() {
		line := scanner.Text()
		log.V(1).InfoContext(ctx, line)
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			log.WarningContextf(ctx, "Failed to unmarshal Docker push output line: %v", err)
			continue
		}
		errDetail, ok := msg["errorDetail"].(map[string]any)
		if !ok {
			// No error to log.
			continue
		}
		if errStr, ok := errDetail["message"].(string); ok && errStr != "" {
			return errors.New(errStr)
		}
	}
	return scanner.Err()
}
