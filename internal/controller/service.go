package controller

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	servingv1alpha1 "github.com/Debasish-87/llm-operator/api/v1alpha1"
)

func serviceForLLMService(
	llmService *servingv1alpha1.LLMService,
) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      llmService.Name,
			Namespace: llmService.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": llmService.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Port:       8000,
					TargetPort: intstr.FromInt(8000),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}
