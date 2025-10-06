/*
Copyright 2024 The Spotalis Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file ex		It("should return 200 OK", func() {
			// Clear any not ready state to ensure clean test
			healthChecker.ClearNotReady()
			healthChecker.ClearKubernetesUnavailable()

			response := performRequest(engine, "GET", "/readyz", nil)
			Expect(response.Code).To(Equal(http.StatusOK))

			var result map[string]interface{}
			err := parseJSONResponse(response, &result)
			Expect(err).NotTo(HaveOccurred()) compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var _ = Describe("HealthChecker", func() {
	var (
		healthChecker *HealthChecker
		fakeClient    *fake.Clientset
		mgr           manager.Manager
		namespace     string
		engine        *gin.Engine
		ctx           context.Context
		cancel        context.CancelFunc
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		namespace = "spotalis-system"
		fakeClient = fake.NewSimpleClientset()

		// Create a mock manager (we'll use nil for most tests)
		mgr = nil

		healthChecker = NewHealthChecker(mgr, fakeClient, namespace)
		engine = createTestEngine()
	})

	AfterEach(func() {
		if cancel != nil {
			cancel()
		}
	})

	Describe("NewHealthChecker", func() {
		It("should create a new health checker with correct configuration", func() {
			checker := NewHealthChecker(mgr, fakeClient, "test-namespace")
			Expect(checker).NotTo(BeNil())
			Expect(checker.namespace).To(Equal("test-namespace"))
			Expect(checker.kubeClient).To(Equal(fakeClient))
			Expect(checker.startTime).To(BeTemporally("~", time.Now(), time.Second))
		})
	})

	Describe("HealthzHandler", func() {
		BeforeEach(func() {
			engine.GET("/healthz", healthChecker.HealthzHandler)
		})

		Context("when the system is healthy", func() {
			It("should return 200 OK", func() {
				response := performRequest(engine, "GET", "/healthz", nil)
				Expect(response.Code).To(Equal(http.StatusOK))

				var result map[string]interface{}
				err := parseJSONResponse(response, &result)
				Expect(err).NotTo(HaveOccurred())
				Expect(result["status"]).To(Equal("healthy"))
				Expect(result).To(HaveKey("uptime"))
			})
		})

		Context("when the system is unhealthy", func() {
			BeforeEach(func() {
				healthChecker.SetUnhealthy("test failure reason")
			})

			It("should return 503 Service Unavailable", func() {
				response := performRequest(engine, "GET", "/healthz", nil)
				Expect(response.Code).To(Equal(http.StatusServiceUnavailable))

				var result map[string]interface{}
				err := parseJSONResponse(response, &result)
				Expect(err).NotTo(HaveOccurred())
				Expect(result["status"]).To(Equal("unhealthy"))
				Expect(result["reason"]).To(Equal("test failure reason"))
			})
		})

		Context("when Kubernetes is unavailable", func() {
			BeforeEach(func() {
				healthChecker.SetKubernetesUnavailable()
			})

			It("should return 503 Service Unavailable", func() {
				response := performRequest(engine, "GET", "/healthz", nil)
				Expect(response.Code).To(Equal(http.StatusServiceUnavailable))

				var result map[string]interface{}
				err := parseJSONResponse(response, &result)
				Expect(err).NotTo(HaveOccurred())
				Expect(result["status"]).To(Equal("unhealthy"))
				Expect(result["component"]).To(Equal("kubernetes-api"))
				Expect(result["error"]).To(Equal("kubernetes API marked as unavailable"))
			})
		})
	})

	Describe("ReadyzHandler", func() {
		BeforeEach(func() {
			engine.GET("/readyz", healthChecker.ReadyzHandler)
		})

		Context("when the system is ready", func() {
			BeforeEach(func() {
				// Create the required namespace for readiness check
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: namespace,
					},
				}
				_, err := fakeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
			})

			It("should return 200 OK", func() {
				response := performRequest(engine, "GET", "/readyz", nil)
				Expect(response.Code).To(Equal(http.StatusOK))

				var result map[string]interface{}
				err := parseJSONResponse(response, &result)
				Expect(err).NotTo(HaveOccurred())
				Expect(result["status"]).To(Equal("ready"))
			})
		})

		Context("when the system is not ready", func() {
			BeforeEach(func() {
				healthChecker.SetNotReady("initializing controllers")
			})

			It("should return 503 Service Unavailable", func() {
				response := performRequest(engine, "GET", "/readyz", nil)
				Expect(response.Code).To(Equal(http.StatusServiceUnavailable))

				var result map[string]interface{}
				err := parseJSONResponse(response, &result)
				Expect(err).NotTo(HaveOccurred())
				Expect(result["status"]).To(Equal("not ready"))

				// Check that the reason is in the checks
				checks, ok := result["checks"].(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(checks["manual-check"]).To(ContainSubstring("initializing controllers"))
			})
		})

		Context("when Kubernetes is unavailable", func() {
			BeforeEach(func() {
				healthChecker.SetKubernetesUnavailable()
			})

			It("should return 503 Service Unavailable", func() {
				response := performRequest(engine, "GET", "/readyz", nil)
				Expect(response.Code).To(Equal(http.StatusServiceUnavailable))

				var result map[string]interface{}
				err := parseJSONResponse(response, &result)
				Expect(err).NotTo(HaveOccurred())
				Expect(result["status"]).To(Equal("not ready"))

				// Check that Kubernetes is marked as unavailable in checks
				checks, ok := result["checks"].(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(checks["kubernetes-api"]).To(Equal("manually marked as unavailable"))
			})
		})
	})

	Describe("State Management", func() {
		Context("unhealthy state", func() {
			It("should set and clear unhealthy state", func() {
				engine.GET("/healthz", healthChecker.HealthzHandler)

				healthChecker.SetUnhealthy("test reason")

				response := performRequest(engine, "GET", "/healthz", nil)
				Expect(response.Code).To(Equal(http.StatusServiceUnavailable))

				healthChecker.ClearUnhealthy()

				response = performRequest(engine, "GET", "/healthz", nil)
				Expect(response.Code).To(Equal(http.StatusOK))
			})
		})

		Context("not ready state", func() {
			It("should set and clear not ready state", func() {
				// Create the required namespace for readiness check
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: namespace,
					},
				}
				_, err := fakeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				engine.GET("/readyz", healthChecker.ReadyzHandler)

				// Clear states to ensure clean starting point
				healthChecker.ClearNotReady()
				healthChecker.ClearKubernetesUnavailable()

				// Initially should be ready
				response := performRequest(engine, "GET", "/readyz", nil)
				Expect(response.Code).To(Equal(http.StatusOK))

				healthChecker.SetNotReady("test reason")

				response = performRequest(engine, "GET", "/readyz", nil)
				Expect(response.Code).To(Equal(http.StatusServiceUnavailable))

				healthChecker.ClearNotReady()

				response = performRequest(engine, "GET", "/readyz", nil)
				Expect(response.Code).To(Equal(http.StatusOK))
			})
		})

		Context("Kubernetes unavailable state", func() {
			It("should set and clear Kubernetes unavailable state", func() {
				// Create the required namespace for readiness check
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: namespace,
					},
				}
				_, err := fakeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				engine.GET("/healthz", healthChecker.HealthzHandler)
				engine.GET("/readyz", healthChecker.ReadyzHandler)

				// Clear states to ensure clean starting point
				healthChecker.ClearNotReady()
				healthChecker.ClearKubernetesUnavailable()

				// Initially should be ready
				readyResponse := performRequest(engine, "GET", "/readyz", nil)
				Expect(readyResponse.Code).To(Equal(http.StatusOK))

				healthChecker.SetKubernetesUnavailable()

				healthResponse := performRequest(engine, "GET", "/healthz", nil)
				Expect(healthResponse.Code).To(Equal(http.StatusServiceUnavailable))

				readyResponse = performRequest(engine, "GET", "/readyz", nil)
				Expect(readyResponse.Code).To(Equal(http.StatusServiceUnavailable))

				healthChecker.ClearKubernetesUnavailable()

				healthResponse = performRequest(engine, "GET", "/healthz", nil)
				Expect(healthResponse.Code).To(Equal(http.StatusOK))

				readyResponse = performRequest(engine, "GET", "/readyz", nil)
				Expect(readyResponse.Code).To(Equal(http.StatusOK))
			})
		})
	})

	Describe("Kubernetes API Checks", func() {
		Context("checkKubernetesAPI", func() {
			It("should succeed when Kubernetes API is available", func() {
				// Create a namespace to ensure the API is responsive
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-namespace",
					},
				}
				_, err := fakeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				err = healthChecker.checkKubernetesAPI(ctx)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("checkNamespaceAccess", func() {
			It("should succeed when namespace exists", func() {
				// Create the test namespace
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: namespace,
					},
				}
				_, err := fakeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())

				err = healthChecker.checkNamespaceAccess(ctx)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should fail when namespace does not exist", func() {
				err := healthChecker.checkNamespaceAccess(ctx)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not found"))
			})
		})
	})

	Describe("GetHealthzChecker", func() {
		It("should return a controller-runtime health checker", func() {
			checker := healthChecker.GetHealthzChecker()
			Expect(checker).NotTo(BeNil())

			// Test that the checker works with a clean state
			req, err := http.NewRequest("GET", "/healthz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())
			err = checker(req)
			Expect(err).NotTo(HaveOccurred())

			// Test that the checker fails when unhealthy
			healthChecker.SetUnhealthy("test error")
			err = checker(req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("test error"))
		})
	})

	Describe("GetReadyzChecker", func() {
		It("should return a controller-runtime readiness checker", func() {
			// Create the required namespace for readiness check
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}
			_, err := fakeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			// Clear states to ensure clean starting point
			healthChecker.ClearNotReady()
			healthChecker.ClearKubernetesUnavailable()

			checker := healthChecker.GetReadyzChecker()
			Expect(checker).NotTo(BeNil())

			// Test that the checker works with a clean state
			req, err := http.NewRequest("GET", "/readyz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())
			err = checker(req)
			Expect(err).NotTo(HaveOccurred())

			// Test that the checker fails when not ready
			healthChecker.SetNotReady("test error")
			err = checker(req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("test error"))
		})
	})
})

var _ = Describe("HealthHandler", func() {
	var (
		healthHandler *HealthHandler
		healthChecker *HealthChecker
		fakeClient    *fake.Clientset
		engine        *gin.Engine
		ctx           context.Context
		cancel        context.CancelFunc
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		fakeClient = fake.NewSimpleClientset()
		healthChecker = NewHealthChecker(nil, fakeClient, "test-namespace")
		healthHandler = NewHealthHandler()
		healthHandler.SetChecker(healthChecker)
		engine = createTestEngine()
	})

	AfterEach(func() {
		if cancel != nil {
			cancel()
		}
	})

	Describe("Healthz", func() {
		BeforeEach(func() {
			engine.GET("/healthz", healthHandler.Healthz)
		})

		It("should delegate to the health checker", func() {
			response := performRequest(engine, "GET", "/healthz", nil)
			Expect(response.Code).To(Equal(http.StatusOK))

			var result map[string]interface{}
			err := parseJSONResponse(response, &result)
			Expect(err).NotTo(HaveOccurred())
			Expect(result["status"]).To(Equal("healthy"))
		})
	})

	Describe("Readyz", func() {
		BeforeEach(func() {
			engine.GET("/readyz", healthHandler.Readyz)
		})

		It("should delegate to the health checker", func() {
			// Create the required namespace for readiness check
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
				},
			}
			_, err := fakeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			// Clear states to ensure clean starting point
			healthChecker.ClearNotReady()
			healthChecker.ClearKubernetesUnavailable()

			response := performRequest(engine, "GET", "/readyz", nil)
			Expect(response.Code).To(Equal(http.StatusOK))

			var result map[string]interface{}
			err = parseJSONResponse(response, &result)
			Expect(err).NotTo(HaveOccurred())
			Expect(result["status"]).To(Equal("ready"))
		})
	})

	Describe("SetUnhealthy", func() {
		It("should delegate to the health checker", func() {
			engine.GET("/healthz", healthHandler.Healthz)

			healthHandler.SetUnhealthy("test reason")

			response := performRequest(engine, "GET", "/healthz", nil)
			Expect(response.Code).To(Equal(http.StatusServiceUnavailable))
		})
	})
})
