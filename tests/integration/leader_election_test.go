//go:build integration

package integration

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/ahoma/spotalis/pkg/operator"
)

var _ = Describe("Leader Election Integration", func() {
	var (
		ctx       context.Context
		cancel    context.CancelFunc
		namespace string
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 15*time.Minute)
		namespace = fmt.Sprintf("spotalis-test-%s", generateRandomSuffix())

		// Create test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		// Wait for namespace to be ready
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: namespace}, &corev1.Namespace{})
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		GinkgoWriter.Printf("Using test namespace: %s\n", namespace)
	})

	AfterEach(func() {
		if namespace != "" {
			// Best effort cleanup
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
			_ = k8sClient.Delete(context.Background(), ns)
		}
		if cancel != nil {
			cancel()
		}
	})

	Describe("Basic Leader Election", func() {
		It("should elect a single leader from multiple operators", func() {
			Skip("Test skipped - leader election testing not suitable for Kind cluster environment")
			// Create two operators with same election ID
			config1 := &operator.OperatorConfig{
				LeaderElection:   true,
				LeaderElectionID: "spotalis-leader-election-test",
				Namespace:        "kube-system",
				LogLevel:         "info", // Reduce log noise
				EnableWebhook:    false,  // Disable webhook to simplify
				MetricsAddr:      ":0",   // Disable metrics server
				ProbeAddr:        ":0",   // Disable health probes
			}

			config2 := &operator.OperatorConfig{
				LeaderElection:   true,
				LeaderElectionID: "spotalis-leader-election-test",
				Namespace:        "kube-system",
				LogLevel:         "info", // Reduce log noise
				EnableWebhook:    false,  // Disable webhook to simplify
				MetricsAddr:      ":0",   // Disable metrics server
				ProbeAddr:        ":0",   // Disable health probes
			}

			operator1, err := operator.NewOperator(config1)
			Expect(err).NotTo(HaveOccurred())

			operator2, err := operator.NewOperator(config2)
			Expect(err).NotTo(HaveOccurred())

			// Start operators in background with shorter context for faster cleanup
			ctx1, cancel1 := context.WithTimeout(ctx, 60*time.Second)
			defer cancel1()
			ctx2, cancel2 := context.WithTimeout(ctx, 60*time.Second)
			defer cancel2()

			go startOperator(operator1, ctx1)
			go startOperator(operator2, ctx2)

			// Give some time for startup
			time.Sleep(5 * time.Second)

			// Wait for leader election to stabilize - exactly one leader
			Eventually(func() bool {
				leader1 := operator1.IsLeader()
				leader2 := operator2.IsLeader()
				GinkgoWriter.Printf("Operator1 leader: %v, Operator2 leader: %v\n", leader1, leader2)

				// Count leaders
				leaderCount := 0
				if leader1 {
					leaderCount++
				}
				if leader2 {
					leaderCount++
				}

				// We want exactly one leader
				return leaderCount == 1
			}, 120*time.Second, 5*time.Second).Should(BeTrue(), "Exactly one operator should become leader")

			// Double-check that we have exactly one leader
			leaderCount := 0
			if operator1.IsLeader() {
				leaderCount++
			}
			if operator2.IsLeader() {
				leaderCount++
			}
			Expect(leaderCount).To(Equal(1), "Should have exactly one leader")

			// Verify lease exists
			Eventually(func() error {
				lease := &coordinationv1.Lease{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "spotalis-leader-election-test",
					Namespace: "kube-system",
				}, lease)
			}, 60*time.Second, 2*time.Second).Should(Succeed(), "Lease should exist")

			GinkgoWriter.Printf("✓ Basic leader election test passed\n")
		})
	})
})

func startOperator(op *operator.Operator, ctx context.Context) {
	defer GinkgoRecover()
	err := op.Start(ctx)
	if err != nil && err != context.Canceled {
		Fail(fmt.Sprintf("Operator failed: %v", err))
	}
}
