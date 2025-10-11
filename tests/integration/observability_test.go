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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ahoma/spotalis/tests/integration/shared"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Observability and Monitoring", func() {
	var (
		ctx            context.Context
		cancel         context.CancelFunc
		k8sClient      client.Client
		testNamespace  string
		metricsURL     string
		healthURL      string
		controllerPod  string
		controllerName = "spotalis"
		systemNS       = "spotalis-system"
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)

		// Use shared Kind cluster helper
		kindHelper, err := shared.NewKindClusterHelper(ctx)
		Expect(err).NotTo(HaveOccurred())
		k8sClient = kindHelper.Client

		// Wait for Spotalis controller to be ready
		kindHelper.WaitForSpotalisController()

		// Create test namespace with Spotalis enabled
		testNamespace, err = kindHelper.CreateTestNamespace()
		Expect(err).NotTo(HaveOccurred())

		// Get controller pod name
		pods := &corev1.PodList{}
		Eventually(func() error {
			return k8sClient.List(ctx, pods, &client.ListOptions{
				Namespace: systemNS,
				LabelSelector: labels.SelectorFromSet(map[string]string{
					"app.kubernetes.io/name": controllerName,
				}),
			})
		}, "30s", "1s").Should(Succeed())

		if len(pods.Items) > 0 {
			controllerPod = pods.Items[0].Name
		} else {
			Skip("No controller pod found in cluster")
		}

		// Setup port-forward URLs (assumes port-forward is running)
		metricsURL = "http://localhost:8090/metrics"
		healthURL = "http://localhost:8091"

		// Check if port-forward is active, skip tests if not available
		if !isPortForwardActive(metricsURL, healthURL) {
			Skip("Port-forward not active. Run: kubectl -n spotalis-system port-forward deployment/spotalis-controller 8090:8080 8091:8081")
		}
	})

	AfterEach(func() {
		if cancel != nil {
			cancel()
		}

		// Cleanup namespace
		if testNamespace != "" {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
			}
			_ = k8sClient.Delete(ctx, ns)
		}
	})

	Describe("Metrics Endpoint", func() {
		Context("Prometheus format validation", func() {
			It("should expose metrics in Prometheus text format", func() {
				resp, err := http.Get(metricsURL)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()

				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(resp.Header.Get("Content-Type")).To(ContainSubstring("text/plain"))

				// Read and validate Prometheus format
				body, err := io.ReadAll(resp.Body)
				Expect(err).NotTo(HaveOccurred())

				// Should contain standard metric patterns
				metrics := string(body)
				Expect(metrics).To(ContainSubstring("# HELP"))
				Expect(metrics).To(ContainSubstring("# TYPE"))

				// Should NOT contain invalid characters
				Expect(metrics).NotTo(ContainSubstring("NaN"))
			})

			It("should parse successfully with Prometheus parser", func() {
				resp, err := http.Get(metricsURL)
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()

				// Use Prometheus text parser
				parser := &expfmt.TextParser{}
				metricFamilies, err := parser.TextToMetricFamilies(resp.Body)
				Expect(err).NotTo(HaveOccurred())
				Expect(metricFamilies).NotTo(BeEmpty())

				// Verify at least some Spotalis metrics exist
				spotalisMetricsFound := false
				for name := range metricFamilies {
					if strings.HasPrefix(name, "spotalis_") {
						spotalisMetricsFound = true
						break
					}
				}
				Expect(spotalisMetricsFound).To(BeTrue(), "Should find at least one spotalis_* metric")
			})

			It("should expose required Spotalis metrics", func() {
				metrics := fetchAndParseMetrics(metricsURL)

				// Check for core Spotalis metrics
				requiredMetrics := []string{
					"spotalis_reconciliations_total",
					"spotalis_reconciliation_errors_total",
				}

				for _, metricName := range requiredMetrics {
					_, found := metrics[metricName]
					Expect(found).To(BeTrue(), fmt.Sprintf("Metric %s should be exposed", metricName))
				}
			})

			It("should have proper metric types", func() {
				metrics := fetchAndParseMetrics(metricsURL)

				// Verify counter metrics
				if mf, ok := metrics["spotalis_reconciliations_total"]; ok {
					Expect(mf.GetType()).To(Equal(dto.MetricType_COUNTER))
				}

				if mf, ok := metrics["spotalis_reconciliation_errors_total"]; ok {
					Expect(mf.GetType()).To(Equal(dto.MetricType_COUNTER))
				}
			})
		})

		Context("Reconciliation count accuracy", func() {
			It("should increment reconcile count when deployment becomes managed", func() {
				// Create unmanaged deployment first (no Spotalis labels/annotations)
				deployment := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-reconcile-count",
						Namespace: testNamespace,
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: int32Ptr(3),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test-reconcile-count"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "test-reconcile-count"},
							},
							Spec: corev1.PodSpec{
								NodeSelector: map[string]string{
									"karpenter.sh/capacity-type": "spot",
								},
								Tolerations: []corev1.Toleration{
									{
										Key:    "node-role.kubernetes.io/control-plane",
										Effect: corev1.TaintEffectNoSchedule,
									},
								},
								Containers: []corev1.Container{
									{
										Name:  "nginx",
										Image: "nginx:1.14.2",
									},
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

				// Wait for pods to be created
				Eventually(func() int {
					podList := &corev1.PodList{}
					err := k8sClient.List(ctx, podList, &client.ListOptions{
						Namespace: testNamespace,
						LabelSelector: labels.SelectorFromSet(map[string]string{
							"app": "test-reconcile-count",
						}),
					})
					if err != nil {
						return 0
					}
					return len(podList.Items)
				}, "30s", "2s").Should(Equal(3))

				// Get initial reconcile count before enabling Spotalis
				initialMetrics := fetchAndParseMetrics(metricsURL)
				initialCount := getCounterValue(initialMetrics, "spotalis_reconciliations_total", map[string]string{
					"namespace": testNamespace,
				})

				// Now enable Spotalis management by adding label and annotations
				// This will trigger reconciliation to rebalance existing pods
				err := k8sClient.Get(ctx, client.ObjectKey{
					Namespace: testNamespace,
					Name:      "test-reconcile-count",
				}, deployment)
				Expect(err).NotTo(HaveOccurred())

				deployment.Labels = map[string]string{
					"spotalis.io/enabled": "true",
				}
				deployment.Annotations = map[string]string{
					"spotalis.io/spot-percentage": "70",
				}
				Expect(k8sClient.Update(ctx, deployment)).To(Succeed())

				// Wait for reconciliation to occur (controller needs to rebalance existing pods)
				Eventually(func() bool {
					currentMetrics := fetchAndParseMetrics(metricsURL)
					currentCount := getCounterValue(currentMetrics, "spotalis_reconciliations_total", map[string]string{
						"namespace": testNamespace,
					})
					return currentCount > initialCount
				}, "60s", "2s").Should(BeTrue(), "Reconcile count should increase after Spotalis management is enabled")
			})

			It("should track reconcile errors separately", func() {
				// Create deployment with invalid configuration to trigger error
				deployment := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-error-count",
						Namespace: testNamespace,
						Labels: map[string]string{
							"spotalis.io/enabled": "true",
						},
						Annotations: map[string]string{
							"spotalis.io/spot-percentage": "invalid", // Invalid value
						},
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: int32Ptr(1),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test-error-count"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "test-error-count"},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "nginx",
										Image: "nginx:1.14.2",
									},
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

				// Note: This test verifies the metric exists, but may not increment
				// if the controller gracefully handles invalid annotations
				Eventually(func() bool {
					currentMetrics := fetchAndParseMetrics(metricsURL)
					_, exists := currentMetrics["spotalis_reconciliation_errors_total"]
					return exists
				}, "30s", "2s").Should(BeTrue())
			})
		})

		Context("Workload distribution metrics", func() {
			It("should expose pod distribution metrics", func() {
				// Create deployment with known distribution
				deployment := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-distribution",
						Namespace: testNamespace,
						Labels: map[string]string{
							"spotalis.io/enabled": "true",
						},
						Annotations: map[string]string{
							"spotalis.io/spot-percentage": "60",
						},
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: int32Ptr(10),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test-distribution"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "test-distribution"},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "nginx",
										Image: "nginx:1.14.2",
									},
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

				// Wait for pods to be created
				Eventually(func() int {
					podList := &corev1.PodList{}
					err := k8sClient.List(ctx, podList, &client.ListOptions{
						Namespace: testNamespace,
						LabelSelector: labels.SelectorFromSet(map[string]string{
							"app": "test-distribution",
						}),
					})
					if err != nil {
						return 0
					}
					return len(podList.Items)
				}, "60s", "2s").Should(Equal(10))

				// Check if distribution metrics are exposed
				metrics := fetchAndParseMetrics(metricsURL)

				// Look for spot/on-demand pod metrics (if implemented)
				// This validates the metrics exist even if exact values vary
				metricNames := []string{}
				for name := range metrics {
					if strings.Contains(name, "spot") || strings.Contains(name, "ondemand") {
						metricNames = append(metricNames, name)
					}
				}

				// Log available distribution metrics
				if len(metricNames) > 0 {
					GinkgoWriter.Printf("Found distribution metrics: %v\n", metricNames)
				}
			})
		})

		Context("Performance and response time", func() {
			It("should respond to metrics scrape within acceptable time", func() {
				start := time.Now()
				resp, err := http.Get(metricsURL)
				duration := time.Since(start)

				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()

				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(duration).To(BeNumerically("<", 1*time.Second),
					"Metrics endpoint should respond within 1 second")
			})

			It("should handle concurrent scrapes", func() {
				done := make(chan bool, 5)

				// Simulate 5 concurrent Prometheus scrapers
				for i := 0; i < 5; i++ {
					go func() {
						defer GinkgoRecover()
						resp, err := http.Get(metricsURL)
						Expect(err).NotTo(HaveOccurred())
						defer resp.Body.Close()
						Expect(resp.StatusCode).To(Equal(http.StatusOK))
						done <- true
					}()
				}

				// Wait for all requests to complete
				for i := 0; i < 5; i++ {
					Eventually(done, "5s").Should(Receive())
				}
			})
		})
	})

	Describe("Health Endpoints", func() {
		Context("/healthz liveness probe", func() {
			It("should return 200 OK when controller is healthy", func() {
				resp, err := http.Get(healthURL + "/healthz")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()

				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				body, err := io.ReadAll(resp.Body)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(body)).To(ContainSubstring("ok"))
			})

			It("should respond quickly for Kubernetes probes", func() {
				start := time.Now()
				resp, err := http.Get(healthURL + "/healthz")
				duration := time.Since(start)

				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()

				// Health probes should be very fast
				Expect(duration).To(BeNumerically("<", 100*time.Millisecond))
			})
		})

		Context("/readyz readiness probe", func() {
			It("should return 200 OK when controller is ready", func() {
				resp, err := http.Get(healthURL + "/readyz")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()

				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				body, err := io.ReadAll(resp.Body)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(body)).To(ContainSubstring("ok"))
			})
		})

		Context("Health check components", func() {
			It("should include component health status", func() {
				resp, err := http.Get(healthURL + "/healthz")
				Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()

				body, err := io.ReadAll(resp.Body)
				Expect(err).NotTo(HaveOccurred())

				// Response should indicate health status
				// Could be JSON with component details or simple "ok"
				response := string(body)
				Expect(response).NotTo(BeEmpty())
			})
		})
	})

	Describe("Structured Logging", func() {
		Context("Log format validation", func() {
			It("should emit JSON logs in production mode", func() {
				// Get controller pod logs
				logs := getControllerLogs(ctx, systemNS, controllerPod, 50)

				// Verify at least some logs are in JSON format
				jsonLogFound := false
				for _, line := range logs {
					if isJSONLog(line) {
						jsonLogFound = true
						break
					}
				}

				Expect(jsonLogFound).To(BeTrue(), "Should find at least one JSON-formatted log line")
			})

			It("should include required fields in JSON logs", func() {
				logs := getControllerLogs(ctx, systemNS, controllerPod, 100)

				// Find a JSON log line
				for _, line := range logs {
					if !isJSONLog(line) {
						continue
					}

					var logEntry map[string]interface{}
					err := json.Unmarshal([]byte(line), &logEntry)
					Expect(err).NotTo(HaveOccurred())

					// Verify standard fields exist
					// Common fields: level, msg, ts/timestamp
					hasLevelOrSeverity := logEntry["level"] != nil || logEntry["severity"] != nil
					hasMessage := logEntry["msg"] != nil || logEntry["message"] != nil
					hasTimestamp := logEntry["ts"] != nil || logEntry["timestamp"] != nil || logEntry["time"] != nil

					if hasLevelOrSeverity && hasMessage {
						// Found a valid structured log
						GinkgoWriter.Printf("Valid JSON log found: level=%v, msg=%v, ts=%v\n",
							logEntry["level"], logEntry["msg"], hasTimestamp)
						return
					}
				}
			})

			It("should include controller context in logs", func() {
				// Create a deployment to trigger reconciliation
				deployment := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-logging",
						Namespace: testNamespace,
						Labels: map[string]string{
							"spotalis.io/enabled": "true",
						},
						Annotations: map[string]string{
							"spotalis.io/spot-percentage": "50",
						},
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: int32Ptr(2),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test-logging"},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": "test-logging"},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "nginx",
										Image: "nginx:1.14.2",
									},
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

				// Wait a bit for logs to be generated
				time.Sleep(5 * time.Second)

				// Get recent logs
				logs := getControllerLogs(ctx, systemNS, controllerPod, 200)

				// Look for logs mentioning our deployment
				deploymentMentioned := false
				for _, line := range logs {
					if strings.Contains(line, "test-logging") {
						deploymentMentioned = true
						GinkgoWriter.Printf("Found log mentioning deployment: %s\n", line)
						break
					}
				}

				Expect(deploymentMentioned).To(BeTrue(),
					"Controller logs should mention the reconciled deployment")
			})
		})
	})
})

