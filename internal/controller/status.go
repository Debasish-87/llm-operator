package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	servingv1alpha1 "github.com/Debasish-87/llm-operator/api/v1alpha1"
)

func updateLLMServiceStatus(
	ctx context.Context,
	c client.Client,
	llmService *servingv1alpha1.LLMService,
	deployment *appsv1.Deployment,
) error {
	desiredStatus := llmService.Status.DeepCopy()

	desiredStatus.ReadyReplicas = deployment.Status.ReadyReplicas
	desiredStatus.ObservedGeneration = llmService.Generation

	desiredStatus.Endpoint = fmt.Sprintf(
		"http://%s.%s.svc.cluster.local:8000",
		llmService.Name,
		llmService.Namespace,
	)

	desiredReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		desiredReplicas = *deployment.Spec.Replicas
	}

	if deployment.Status.ReadyReplicas >= desiredReplicas {
		desiredStatus.Phase = servingv1alpha1.LLMServicePhaseReady
	} else {
		desiredStatus.Phase = servingv1alpha1.LLMServicePhasePending
	}

	readyCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: llmService.Generation,
	}

	if deployment.Status.ReadyReplicas >= desiredReplicas {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = "DeploymentReady"
		readyCondition.Message = "Deployment has the desired number of ready replicas"
	} else {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "DeploymentNotReady"
		readyCondition.Message = "Deployment does not have the desired number of ready replicas"
	}

	existingCondition := meta.FindStatusCondition(
		desiredStatus.Conditions,
		"Ready",
	)

	if existingCondition == nil ||
		existingCondition.Status != readyCondition.Status {
		readyCondition.LastTransitionTime = metav1.Now()
	} else {
		readyCondition.LastTransitionTime = existingCondition.LastTransitionTime
	}

	meta.SetStatusCondition(
		&desiredStatus.Conditions,
		readyCondition,
	)

	if equality.Semantic.DeepEqual(llmService.Status, *desiredStatus) {
		return nil
	}

	llmService.Status = *desiredStatus

	return c.Status().Update(ctx, llmService)
}
