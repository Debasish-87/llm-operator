package controller

import (
	"testing"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	servingv1alpha1 "github.com/Debasish-87/llm-operator/api/v1alpha1"
)

func TestPVCForLLMService(t *testing.T) {
	g := NewWithT(t)

	llmService := &servingv1alpha1.LLMService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "qwen-demo",
			Namespace: "default",
		},
		Spec: servingv1alpha1.LLMServiceSpec{
			Model:              "Qwen/Qwen2.5-1.5B-Instruct",
			GPUs:               1,
			MaxNumSeqs:         64,
			MaxModelLen:        4096,
			TensorParallelSize: 1,
			CacheSize:          "20Gi",
		},
	}

	pvc := pvcForLLMService(llmService)

	g.Expect(pvc.Name).To(Equal("qwen-demo"))
	g.Expect(pvc.Namespace).To(Equal("default"))

	g.Expect(pvc.Spec.AccessModes).To(ConsistOf(
		corev1.ReadWriteOnce,
	))

	storage := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	g.Expect(storage.String()).To(Equal("20Gi"))
}
