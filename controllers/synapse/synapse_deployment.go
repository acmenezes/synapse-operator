/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package synapse

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	synapsev1alpha1 "github.com/opdev/synapse-operator/apis/synapse/v1alpha1"
)

// deploymentForSynapse returns a synapse Deployment object
func (r *SynapseReconciler) deploymentForSynapse(s *synapsev1alpha1.Synapse, objectMeta metav1.ObjectMeta) (client.Object, error) {
	ls := labelsForSynapse(s.Name)
	replicas := int32(1)

	server_name := s.Status.HomeserverConfiguration.ServerName
	report_stats := s.Status.HomeserverConfiguration.ReportStats
	// The created Synapse ConfigMap shares the same name as the Synapse deployment
	synapseConfigMapName := objectMeta.Name

	dep := &appsv1.Deployment{
		ObjectMeta: objectMeta,
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{
						Image: "matrixdotorg/synapse:v1.60.0",
						Name:  "synapse-generate",
						Args:  []string{"generate"},
						Env: []corev1.EnvVar{{
							Name:  "SYNAPSE_CONFIG_PATH",
							Value: "/data-homeserver/homeserver.yaml",
						}, {
							Name:  "SYNAPSE_SERVER_NAME",
							Value: server_name,
						}, {
							Name:  "SYNAPSE_REPORT_STATS",
							Value: boolToYesNo(report_stats),
						}},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "homeserver",
							MountPath: "/data-homeserver",
						}, {
							Name:      "data-pv",
							MountPath: "/data",
						}},
					}},
					Containers: []corev1.Container{{
						Image: "matrixdotorg/synapse:v1.60.0",
						Name:  "synapse",
						Env: []corev1.EnvVar{{
							Name:  "SYNAPSE_CONFIG_PATH",
							Value: "/data-homeserver/homeserver.yaml",
						}},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "homeserver",
							MountPath: "/data-homeserver",
						}, {
							Name:      "data-pv",
							MountPath: "/data",
						}},
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8008,
						}},
					}},
					ServiceAccountName: s.Name,
					Volumes: []corev1.Volume{{
						Name: "homeserver",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: synapseConfigMapName,
								},
							},
						},
					}, {
						Name: "data-pv",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: s.Name,
							},
						},
					}},
				},
			},
		},
	}

	if s.Spec.Bridges.Heisenbridge.Enabled {
		heisenbridgeConfigMapName := objectMeta.Name + "-heisenbridge"

		dep.Spec.Template.Spec.Containers[0].VolumeMounts = append(
			dep.Spec.Template.Spec.Containers[0].VolumeMounts,
			corev1.VolumeMount{
				Name:      "data-heisenbridge",
				MountPath: "/data-heisenbridge",
			},
		)

		dep.Spec.Template.Spec.Volumes = append(
			dep.Spec.Template.Spec.Volumes,
			corev1.Volume{
				Name: "data-heisenbridge",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: heisenbridgeConfigMapName,
						},
					},
				},
			},
		)
	}

	// Set Synapse instance as the owner and controller
	if err := ctrl.SetControllerReference(s, dep, r.Scheme); err != nil {
		return &appsv1.Deployment{}, err
	}

	return dep, nil
}
