/*
Copyright 2026.

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

package controller

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	servingv1alpha1 "github.com/Debasish-87/llm-operator/api/v1alpha1"
)

const llmServiceFinalizer = "serving.debasish.dev/finalizer"

// LLMServiceReconciler reconciles a LLMService object
type LLMServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=serving.debasish.dev,resources=llmservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=serving.debasish.dev,resources=llmservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=serving.debasish.dev,resources=llmservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keda.sh,resources=scaledobjects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.

func (r *LLMServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var llmService servingv1alpha1.LLMService

	err := r.Get(ctx, req.NamespacedName, &llmService)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	if llmService.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(&llmService, llmServiceFinalizer) {
			controllerutil.AddFinalizer(&llmService, llmServiceFinalizer)
			if err := r.Update(ctx, &llmService); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		if controllerutil.ContainsFinalizer(&llmService, llmServiceFinalizer) {
			controllerutil.RemoveFinalizer(&llmService, llmServiceFinalizer)
			if err := r.Update(ctx, &llmService); err != nil {
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	}

	desiredDeployment := deploymentForLLMService(&llmService)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: desiredDeployment.Name, Namespace: desiredDeployment.Namespace},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		// Selector is immutable and replicas may be managed by KEDA:
		// only set them when the Deployment is first created.
		if deployment.CreationTimestamp.IsZero() {
			deployment.Spec.Selector = desiredDeployment.Spec.Selector
			deployment.Spec.Replicas = desiredDeployment.Spec.Replicas
		}
		deployment.Spec.Template = desiredDeployment.Spec.Template
		return ctrl.SetControllerReference(&llmService, deployment, r.Scheme)
	}); err != nil {
		return ctrl.Result{}, err
	}

	desiredPVC := pvcForLLMService(&llmService)
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: desiredPVC.Name, Namespace: desiredPVC.Namespace},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, pvc, func() error {
		// PVC spec is largely immutable after creation; set it only once.
		if pvc.CreationTimestamp.IsZero() {
			pvc.Spec = desiredPVC.Spec
		}
		return ctrl.SetControllerReference(&llmService, pvc, r.Scheme)
	}); err != nil {
		return ctrl.Result{}, err
	}

	desiredService := serviceForLLMService(&llmService)
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: desiredService.Name, Namespace: desiredService.Namespace},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		service.Spec.Selector = desiredService.Spec.Selector
		service.Spec.Ports = desiredService.Spec.Ports
		return ctrl.SetControllerReference(&llmService, service, r.Scheme)
	}); err != nil {
		return ctrl.Result{}, err
	}

	if result, err := r.reconcileScaledObject(ctx, &llmService); err != nil {
		return result, err
	}

	currentDeployment := &appsv1.Deployment{}

	if err := r.Get(
		ctx,
		req.NamespacedName,
		currentDeployment,
	); err != nil {
		return ctrl.Result{}, err
	}

	if err := updateLLMServiceStatus(
		ctx,
		r.Client,
		&llmService,
		currentDeployment,
	); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil

}

// reconcileScaledObject creates/updates the KEDA ScaledObject when autoscaling
// is enabled, and removes it otherwise. A missing KEDA CRD is only an error
// when autoscaling is actually requested.
func (r *LLMServiceReconciler) reconcileScaledObject(
	ctx context.Context,
	llmService *servingv1alpha1.LLMService,
) (ctrl.Result, error) {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "keda.sh", Version: "v1alpha1", Kind: "ScaledObject",
	})
	key := types.NamespacedName{Name: llmService.Name, Namespace: llmService.Namespace}
	err := r.Get(ctx, key, existing)

	autoscalingEnabled := llmService.Spec.Autoscaling != nil && llmService.Spec.Autoscaling.Enabled

	if !autoscalingEnabled {
		switch {
		case err == nil:
			if derr := r.Delete(ctx, existing); derr != nil && !apierrors.IsNotFound(derr) {
				return ctrl.Result{}, derr
			}
		case apierrors.IsNotFound(err), meta.IsNoMatchError(err):
			// nothing to clean up (or KEDA is not installed)
		default:
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	desired := scaledObjectForLLMService(llmService)
	if err := ctrl.SetControllerReference(llmService, desired, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, r.Create(ctx, desired)
		}
		return ctrl.Result{}, err // includes "KEDA CRD not installed"
	}

	desiredSpec, _, err := unstructured.NestedMap(desired.Object, "spec")
	if err != nil {
		return ctrl.Result{}, err
	}
	existingSpec, _, err := unstructured.NestedMap(existing.Object, "spec")
	if err != nil {
		return ctrl.Result{}, err
	}
	// KEDA defaults extra fields; only compare/apply the fields we own.
	changed := false
	for k, v := range desiredSpec {
		if !equality.Semantic.DeepEqual(existingSpec[k], v) {
			existingSpec[k] = v
			changed = true
		}
	}
	if changed {
		existing.Object["spec"] = existingSpec
		return ctrl.Result{}, r.Update(ctx, existing)
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LLMServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&servingv1alpha1.LLMService{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Named("llmservice").
		Complete(r)
}
