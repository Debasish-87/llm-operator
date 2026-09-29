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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

	deployment := deploymentForLLMService(&llmService)

	if err := ctrl.SetControllerReference(&llmService, deployment, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	existingDeployment := &appsv1.Deployment{}

	err = r.Get(ctx, req.NamespacedName, existingDeployment)
	if err != nil {
		if apierrors.IsNotFound(err) {
			if err := r.Create(ctx, deployment); err != nil {
				return ctrl.Result{}, err
			}
		} else {
			return ctrl.Result{}, err
		}
	} else {
		if !equality.Semantic.DeepEqual(
			existingDeployment.Spec.Template,
			deployment.Spec.Template,
		) {
			existingDeployment.Spec.Template = deployment.Spec.Template

			if err := r.Update(ctx, existingDeployment); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	pvc := pvcForLLMService(&llmService)

	if err := ctrl.SetControllerReference(&llmService, pvc, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	existingPVC := &corev1.PersistentVolumeClaim{}

	err = r.Get(ctx, req.NamespacedName, existingPVC)
	if err != nil {
		if apierrors.IsNotFound(err) {
			err = r.Create(ctx, pvc)
			if err != nil {
				return ctrl.Result{}, err
			}
		} else {
			return ctrl.Result{}, err
		}
	}

	service := serviceForLLMService(&llmService)

	if err := ctrl.SetControllerReference(&llmService, service, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	existingService := &corev1.Service{}

	err = r.Get(ctx, req.NamespacedName, existingService)
	if err != nil {
		if apierrors.IsNotFound(err) {
			if err := r.Create(ctx, service); err != nil {
				return ctrl.Result{}, err
			}
		} else {
			return ctrl.Result{}, err
		}
	}

	if llmService.Spec.Autoscaling != nil &&
		llmService.Spec.Autoscaling.Enabled {
		scaledObject := scaledObjectForLLMService(&llmService)

		if err := ctrl.SetControllerReference(
			&llmService,
			scaledObject,
			r.Scheme,
		); err != nil {
			return ctrl.Result{}, err
		}

		existingScaledObject := &unstructured.Unstructured{}
		existingScaledObject.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "keda.sh",
			Version: "v1alpha1",
			Kind:    "ScaledObject",
		})

		err := r.Get(
			ctx,
			req.NamespacedName,
			existingScaledObject,
		)

		if err != nil {
			if apierrors.IsNotFound(err) {
				if err := r.Create(ctx, scaledObject); err != nil {
					return ctrl.Result{}, err
				}
			} else {
				return ctrl.Result{}, err
			}
		} else {
			desiredSpec, _, err := unstructured.NestedMap(
				scaledObject.Object,
				"spec",
			)
			if err != nil {
				return ctrl.Result{}, err
			}

			existingSpec, _, err := unstructured.NestedMap(
				existingScaledObject.Object,
				"spec",
			)
			if err != nil {
				return ctrl.Result{}, err
			}

			if !equality.Semantic.DeepEqual(existingSpec, desiredSpec) {
				existingScaledObject.Object["spec"] = desiredSpec

				if err := r.Update(ctx, existingScaledObject); err != nil {
					return ctrl.Result{}, err
				}
			}
		}
	} else {
		existingScaledObject := &unstructured.Unstructured{}
		existingScaledObject.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "keda.sh",
			Version: "v1alpha1",
			Kind:    "ScaledObject",
		})

		err := r.Get(
			ctx,
			req.NamespacedName,
			existingScaledObject,
		)

		if err == nil {
			if err := r.Delete(ctx, existingScaledObject); err != nil {
				return ctrl.Result{}, err
			}
		} else if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
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

// SetupWithManager sets up the controller with the Manager.
func (r *LLMServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&servingv1alpha1.LLMService{}).
		Named("llmservice").
		Complete(r)
}
