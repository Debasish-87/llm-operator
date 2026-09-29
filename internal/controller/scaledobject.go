package controller

import (
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	servingv1alpha1 "github.com/Debasish-87/llm-operator/api/v1alpha1"
)

func scaledObjectForLLMService(
	llmService *servingv1alpha1.LLMService,
) *unstructured.Unstructured {
	if llmService.Spec.Autoscaling == nil {
		return nil
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "keda.sh/v1alpha1",
			"kind":       "ScaledObject",
			"metadata": map[string]interface{}{
				"name":      llmService.Name,
				"namespace": llmService.Namespace,
			},
			"spec": map[string]interface{}{
				"scaleTargetRef": map[string]interface{}{
					"name":       llmService.Name,
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
				},
				"minReplicaCount": int64(llmService.Spec.Autoscaling.MinReplicas),
				"maxReplicaCount": int64(llmService.Spec.Autoscaling.MaxReplicas),
				"triggers": []interface{}{
					map[string]interface{}{
						"type": "prometheus",
						"metadata": map[string]interface{}{
							"serverAddress": "http://prometheus.monitoring.svc.cluster.local:9090",
							"query": fmt.Sprintf(
								`sum(vllm:num_requests_waiting{model_name="%s"})`,
								llmService.Spec.Model,
							),
							"threshold": strconv.FormatInt(
								int64(llmService.Spec.Autoscaling.TargetWaitingRequests),
								10,
							),
						},
					},
				},
			},
		},
	}
}
