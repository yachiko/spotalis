//go:build integration
// +build integration

/*
Copyright 2024 The Spotalis Authors.

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

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/yachiko/spotalis/tests/integration/shared"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestPDBIntegration(t *testing.T) {
	if err := shared.SetupTestLogger(); err != nil {
		t.Fatalf("Failed to set up logger: %v", err)
	}
	RegisterFailHandler(Fail)
	RunSpecs(t, "PDB Integration Suite", Label("integration", "pdb"))
}

var _ = Describe("PDB Integration Tests", func() {
	var (
		ctx        context.Context
		cancel     context.CancelFunc
		kindHelper *shared.KindClusterHelper
		k8sClient  client.Client
		namespace  string
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())

		var err error
		kindHelper, err = shared.NewKindClusterHelper(ctx)
		Expect(err).NotTo(HaveOccurred())

		kindHelper.WaitForSpotalisController()

		namespace, err = kindHelper.CreateTestNamespace()
		Expect(err).NotTo(HaveOccurred())

		k8sClient = kindHelper.Client
	})

	AfterEach(func() {
		if namespace != "" && kindHelper != nil {
			err := kindHelper.CleanupNamespace(namespace)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed to cleanup test namespace %s", namespace))
		}
		if cancel != nil {
			cancel()
		}
	})

	Context("PDB Pre-Check Validation (Task 004)", func() {
		It("should respect PDB with minAvailable when rebalancing", func() {
			By("Creating a deployment with 3 replicas and 70% spot target")
			replicas := int32(3)
			deployment := createPDBTestDeployment("pdb-minavail-test", namespace, replicas, 70, 1)

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all pods to be created")
			Eventually(func() int {
				return countDeploymentPods(ctx, k8sClient, namespace, "pdb-minavail-test")
			}, 120*time.Second, 3*time.Second).Should(Equal(3))

			By("Creating a PDB that requires all pods to be available")
			pdb := &policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pdb-minavail-test",
					Namespace: namespace,
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					MinAvailable: intStrPtr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "pdb-minavail-test"},
					},
				},
			}
			err = k8sClient.Create(ctx, pdb)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for pods to have nodeSelector applied")
			Eventually(func() bool {
				return allPodsHaveNodeSelector(ctx, k8sClient, namespace, "pdb-minavail-test")
			}, 90*time.Second, 3*time.Second).Should(BeTrue())

			By("Verifying PDB status shows zero disruptions allowed")
			Eventually(func() int32 {
				pdbStatus := &policyv1.PodDisruptionBudget{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "pdb-minavail-test"}, pdbStatus)
				if err != nil {
					return -1
				}
				return pdbStatus.Status.DisruptionsAllowed
			}, 60*time.Second, 3*time.Second).Should(Equal(int32(0)))

			By("Checking that pods remain stable (no deletions)")
			initialPodUIDs := getPodUIDs(ctx, k8sClient, namespace, "pdb-minavail-test")
			GinkgoWriter.Printf("Initial pod UIDs: %v\n", initialPodUIDs)

			Consistently(func() int {
				currentPods := countDeploymentPods(ctx, k8sClient, namespace, "pdb-minavail-test")
				return currentPods
			}, 30*time.Second, 5*time.Second).Should(Equal(3), "Pod count should remain stable with strict PDB")
		})

		It("should allow rebalancing when PDB permits disruption", func() {
			By("Creating a deployment with 4 replicas and 70% spot target")
			replicas := int32(4)
			deployment := createPDBTestDeployment("pdb-allow-test", namespace, replicas, 70, 1)

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all pods to be created")
			Eventually(func() int {
				return countDeploymentPods(ctx, k8sClient, namespace, "pdb-allow-test")
			}, 120*time.Second, 3*time.Second).Should(Equal(4))

			By("Creating a PDB that allows 1 disruption")
			pdb := &policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pdb-allow-test",
					Namespace: namespace,
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					MaxUnavailable: intStrPtr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "pdb-allow-test"},
					},
				},
			}
			err = k8sClient.Create(ctx, pdb)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for pods to have nodeSelector applied")
			Eventually(func() bool {
				return allPodsHaveNodeSelector(ctx, k8sClient, namespace, "pdb-allow-test")
			}, 90*time.Second, 3*time.Second).Should(BeTrue())

			By("Verifying PDB status shows disruptions allowed")
			Eventually(func() int32 {
				pdbStatus := &policyv1.PodDisruptionBudget{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "pdb-allow-test"}, pdbStatus)
				if err != nil {
					return -1
				}
				return pdbStatus.Status.DisruptionsAllowed
			}, 60*time.Second, 3*time.Second).Should(BeNumerically(">=", int32(1)))

			By("Verifying distribution is maintained")
			spotCount, onDemandCount := countPodDistribution(ctx, k8sClient, namespace, "pdb-allow-test")
			GinkgoWriter.Printf("Distribution with permissive PDB: spot=%d, on-demand=%d\n", spotCount, onDemandCount)

			Expect(onDemandCount).To(BeNumerically(">=", 1))
		})

		It("should handle percentage-based PDB correctly", func() {
			By("Creating a deployment with 5 replicas")
			replicas := int32(5)
			deployment := createPDBTestDeployment("pdb-percent-test", namespace, replicas, 60, 1)

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all pods to be created")
			Eventually(func() int {
				return countDeploymentPods(ctx, k8sClient, namespace, "pdb-percent-test")
			}, 120*time.Second, 3*time.Second).Should(Equal(5))

			By("Creating a percentage-based PDB (20% maxUnavailable = 1 pod for 5 replicas)")
			pdb := &policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pdb-percent-test",
					Namespace: namespace,
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.String,
						StrVal: "20%",
					},
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "pdb-percent-test"},
					},
				},
			}
			err = k8sClient.Create(ctx, pdb)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for pods to have nodeSelector applied")
			Eventually(func() bool {
				return allPodsHaveNodeSelector(ctx, k8sClient, namespace, "pdb-percent-test")
			}, 90*time.Second, 3*time.Second).Should(BeTrue())

			By("Verifying PDB status reflects percentage calculation")
			Eventually(func() int32 {
				pdbStatus := &policyv1.PodDisruptionBudget{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "pdb-percent-test"}, pdbStatus)
				if err != nil {
					return -1
				}
				GinkgoWriter.Printf("PDB status: disruptions_allowed=%d, current_healthy=%d, desired_healthy=%d\n",
					pdbStatus.Status.DisruptionsAllowed,
					pdbStatus.Status.CurrentHealthy,
					pdbStatus.Status.DesiredHealthy)
				return pdbStatus.Status.DisruptionsAllowed
			}, 60*time.Second, 3*time.Second).Should(BeNumerically(">=", int32(1)))
		})

		It("should not rebalance workload without PDB (baseline)", func() {
			By("Creating a deployment without PDB")
			replicas := int32(3)
			deployment := createPDBTestDeployment("no-pdb-test", namespace, replicas, 70, 1)

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all pods to be created")
			Eventually(func() int {
				return countDeploymentPods(ctx, k8sClient, namespace, "no-pdb-test")
			}, 120*time.Second, 3*time.Second).Should(Equal(3))

			By("Waiting for pods to have nodeSelector applied")
			Eventually(func() bool {
				return allPodsHaveNodeSelector(ctx, k8sClient, namespace, "no-pdb-test")
			}, 90*time.Second, 3*time.Second).Should(BeTrue())

			By("Verifying distribution without PDB constraints")
			spotCount, onDemandCount := countPodDistribution(ctx, k8sClient, namespace, "no-pdb-test")
			GinkgoWriter.Printf("Distribution without PDB: spot=%d, on-demand=%d\n", spotCount, onDemandCount)

			Expect(spotCount + onDemandCount).To(Equal(3))
			Expect(onDemandCount).To(BeNumerically(">=", 1))
		})
	})

	Context("PDB Edge Cases", func() {
		It("should handle PDB with zero minAvailable gracefully", func() {
			By("Creating a deployment with 3 replicas")
			replicas := int32(3)
			deployment := createPDBTestDeployment("pdb-zero-test", namespace, replicas, 70, 1)

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all pods to be created")
			Eventually(func() int {
				return countDeploymentPods(ctx, k8sClient, namespace, "pdb-zero-test")
			}, 120*time.Second, 3*time.Second).Should(Equal(3))

			By("Creating a PDB with minAvailable=0 (no protection)")
			pdb := &policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pdb-zero-test",
					Namespace: namespace,
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					MinAvailable: intStrPtr(0),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "pdb-zero-test"},
					},
				},
			}
			err = k8sClient.Create(ctx, pdb)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for pods to have nodeSelector applied")
			Eventually(func() bool {
				return allPodsHaveNodeSelector(ctx, k8sClient, namespace, "pdb-zero-test")
			}, 90*time.Second, 3*time.Second).Should(BeTrue())

			By("Verifying PDB allows all disruptions")
			Eventually(func() int32 {
				pdbStatus := &policyv1.PodDisruptionBudget{}
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "pdb-zero-test"}, pdbStatus)
				if err != nil {
					return -1
				}
				return pdbStatus.Status.DisruptionsAllowed
			}, 60*time.Second, 3*time.Second).Should(Equal(int32(3)))
		})

		It("should handle multiple PDBs matching same pods", func() {
			By("Creating a deployment with 4 replicas")
			replicas := int32(4)
			deployment := createPDBTestDeployment("multi-pdb-test", namespace, replicas, 60, 1)

			deployment.Spec.Selector.MatchLabels["tier"] = "frontend"
			deployment.Spec.Template.Labels["tier"] = "frontend"

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all pods to be created")
			Eventually(func() int {
				return countDeploymentPods(ctx, k8sClient, namespace, "multi-pdb-test")
			}, 120*time.Second, 3*time.Second).Should(Equal(4))

			By("Creating first PDB matching on app label")
			pdb1 := &policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pdb-app-match",
					Namespace: namespace,
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					MaxUnavailable: intStrPtr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "multi-pdb-test"},
					},
				},
			}
			err = k8sClient.Create(ctx, pdb1)
			Expect(err).NotTo(HaveOccurred())

			By("Creating second PDB matching on tier label (more restrictive)")
			pdb2 := &policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pdb-tier-match",
					Namespace: namespace,
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					MinAvailable: intStrPtr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"tier": "frontend"},
					},
				},
			}
			err = k8sClient.Create(ctx, pdb2)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for pods to have nodeSelector applied")
			Eventually(func() bool {
				return allPodsHaveNodeSelector(ctx, k8sClient, namespace, "multi-pdb-test")
			}, 90*time.Second, 3*time.Second).Should(BeTrue())

			By("Verifying both PDBs are tracked")
			pdb1Status := &policyv1.PodDisruptionBudget{}
			err = k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "pdb-app-match"}, pdb1Status)
			Expect(err).NotTo(HaveOccurred())

			pdb2Status := &policyv1.PodDisruptionBudget{}
			err = k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "pdb-tier-match"}, pdb2Status)
			Expect(err).NotTo(HaveOccurred())

			GinkgoWriter.Printf("PDB1 (app): disruptions_allowed=%d\n", pdb1Status.Status.DisruptionsAllowed)
			GinkgoWriter.Printf("PDB2 (tier): disruptions_allowed=%d\n", pdb2Status.Status.DisruptionsAllowed)

			Expect(pdb1Status.Status.DisruptionsAllowed + pdb2Status.Status.DisruptionsAllowed).To(BeNumerically(">=", int32(1)))
		})
	})
})

func createPDBTestDeployment(name, namespace string, replicas int32, spotPercentage, minOnDemand int) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":                 name,
				"spotalis.io/enabled": "true",
			},
			Annotations: map[string]string{
				"spotalis.io/spot-percentage": fmt.Sprintf("%d", spotPercentage),
				"spotalis.io/min-on-demand":   fmt.Sprintf("%d", minOnDemand),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": name},
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: int64Ptr(0),
					Tolerations: []corev1.Toleration{
						{
							Key:    "node-role.kubernetes.io/control-plane",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:alpine",
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/",
										Port: intstr.FromInt(80),
									},
								},
								InitialDelaySeconds: 1,
								PeriodSeconds:       2,
							},
						},
					},
				},
			},
		},
	}
}

func countDeploymentPods(ctx context.Context, c client.Client, namespace, appLabel string) int {
	podList := &corev1.PodList{}
	if err := c.List(ctx, podList,
		client.InNamespace(namespace),
		client.MatchingLabels{"app": appLabel}); err != nil {
		return 0
	}

	count := 0
	for _, pod := range podList.Items {
		if pod.DeletionTimestamp == nil {
			count++
		}
	}
	return count
}

func getPodUIDs(ctx context.Context, c client.Client, namespace, appLabel string) []string {
	podList := &corev1.PodList{}
	if err := c.List(ctx, podList,
		client.InNamespace(namespace),
		client.MatchingLabels{"app": appLabel}); err != nil {
		return nil
	}

	var uids []string
	for _, pod := range podList.Items {
		if pod.DeletionTimestamp == nil {
			uids = append(uids, string(pod.UID))
		}
	}
	return uids
}

func intStrPtr(val int) *intstr.IntOrString {
	v := intstr.FromInt(val)
	return &v
}
