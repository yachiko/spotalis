package controllers

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("CheckPDBStatus", func() {
	var (
		ctx    context.Context
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		_ = corev1.AddToScheme(scheme)
		_ = policyv1.AddToScheme(scheme)
	})

	Context("when no PDB exists", func() {
		It("should return CanDisrupt=true", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels:    map[string]string{"app": "test"},
				},
			}

			client := fake.NewClientBuilder().WithScheme(scheme).Build()

			status, err := CheckPDBStatus(ctx, client, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(status).NotTo(BeNil())
			Expect(status.HasPDB).To(BeFalse())
			Expect(status.CanDisrupt).To(BeTrue())
		})
	})

	Context("when PDB allows disruption", func() {
		It("should return CanDisrupt=true with PDB details", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels:    map[string]string{"app": "test"},
				},
			}

			pdb := &policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pdb",
					Namespace: "default",
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					MinAvailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 2},
				},
				Status: policyv1.PodDisruptionBudgetStatus{
					DisruptionsAllowed: 1,
					CurrentHealthy:     3,
					DesiredHealthy:     2,
					ExpectedPods:       3,
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(pod, pdb).
				Build()

			status, err := CheckPDBStatus(ctx, client, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(status).NotTo(BeNil())
			Expect(status.HasPDB).To(BeTrue())
			Expect(status.PDBName).To(Equal("test-pdb"))
			Expect(status.CanDisrupt).To(BeTrue())
			Expect(status.DisruptionsAllowed).To(Equal(int32(1)))
			Expect(status.CurrentHealthy).To(Equal(int32(3)))
			Expect(status.DesiredHealthy).To(Equal(int32(2)))
		})
	})

	Context("when PDB blocks disruption", func() {
		It("should return CanDisrupt=false with block reason", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels:    map[string]string{"app": "test"},
				},
			}

			pdb := &policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pdb",
					Namespace: "default",
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					MinAvailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 3},
				},
				Status: policyv1.PodDisruptionBudgetStatus{
					DisruptionsAllowed: 0,
					CurrentHealthy:     3,
					DesiredHealthy:     3,
					ExpectedPods:       3,
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(pod, pdb).
				Build()

			status, err := CheckPDBStatus(ctx, client, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(status).NotTo(BeNil())
			Expect(status.HasPDB).To(BeTrue())
			Expect(status.PDBName).To(Equal("test-pdb"))
			Expect(status.CanDisrupt).To(BeFalse())
			Expect(status.DisruptionsAllowed).To(Equal(int32(0)))
			Expect(status.BlockReason).To(ContainSubstring("test-pdb blocks eviction"))
			Expect(status.BlockReason).To(ContainSubstring("disruptions allowed: 0"))
		})
	})

	Context("when PDB selector doesn't match pod", func() {
		It("should return CanDisrupt=true (no matching PDB)", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels:    map[string]string{"app": "test"},
				},
			}

			pdb := &policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-pdb",
					Namespace: "default",
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "other"},
					},
					MinAvailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 3},
				},
				Status: policyv1.PodDisruptionBudgetStatus{
					DisruptionsAllowed: 0,
					CurrentHealthy:     3,
					DesiredHealthy:     3,
					ExpectedPods:       3,
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(pod, pdb).
				Build()

			status, err := CheckPDBStatus(ctx, client, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(status).NotTo(BeNil())
			Expect(status.HasPDB).To(BeFalse())
			Expect(status.CanDisrupt).To(BeTrue())
		})
	})

	Context("when multiple PDBs match", func() {
		It("should use first matching PDB", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels:    map[string]string{"app": "test", "tier": "backend"},
				},
			}

			pdb1 := &policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "first-pdb",
					Namespace: "default",
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					MinAvailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 2},
				},
				Status: policyv1.PodDisruptionBudgetStatus{
					DisruptionsAllowed: 1,
					CurrentHealthy:     3,
					DesiredHealthy:     2,
					ExpectedPods:       3,
				},
			}

			pdb2 := &policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "second-pdb",
					Namespace: "default",
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"tier": "backend"},
					},
					MinAvailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 3},
				},
				Status: policyv1.PodDisruptionBudgetStatus{
					DisruptionsAllowed: 0,
					CurrentHealthy:     3,
					DesiredHealthy:     3,
					ExpectedPods:       3,
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(pod, pdb1, pdb2).
				Build()

			status, err := CheckPDBStatus(ctx, client, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(status).NotTo(BeNil())
			Expect(status.HasPDB).To(BeTrue())
			// Should use whichever PDB is first in the list
			Expect([]string{"first-pdb", "second-pdb"}).To(ContainElement(status.PDBName))
		})
	})
})

func TestCheckPDBStatus(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Eviction PDB Suite")
}
