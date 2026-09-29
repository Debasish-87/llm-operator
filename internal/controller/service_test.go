package controller

import (
	"testing"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	servingv1alpha1 "github.com/Debasish-87/llm-operator/api/v1alpha1"
)

func TestServiceForLLMService(t *testing.T) {
	g := NewWithT(t)

	llmService := &servingv1alpha1.LLMService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "qwen-demo",
			Namespace: "default",
		},
	}

	service := serviceForLLMService(llmService)

	g.Expect(service.Name).To(Equal("qwen-demo"))
	g.Expect(service.Namespace).To(Equal("default"))

	g.Expect(service.Spec.Selector).To(Equal(map[string]string{
		"app": "qwen-demo",
	}))

	g.Expect(service.Spec.Ports).To(HaveLen(1))

	g.Expect(service.Spec.Ports[0].Port).To(Equal(int32(8000)))
	g.Expect(service.Spec.Ports[0].TargetPort.IntValue()).To(Equal(8000))

	g.Expect(service.Spec.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
}
