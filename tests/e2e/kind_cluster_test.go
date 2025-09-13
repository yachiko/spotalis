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

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

var _ = Describe("Kind Cluster Setup", func() {

	const (
		ClusterName = "spotalis-test"
		WaitTimeout = 5 * time.Minute
	)

	var (
		ctx       context.Context
		k8sClient client.Client
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Cluster Management", func() {
		It("should create a Kind cluster for testing", func() {
			By("Checking if Kind is available")
			_, err := exec.LookPath("kind")
			if err != nil {
				Skip("Kind not available in PATH")
			}

			By("Creating Kind cluster with custom configuration")
			cmd := exec.Command("kind", "create", "cluster",
				"--name", ClusterName,
				"--config", "/dev/stdin")

			// Kind cluster configuration for testing Spotalis
			kindConfig := `
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  image: kindest/node:v1.28.0
- role: worker
  image: kindest/node:v1.28.0
  labels:
    node.kubernetes.io/lifecycle: spot
    karpenter.sh/capacity-type: spot
- role: worker  
  image: kindest/node:v1.28.0
  labels:
    node.kubernetes.io/lifecycle: normal
    karpenter.sh/capacity-type: on-demand
- role: worker
  image: kindest/node:v1.28.0
  labels:
    node.kubernetes.io/lifecycle: spot
    karpenter.sh/capacity-type: spot
networking:
  disableDefaultCNI: false
  kubeProxyMode: "iptables"
`
			cmd.Stdin = bytes.NewBufferString(kindConfig)
			output, err := cmd.CombinedOutput()

			if err != nil {
				// Check if cluster already exists
				if bytes.Contains(output, []byte("already exists")) {
					By("Kind cluster already exists, continuing")
				} else {
					Fail(fmt.Sprintf("Failed to create Kind cluster: %s\nOutput: %s", err, output))
				}
			} else {
				By("Kind cluster created successfully")
			}

			By("Setting up kubeconfig for the cluster")
			cmd = exec.Command("kind", "export", "kubeconfig", "--name", ClusterName)
			output, err = cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), "Failed to export kubeconfig: %s", output)

			By("Creating Kubernetes client")
			cfg, err := config.GetConfig()
			Expect(err).ToNot(HaveOccurred())

			k8sClient, err = client.New(cfg, client.Options{})
			Expect(err).ToNot(HaveOccurred())

			By("Waiting for cluster to be ready")
			Eventually(func() bool {
				nodes := &corev1.NodeList{}
				err := k8sClient.List(ctx, nodes)
				if err != nil {
					return false
				}

				readyNodes := 0
				for _, node := range nodes.Items {
					for _, condition := range node.Status.Conditions {
						if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
							readyNodes++
							break
						}
					}
				}

				// Expect 4 nodes: 1 control-plane + 3 workers
				return readyNodes == 4
			}, WaitTimeout, 10*time.Second).Should(BeTrue())
		})

		It("should verify cluster has mixed node types for testing", func() {
			cfg, err := config.GetConfig()
			if err != nil {
				Skip("No kubeconfig available")
			}

			k8sClient, err := client.New(cfg, client.Options{})
			Expect(err).ToNot(HaveOccurred())

			By("Listing all nodes")
			nodes := &corev1.NodeList{}
			err = k8sClient.List(ctx, nodes)
			Expect(err).ToNot(HaveOccurred())

			By("Verifying node classifications")
			spotNodes := 0
			onDemandNodes := 0
			controlPlaneNodes := 0

			for _, node := range nodes.Items {
				labels := node.GetLabels()

				// Check if it's a control-plane node
				if _, isControlPlane := labels["node-role.kubernetes.io/control-plane"]; isControlPlane {
					controlPlaneNodes++
					continue
				}

				// Check node lifecycle
				if lifecycle, exists := labels["node.kubernetes.io/lifecycle"]; exists {
					switch lifecycle {
					case "spot":
						spotNodes++
					case "normal":
						onDemandNodes++
					}
				}

				// Log node information for debugging
				fmt.Printf("Node: %s, Labels: %v\n", node.Name, labels)
			}

			By(fmt.Sprintf("Found %d control-plane, %d spot, and %d on-demand nodes",
				controlPlaneNodes, spotNodes, onDemandNodes))

			Expect(controlPlaneNodes).To(Equal(1), "Should have exactly 1 control-plane node")
			Expect(spotNodes).To(BeNumerically(">=", 1), "Should have at least 1 spot node")
			Expect(onDemandNodes).To(BeNumerically(">=", 1), "Should have at least 1 on-demand node")
		})

		It("should create test namespace", func() {
			cfg, err := config.GetConfig()
			if err != nil {
				Skip("No kubeconfig available")
			}

			k8sClient, err := client.New(cfg, client.Options{})
			Expect(err).ToNot(HaveOccurred())

			By("Creating spotalis-system namespace")
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "spotalis-system",
				},
			}

			err = k8sClient.Create(ctx, namespace)
			if err != nil {
				// Namespace might already exist
				fmt.Printf("Namespace creation result: %v\n", err)
			}

			By("Creating test namespace")
			testNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "spotalis-test",
				},
			}

			err = k8sClient.Create(ctx, testNamespace)
			if err != nil {
				// Namespace might already exist
				fmt.Printf("Test namespace creation result: %v\n", err)
			}

			By("Verifying namespaces exist")
			var ns corev1.Namespace
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "spotalis-system"}, &ns)
			Expect(err).ToNot(HaveOccurred())

			err = k8sClient.Get(ctx, client.ObjectKey{Name: "spotalis-test"}, &ns)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should load Spotalis controller image to cluster", func() {
			By("Checking if Docker image exists")
			cmd := exec.Command("docker", "image", "inspect", "spotalis:test")
			err := cmd.Run()
			if err != nil {
				Skip("Spotalis Docker image not available. Build with: make docker-build")
			}

			By("Loading image into Kind cluster")
			cmd = exec.Command("kind", "load", "docker-image", "spotalis:test", "--name", ClusterName)
			output, err := cmd.CombinedOutput()
			if err != nil {
				fmt.Printf("Warning: Failed to load image into Kind cluster: %s\nOutput: %s\n", err, output)
			} else {
				By("Successfully loaded Spotalis image into cluster")
			}
		})
	})

	Describe("Cluster Cleanup", func() {
		It("should provide cleanup instructions", func() {
			By("Printing cleanup commands")
			fmt.Println("\n" + strings.Repeat("=", 60))
			fmt.Println("CLUSTER CLEANUP INSTRUCTIONS")
			fmt.Println(strings.Repeat("=", 60))
			fmt.Printf("To delete the test cluster:\n")
			fmt.Printf("  kind delete cluster --name %s\n", ClusterName)
			fmt.Println("\nTo see all Kind clusters:")
			fmt.Println("  kind get clusters")
			fmt.Println(strings.Repeat("=", 60))
		})
	})
})

// Helper function to delete cluster (optional, can be run manually)
var _ = AfterSuite(func() {
	if shouldCleanupCluster() {
		By("Cleaning up Kind cluster")
		cmd := exec.Command("kind", "delete", "cluster", "--name", "spotalis-test")
		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("Warning: Failed to cleanup cluster: %s\nOutput: %s\n", err, output)
		} else {
			fmt.Printf("Successfully deleted cluster: %s\n", "spotalis-test")
		}
	}
})

// Helper to determine if cleanup should happen
func shouldCleanupCluster() bool {
	// Set SPOTALIS_CLEANUP_CLUSTER=true to auto-cleanup
	return os.Getenv("SPOTALIS_CLEANUP_CLUSTER") == "true"
}
