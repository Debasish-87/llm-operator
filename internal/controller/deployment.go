package controller

import (
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	servingv1alpha1 "github.com/Debasish-87/llm-operator/api/v1alpha1"
)

func deploymentForLLMService(
	llmService *servingv1alpha1.LLMService,
) *appsv1.Deployment {
	var env []corev1.EnvVar
	if ref := llmService.Spec.HFTokenSecretRef; ref != nil {
		key := ref.Key
		if key == "" {
			key = "token"
		}
		env = append(env, corev1.EnvVar{
			Name: "HF_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: ref.Name},
					Key:                  key,
				},
			},
		})
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      llmService.Name,
			Namespace: llmService.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": llmService.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": llmService.Name,
					},
					Annotations: map[string]string{
						"prometheus.io/scrape": "true",
						"prometheus.io/path":   "/metrics",
						"prometheus.io/port":   "8000",
					},
				},
				Spec: corev1.PodSpec{
					Tolerations: []corev1.Toleration{
						{
							Key:      "nvidia.com/gpu",
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "model-cache",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: llmService.Name,
								},
							},
						},
						{
							// vLLM needs a large shared-memory segment (tensor parallel / NCCL).
							Name: "shm",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "vllm",
							Image: llmService.Spec.Image,
							Env:   env,
							Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8000, Protocol: corev1.ProtocolTCP}},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "model-cache",
									MountPath: "/root/.cache/huggingface",
								},
								{
									Name:      "shm",
									MountPath: "/dev/shm",
								},
							},
							Args: []string{
								"--model",
								llmService.Spec.Model,
								"--max-num-seqs",
								strconv.FormatInt(int64(llmService.Spec.MaxNumSeqs), 10),
								"--max-model-len",
								strconv.FormatInt(int64(llmService.Spec.MaxModelLen), 10),
								"--tensor-parallel-size",
								strconv.FormatInt(int64(llmService.Spec.TensorParallelSize), 10),
							},

							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"nvidia.com/gpu": resource.MustParse(
										strconv.FormatInt(int64(llmService.Spec.GPUs), 10),
									),
								},
								Limits: corev1.ResourceList{
									"nvidia.com/gpu": resource.MustParse(
										strconv.FormatInt(int64(llmService.Spec.GPUs), 10),
									),
								},
							},

							// Model download/load can take many minutes on first start.
							StartupProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(8000),
									},
								},
								PeriodSeconds:    10,
								FailureThreshold: 180,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(8000),
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func int32Ptr(value int32) *int32 {
	return &value
}
