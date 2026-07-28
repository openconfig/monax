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
	"testing"

	"github.com/google/go-cmp/cmp"

	kubernetesapps "k8s.io/api/apps/v1"
	kubernetescore "k8s.io/api/core/v1"
	kubernetesmeta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestApplyUniqueNameSuffix(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		nameSuffix     string
		deployment     *kubernetesapps.Deployment
		service        *kubernetescore.Service
		wantDeployment *kubernetesapps.Deployment
		wantService    *kubernetescore.Service
	}{
		"no suffix": {
			nameSuffix: "",
			deployment: &kubernetesapps.Deployment{
				ObjectMeta: kubernetesmeta.ObjectMeta{Name: "echo"},
			},
			service: &kubernetescore.Service{
				ObjectMeta: kubernetesmeta.ObjectMeta{Name: "echo-service"},
			},
			wantDeployment: &kubernetesapps.Deployment{
				ObjectMeta: kubernetesmeta.ObjectMeta{Name: "echo"},
			},
			wantService: &kubernetescore.Service{
				ObjectMeta: kubernetesmeta.ObjectMeta{Name: "echo-service"},
			},
		},
		"instance label added": {
			nameSuffix: "withLabel",
			deployment: &kubernetesapps.Deployment{
				ObjectMeta: kubernetesmeta.ObjectMeta{Name: "echo"},
			},
			service: &kubernetescore.Service{
				ObjectMeta: kubernetesmeta.ObjectMeta{Name: "echo-service"},
			},
			wantDeployment: &kubernetesapps.Deployment{
				ObjectMeta: kubernetesmeta.ObjectMeta{
					Name: "echo-withLabel",
					Labels: map[string]string{
						"app.kubernetes.io/instance": "echo-withLabel",
					},
				},
				Spec: kubernetesapps.DeploymentSpec{
					Selector: &kubernetesmeta.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/instance": "echo-withLabel",
						},
					},
					Template: kubernetescore.PodTemplateSpec{
						ObjectMeta: kubernetesmeta.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/instance": "echo-withLabel",
							},
						},
					},
				},
			},
			wantService: &kubernetescore.Service{
				ObjectMeta: kubernetesmeta.ObjectMeta{
					Name: "echo-service-withLabel",
					Labels: map[string]string{
						"app.kubernetes.io/instance": "echo-service-withLabel",
					},
				},
				Spec: kubernetescore.ServiceSpec{
					Selector: map[string]string{
						"app.kubernetes.io/instance": "echo-service-withLabel",
					},
				},
			},
		},
		"applies suffix to deployment, service, env, envFrom and volumes": {
			nameSuffix: "run-123",
			deployment: &kubernetesapps.Deployment{
				ObjectMeta: kubernetesmeta.ObjectMeta{
					Name: "echo",
					Labels: map[string]string{
						"app.kubernetes.io/instance": "echo",
					},
				},
				Spec: kubernetesapps.DeploymentSpec{
					Selector: &kubernetesmeta.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/instance": "echo",
						},
					},
					Template: kubernetescore.PodTemplateSpec{
						ObjectMeta: kubernetesmeta.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/instance": "echo",
							},
						},
						Spec: kubernetescore.PodSpec{
							Containers: []kubernetescore.Container{
								{
									Name: "app",
									Env: []kubernetescore.EnvVar{
										{
											Name: "MONAX_CONFIG",
											ValueFrom: &kubernetescore.EnvVarSource{
												ConfigMapKeyRef: &kubernetescore.ConfigMapKeySelector{
													LocalObjectReference: kubernetescore.LocalObjectReference{Name: "echo"},
													Key:                  "config.json",
												},
											},
										},
										{
											Name: "EXTERNAL_CONFIG",
											ValueFrom: &kubernetescore.EnvVarSource{
												ConfigMapKeyRef: &kubernetescore.ConfigMapKeySelector{
													LocalObjectReference: kubernetescore.LocalObjectReference{Name: "external-cm"},
													Key:                  "ext.json",
												},
											},
										},
									},
									EnvFrom: []kubernetescore.EnvFromSource{
										{
											ConfigMapRef: &kubernetescore.ConfigMapEnvSource{
												LocalObjectReference: kubernetescore.LocalObjectReference{Name: "echo"},
											},
										},
										{
											ConfigMapRef: &kubernetescore.ConfigMapEnvSource{
												LocalObjectReference: kubernetescore.LocalObjectReference{Name: "external-cm"},
											},
										},
									},
								},
							},
							Volumes: []kubernetescore.Volume{
								{
									Name: "config-vol",
									VolumeSource: kubernetescore.VolumeSource{
										ConfigMap: &kubernetescore.ConfigMapVolumeSource{
											LocalObjectReference: kubernetescore.LocalObjectReference{Name: "echo"},
										},
									},
								},
								{
									Name: "ext-vol",
									VolumeSource: kubernetescore.VolumeSource{
										ConfigMap: &kubernetescore.ConfigMapVolumeSource{
											LocalObjectReference: kubernetescore.LocalObjectReference{Name: "external-cm"},
										},
									},
								},
							},
						},
					},
				},
			},
			service: &kubernetescore.Service{
				ObjectMeta: kubernetesmeta.ObjectMeta{
					Name: "echo-service",
					Labels: map[string]string{
						"app.kubernetes.io/instance": "echo-service",
					},
				},
				Spec: kubernetescore.ServiceSpec{
					Selector: map[string]string{
						"app.kubernetes.io/instance": "echo-service",
					},
				},
			},
			wantDeployment: &kubernetesapps.Deployment{
				ObjectMeta: kubernetesmeta.ObjectMeta{
					Name: "echo-run-123",
					Labels: map[string]string{
						"app.kubernetes.io/instance": "echo-run-123",
					},
				},
				Spec: kubernetesapps.DeploymentSpec{
					Selector: &kubernetesmeta.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/instance": "echo-run-123",
						},
					},
					Template: kubernetescore.PodTemplateSpec{
						ObjectMeta: kubernetesmeta.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/instance": "echo-run-123",
							},
						},
						Spec: kubernetescore.PodSpec{
							Containers: []kubernetescore.Container{
								{
									Name: "app-run-123",
									Env: []kubernetescore.EnvVar{
										{
											Name: "MONAX_CONFIG",
											ValueFrom: &kubernetescore.EnvVarSource{
												ConfigMapKeyRef: &kubernetescore.ConfigMapKeySelector{
													LocalObjectReference: kubernetescore.LocalObjectReference{Name: "echo-run-123"},
													Key:                  "config.json",
												},
											},
										},
										{
											Name: "EXTERNAL_CONFIG",
											ValueFrom: &kubernetescore.EnvVarSource{
												ConfigMapKeyRef: &kubernetescore.ConfigMapKeySelector{
													LocalObjectReference: kubernetescore.LocalObjectReference{Name: "external-cm"},
													Key:                  "ext.json",
												},
											},
										},
									},
									EnvFrom: []kubernetescore.EnvFromSource{
										{
											ConfigMapRef: &kubernetescore.ConfigMapEnvSource{
												LocalObjectReference: kubernetescore.LocalObjectReference{Name: "echo-run-123"},
											},
										},
										{
											ConfigMapRef: &kubernetescore.ConfigMapEnvSource{
												LocalObjectReference: kubernetescore.LocalObjectReference{Name: "external-cm"},
											},
										},
									},
								},
							},
							Volumes: []kubernetescore.Volume{
								{
									Name: "config-vol",
									VolumeSource: kubernetescore.VolumeSource{
										ConfigMap: &kubernetescore.ConfigMapVolumeSource{
											LocalObjectReference: kubernetescore.LocalObjectReference{Name: "echo-run-123"},
										},
									},
								},
								{
									Name: "ext-vol",
									VolumeSource: kubernetescore.VolumeSource{
										ConfigMap: &kubernetescore.ConfigMapVolumeSource{
											LocalObjectReference: kubernetescore.LocalObjectReference{Name: "external-cm"},
										},
									},
								},
							},
						},
					},
				},
			},
			wantService: &kubernetescore.Service{
				ObjectMeta: kubernetesmeta.ObjectMeta{
					Name: "echo-service-run-123",
					Labels: map[string]string{
						"app.kubernetes.io/instance": "echo-service-run-123",
					},
				},
				Spec: kubernetescore.ServiceSpec{
					Selector: map[string]string{
						"app.kubernetes.io/instance": "echo-service-run-123",
					},
				},
			},
		},
	}

	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			handler := &kubernetesHandler{
				nameSuffix: test.nameSuffix,
			}
			handler.applyUniqueNameSuffix(test.deployment, test.service)

			if diff := cmp.Diff(test.wantDeployment, test.deployment); diff != "" {
				t.Errorf("applyUniqueNameSuffix: deployment diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantService, test.service); diff != "" {
				t.Errorf("applyUniqueNameSuffix: service diff (-want +got):\n%s", diff)
			}
		})
	}
}
