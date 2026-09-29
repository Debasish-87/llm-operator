package controller

import (
	"testing"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	servingv1alpha1 "github.com/Debasish-87/llm-operator/api/v1alpha1"
)

func TestDeploymentForLLMService(t *testing.T) {
	g := NewWithT(t)

	llmService := &servingv1alpha1.LLMService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "qwen-demo",
			Namespace: "default",
		},
		Spec: servingv1alpha1.LLMServiceSpec{
			Model:              "Qwen/Qwen2.5-1.5B-Instruct",
			Image:              "vllm/vllm-openai:v0.30.0",
			MaxNumSeqs:         64,
			MaxModelLen:        4096,
			TensorParallelSize: 1,
			GPUs:               1,
		},
	}

	deployment := deploymentForLLMService(llmService)

	g.Expect(deployment.Name).To(Equal("qwen-demo"))
	g.Expect(deployment.Namespace).To(Equal("default"))
	g.Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))

	g.Expect(deployment.Spec.Selector.MatchLabels).To(Equal(map[string]string{
		"app": "qwen-demo",
	}))

	g.Expect(deployment.Spec.Template.Labels).To(Equal(map[string]string{
		"app": "qwen-demo",
	}))

	g.Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
	g.Expect(deployment.Spec.Template.Spec.Containers[0].Name).To(Equal("vllm"))
	g.Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("vllm/vllm-openai:v0.30.0"))
	g.Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(Equal([]string{
		"--model",
		"Qwen/Qwen2.5-1.5B-Instruct",
		"--max-num-seqs",
		"64",
		"--max-model-len",
		"4096",
		"--tensor-parallel-size",
		"1",
	}))

	gpuQuantity := deployment.Spec.Template.Spec.Containers[0].Resources.Requests["nvidia.com/gpu"]

	g.Expect(gpuQuantity.Value()).To(Equal(int64(1)))

	gpuLimit := deployment.Spec.Template.Spec.Containers[0].Resources.Limits["nvidia.com/gpu"]

	g.Expect(gpuLimit.Value()).To(Equal(int64(1)))

	g.Expect(deployment.Spec.Template.Spec.Tolerations).To(ContainElement(
		corev1.Toleration{
			Key:      "nvidia.com/gpu",
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
	))

	g.Expect(deployment.Spec.Template.Spec.Volumes).To(ContainElement(
		corev1.Volume{
			Name: "model-cache",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: "qwen-demo",
				},
			},
		},
	))

	g.Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts).To(ContainElement(
		corev1.VolumeMount{
			Name:      "model-cache",
			MountPath: "/root/.cache/huggingface",
		},
	))
}
