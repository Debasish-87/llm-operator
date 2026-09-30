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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	servingv1alpha1 "github.com/Debasish-87/llm-operator/api/v1alpha1"
)

var _ = Describe("LLMService Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		llmservice := &servingv1alpha1.LLMService{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind LLMService")
			err := k8sClient.Get(ctx, typeNamespacedName, llmservice)
			if err != nil && errors.IsNotFound(err) {
				resource := &servingv1alpha1.LLMService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: servingv1alpha1.LLMServiceSpec{
						Model:              "Qwen/Qwen2.5-1.5B-Instruct",
						GPUs:               1,
						MaxNumSeqs:         64,
						MaxModelLen:        4096,
						TensorParallelSize: 1,
						CacheSize:          "20Gi",
						Autoscaling: &servingv1alpha1.AutoscalingSpec{
							Enabled:               true,
							MinReplicas:           1,
							MaxReplicas:           3,
							TargetWaitingRequests: 5,
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
				llmservice = resource

			}
		})

		It("should update status to Pending when deployment is not ready", func() {
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": resourceName,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": resourceName,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "vllm",
									Image: "test-image",
								},
							},
						},
					},
				},
				Status: appsv1.DeploymentStatus{
					ReadyReplicas: 0,
				},
			}

			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

			llmservice.Generation = 1

			Expect(
				updateLLMServiceStatus(
					ctx,
					k8sClient,
					llmservice,
					deployment,
				),
			).To(Succeed())

			Eventually(func() servingv1alpha1.LLMServicePhase {
				var updated servingv1alpha1.LLMService
				Expect(
					k8sClient.Get(
						ctx,
						types.NamespacedName{
							Name:      resourceName,
							Namespace: "default",
						},
						&updated,
					),
				).To(Succeed())

				return updated.Status.Phase
			}).Should(Equal(servingv1alpha1.LLMServicePhasePending))

			var updated servingv1alpha1.LLMService
			Expect(k8sClient.Get(
				ctx,
				typeNamespacedName,
				&updated,
			)).To(Succeed())

			condition := meta.FindStatusCondition(
				updated.Status.Conditions,
				"Ready",
			)

			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal("DeploymentNotReady"))
			firstTransitionTime := condition.LastTransitionTime
			Expect(firstTransitionTime.IsZero()).To(BeFalse())

			Expect(
				updateLLMServiceStatus(
					ctx,
					k8sClient,
					&updated,
					deployment,
				),
			).To(Succeed())

			var updatedAgain servingv1alpha1.LLMService

			Expect(k8sClient.Get(
				ctx,
				typeNamespacedName,
				&updatedAgain,
			)).To(Succeed())

			conditionAgain := meta.FindStatusCondition(
				updatedAgain.Status.Conditions,
				"Ready",
			)

			Expect(conditionAgain).NotTo(BeNil())
			Expect(conditionAgain.LastTransitionTime).To(Equal(firstTransitionTime))

			Expect(updatedAgain.Status.Endpoint).To(Equal(
				"http://test-resource.default.svc.cluster.local:8000",
			))

			Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
		})

		AfterEach(func() {
			resource := &servingv1alpha1.LLMService{}

			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if errors.IsNotFound(err) {
				return
			}

			Expect(err).NotTo(HaveOccurred())

			By("Cleaning up the LLMService")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			By("Reconciling deletion to remove the finalizer")
			controllerReconciler := &LLMServiceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, resource)
				return errors.IsNotFound(err)
			}).Should(BeTrue())
		})

		It("should update status to Ready when deployment is ready", func() {
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": resourceName,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": resourceName,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "vllm",
									Image: "test-image",
								},
							},
						},
					},
				},
				Status: appsv1.DeploymentStatus{
					Replicas:      1,
					ReadyReplicas: 1,
				},
			}

			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

			deployment.Status.Replicas = 1
			deployment.Status.ReadyReplicas = 1

			Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

			Expect(deployment.Status.ReadyReplicas).To(Equal(int32(1)))

			llmservice.Generation = 1

			Expect(
				updateLLMServiceStatus(
					ctx,
					k8sClient,
					llmservice,
					deployment,
				),
			).To(Succeed())

			Eventually(func() servingv1alpha1.LLMServicePhase {
				var updated servingv1alpha1.LLMService

				Expect(
					k8sClient.Get(
						ctx,
						types.NamespacedName{
							Name:      resourceName,
							Namespace: "default",
						},
						&updated,
					),
				).To(Succeed())

				return updated.Status.Phase
			}).Should(Equal(servingv1alpha1.LLMServicePhaseReady))

			var updated servingv1alpha1.LLMService
			Expect(k8sClient.Get(
				ctx,
				typeNamespacedName,
				&updated,
			)).To(Succeed())

			condition := meta.FindStatusCondition(
				updated.Status.Conditions,
				"Ready",
			)

			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal("DeploymentReady"))

			Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &LLMServiceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}

			Eventually(func() error {
				return k8sClient.Get(
					ctx,
					typeNamespacedName,
					deployment,
				)
			}).Should(Succeed())

			Expect(deployment.Spec.Replicas).NotTo(BeNil())
			Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].Name).To(Equal("vllm"))
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.

			service := &corev1.Service{}

			Eventually(func() error {
				return k8sClient.Get(
					ctx,
					typeNamespacedName,
					service,
				)
			}).Should(Succeed())

			Expect(service.Spec.Ports).To(HaveLen(1))
			Expect(service.Spec.Ports[0].Port).To(Equal(int32(8000)))
			Expect(service.Spec.Ports[0].TargetPort.IntValue()).To(Equal(8000))

			Expect(service.Spec.Selector).To(Equal(map[string]string{
				"app": "test-resource",
			}))

			pvc := &corev1.PersistentVolumeClaim{}

			Eventually(func() error {
				return k8sClient.Get(
					ctx,
					typeNamespacedName,
					pvc,
				)
			}).Should(Succeed())

			storage := pvc.Spec.Resources.Requests[corev1.ResourceStorage]

			Expect(storage.String()).To(Equal("20Gi"))

			Expect(pvc.Spec.AccessModes).To(ContainElement(
				corev1.ReadWriteOnce,
			))

			By("updating the LLMService")

			currentLLMService := &servingv1alpha1.LLMService{}

			Expect(k8sClient.Get(
				ctx,
				typeNamespacedName,
				currentLLMService,
			)).To(Succeed())

			currentLLMService.Spec.MaxNumSeqs = 128

			Expect(k8sClient.Update(
				ctx,
				currentLLMService,
			)).To(Succeed())

			By("reconciling the updated LLMService")

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("checking that the Deployment was updated")

			updatedDeployment := &appsv1.Deployment{}

			Eventually(func() error {
				return k8sClient.Get(
					ctx,
					typeNamespacedName,
					updatedDeployment,
				)
			}).Should(Succeed())

			Expect(updatedDeployment.Spec.Template.Spec.Containers).To(HaveLen(1))

			Expect(updatedDeployment.Spec.Template.Spec.Containers[0].Args).To(ContainElements(
				"--model",
				"Qwen/Qwen2.5-1.5B-Instruct",
				"--max-num-seqs",
				"128",
				"--max-model-len",
				"4096",
				"--tensor-parallel-size",
				"1",
			))

			By("checking the ScaledObject")
			scaledObject := &unstructured.Unstructured{}
			scaledObject.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "keda.sh",
				Version: "v1alpha1",
				Kind:    "ScaledObject",
			})

			Eventually(func() error {
				return k8sClient.Get(
					ctx,
					types.NamespacedName{
						Name:      resourceName,
						Namespace: "default",
					},
					scaledObject,
				)
			}).Should(Succeed())

			Expect(scaledObject.Object["apiVersion"]).To(Equal("keda.sh/v1alpha1"))
			Expect(scaledObject.Object["kind"]).To(Equal("ScaledObject"))

			spec, found, err := unstructured.NestedMap(scaledObject.Object, "spec")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())

			scaleTargetRef, found, err := unstructured.NestedMap(spec, "scaleTargetRef")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(scaleTargetRef["name"]).To(Equal(resourceName))
			Expect(scaleTargetRef["apiVersion"]).To(Equal("apps/v1"))
			Expect(scaleTargetRef["kind"]).To(Equal("Deployment"))

			minReplicas, found, err := unstructured.NestedInt64(spec, "minReplicaCount")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(minReplicas).To(Equal(int64(1)))

			maxReplicas, found, err := unstructured.NestedInt64(spec, "maxReplicaCount")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(maxReplicas).To(Equal(int64(3)))

			triggers, found, err := unstructured.NestedSlice(spec, "triggers")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(triggers).To(HaveLen(1))

			trigger := triggers[0].(map[string]any)
			Expect(trigger["type"]).To(Equal("prometheus"))

			triggerMetadata := trigger["metadata"].(map[string]any)
			Expect(triggerMetadata["threshold"]).To(Equal("5"))
			Expect(triggerMetadata["query"]).To(Equal(
				`sum(vllm:num_requests_waiting{model_name="Qwen/Qwen2.5-1.5B-Instruct"})`,
			))

			By("updating the autoscaling target")

			autoscalingLLMService := &servingv1alpha1.LLMService{}

			Eventually(func() error {
				return k8sClient.Get(
					ctx,
					typeNamespacedName,
					autoscalingLLMService,
				)
			}).Should(Succeed())

			autoscalingLLMService.Spec.Autoscaling.TargetWaitingRequests = 10

			Expect(k8sClient.Update(
				ctx,
				autoscalingLLMService,
			)).To(Succeed())

			By("reconciling the updated LLMService")

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("checking that the ScaledObject was updated")

			updatedScaledObject := &unstructured.Unstructured{}
			updatedScaledObject.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "keda.sh",
				Version: "v1alpha1",
				Kind:    "ScaledObject",
			})

			Eventually(func() error {
				return k8sClient.Get(
					ctx,
					typeNamespacedName,
					updatedScaledObject,
				)
			}).Should(Succeed())

			updatedSpec, found, err := unstructured.NestedMap(
				updatedScaledObject.Object,
				"spec",
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())

			updatedTriggers, found, err := unstructured.NestedSlice(
				updatedSpec,
				"triggers",
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(updatedTriggers).To(HaveLen(1))

			updatedTrigger := updatedTriggers[0].(map[string]any)
			updatedMetadata := updatedTrigger["metadata"].(map[string]any)

			Expect(updatedMetadata["threshold"]).To(Equal("10"))

			By("disabling autoscaling")

			autoscalingLLMService = &servingv1alpha1.LLMService{}

			Eventually(func() error {
				return k8sClient.Get(
					ctx,
					typeNamespacedName,
					autoscalingLLMService,
				)
			}).Should(Succeed())

			autoscalingLLMService.Spec.Autoscaling.Enabled = false

			Expect(k8sClient.Update(
				ctx,
				autoscalingLLMService,
			)).To(Succeed())

			By("reconciling with autoscaling disabled")

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("checking that the ScaledObject was deleted")

			deletedScaledObject := &unstructured.Unstructured{}
			deletedScaledObject.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "keda.sh",
				Version: "v1alpha1",
				Kind:    "ScaledObject",
			})

			Eventually(func() bool {
				err := k8sClient.Get(
					ctx,
					typeNamespacedName,
					deletedScaledObject,
				)
				return errors.IsNotFound(err)
			}).Should(BeTrue())

		})
		It("should add the finalizer", func() {
			controllerReconciler := &LLMServiceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &servingv1alpha1.LLMService{}

			Expect(k8sClient.Get(
				ctx,
				typeNamespacedName,
				updated,
			)).To(Succeed())

			Expect(updated.Finalizers).To(
				ContainElement(llmServiceFinalizer),
			)
		})
	})
})