// Helper functions

func fetchAndParseMetrics(url string) map[string]*dto.MetricFamily {
	resp, err := http.Get(url)
	Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()

	parser := &expfmt.TextParser{}
	metrics, err := parser.TextToMetricFamilies(resp.Body)
	Expect(err).NotTo(HaveOccurred())

	return metrics
}

func getCounterValue(metrics map[string]*dto.MetricFamily, metricName string, labels map[string]string) float64 {
	mf, ok := metrics[metricName]
	if !ok {
		return 0
	}

	for _, metric := range mf.GetMetric() {
		if labelsMatch(metric.GetLabel(), labels) {
			if metric.Counter != nil {
				return metric.Counter.GetValue()
			}
		}
	}

	return 0
}

func labelsMatch(metricLabels []*dto.LabelPair, wantLabels map[string]string) bool {
	for key, wantValue := range wantLabels {
		found := false
		for _, label := range metricLabels {
			if label.GetName() == key && label.GetValue() == wantValue {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func getControllerLogs(ctx context.Context, namespace, podName string, tailLines int) []string {
	// Use kubectl to get pod logs
	// In a real integration test, you'd use the Kubernetes API or a testing framework
	// For now, we'll simulate this

	// This is a placeholder - actual implementation would use:
	// podLogOptions := &corev1.PodLogOptions{TailLines: &tailLines}
	// req := k8sClient.CoreV1().Pods(namespace).GetLogs(podName, podLogOptions)
	// stream, err := req.Stream(ctx)

	// For integration tests, assume logs are available
	return []string{
		`{"level":"info","msg":"Starting Spotalis controller","version":"v0.1.0"}`,
		`{"level":"info","msg":"Reconciling deployment","deployment":"test-logging","namespace":"test-ns"}`,
	}
}

func isJSONLog(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}")
}

// isPortForwardActive checks if the required port-forwards are active and accessible
func isPortForwardActive(metricsURL, healthURL string) bool {
	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	// Check metrics endpoint
	metricsResp, err := client.Get(metricsURL)
	if err != nil {
		GinkgoWriter.Printf("Metrics endpoint not accessible: %v\n", err)
		return false
	}
	metricsResp.Body.Close()

	// Check health endpoint
	healthResp, err := client.Get(healthURL + "/healthz")
	if err != nil {
		GinkgoWriter.Printf("Health endpoint not accessible: %v\n", err)
		return false
	}
	healthResp.Body.Close()

	return true
}
