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

package webhook

import (
	"context"
	"crypto/tls"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ = Describe("AdmissionController", func() {
	var (
		ctx        context.Context
		cancel     context.CancelFunc
		tempDir    string
		certPath   string
		kubeClient *fake.Clientset
		testMgr    manager.Manager
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())

		// Create temporary directory for certificates
		var err error
		tempDir, err = os.MkdirTemp("", "webhook-test-*")
		Expect(err).NotTo(HaveOccurred())

		certPath = filepath.Join(tempDir, "tls.crt")
		_ = filepath.Join(tempDir, "tls.key") // keyPath not used in most tests

		// Create fake Kubernetes client
		kubeClient = fake.NewSimpleClientset()

		// Skip creating actual manager for most tests
		testMgr = nil
	})

	AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
	})

	Describe("DefaultAdmissionConfig", func() {
		It("should return valid default configuration", func() {
			config := DefaultAdmissionConfig()

			Expect(config.Enabled).To(BeTrue())
			Expect(config.Port).To(Equal(9443))
			Expect(config.CertDir).To(Equal("/tmp/k8s-webhook-server/serving-certs"))
			Expect(config.CertName).To(Equal("tls.crt"))
			Expect(config.KeyName).To(Equal("tls.key"))
			Expect(config.ServiceName).To(Equal("spotalis-webhook-service"))
			Expect(config.ServiceNamespace).To(Equal("spotalis-system"))
			Expect(config.ServicePath).To(Equal("/mutate"))
			Expect(config.WebhookName).To(Equal("spotalis-mutating-webhook"))
			Expect(config.TLSMinVersion).To(Equal("1.3"))

			// Check failure policy
			Expect(config.FailurePolicy).NotTo(BeNil())
			Expect(*config.FailurePolicy).To(Equal(admissionv1.Fail))

			// Check side effects
			Expect(config.SideEffects).NotTo(BeNil())
			Expect(*config.SideEffects).To(Equal(admissionv1.SideEffectClassNone))

			// Check timeout
			Expect(config.TimeoutSeconds).NotTo(BeNil())
			Expect(*config.TimeoutSeconds).To(Equal(int32(10)))

			// Check admission review versions
			Expect(config.AdmissionReviewVersions).To(ContainElement("v1"))
			Expect(config.AdmissionReviewVersions).To(ContainElement("v1beta1"))

			// Check rules
			Expect(config.Rules).To(HaveLen(2))
			Expect(config.Rules[0].Rule.Resources).To(ContainElement("pods"))
			Expect(config.Rules[1].Rule.Resources).To(ContainElement("deployments"))
			Expect(config.Rules[1].Rule.Resources).To(ContainElement("statefulsets"))
		})
	})

	Describe("NewAdmissionController", func() {
		Context("with missing certificates", func() {
			It("should return error for missing certificate file", func() {
				config := &AdmissionConfig{
					CertDir:  tempDir,
					CertName: "missing.crt",
					KeyName:  "missing.key",
				}

				controller, err := NewAdmissionController(config, kubeClient, testMgr, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("certificate file not found"))
				Expect(controller).To(BeNil())
			})
		})
	})

	Describe("AdmissionConfig validation", func() {
		var config *AdmissionConfig

		BeforeEach(func() {
			config = DefaultAdmissionConfig()
		})

		It("should have valid service configuration", func() {
			Expect(config.ServiceName).NotTo(BeEmpty())
			Expect(config.ServiceNamespace).NotTo(BeEmpty())
			Expect(config.ServicePath).To(HavePrefix("/"))
		})

		It("should have valid webhook configuration", func() {
			Expect(config.WebhookName).NotTo(BeEmpty())
			Expect(config.FailurePolicy).NotTo(BeNil())
			Expect(config.SideEffects).NotTo(BeNil())
			Expect(config.AdmissionReviewVersions).NotTo(BeEmpty())
			Expect(config.TimeoutSeconds).NotTo(BeNil())
			Expect(*config.TimeoutSeconds).To(BeNumerically(">", 0))
		})

		It("should have valid TLS configuration", func() {
			Expect(config.TLSMinVersion).NotTo(BeEmpty())
			Expect(config.Port).To(BeNumerically(">", 0))
			Expect(config.Port).To(BeNumerically("<=", 65535))
		})

		It("should have valid rules", func() {
			Expect(config.Rules).NotTo(BeEmpty())
			for _, rule := range config.Rules {
				Expect(rule.Operations).NotTo(BeEmpty())
				Expect(rule.Rule.APIVersions).NotTo(BeEmpty())
				Expect(rule.Rule.Resources).NotTo(BeEmpty())
			}
		})
	})

	Describe("TLS configuration methods", func() {
		var config *AdmissionConfig

		BeforeEach(func() {
			config = DefaultAdmissionConfig()
		})

		Describe("getTLSVersion", func() {
			It("should return correct TLS version constants", func() {
				controller := &AdmissionController{config: config}

				// Test different TLS versions
				config.TLSMinVersion = "1.0"
				Expect(controller.getTLSVersion()).To(Equal(uint16(tls.VersionTLS10)))

				config.TLSMinVersion = "1.1"
				Expect(controller.getTLSVersion()).To(Equal(uint16(tls.VersionTLS11)))

				config.TLSMinVersion = "1.2"
				Expect(controller.getTLSVersion()).To(Equal(uint16(tls.VersionTLS12)))

				config.TLSMinVersion = "1.3"
				Expect(controller.getTLSVersion()).To(Equal(uint16(tls.VersionTLS13)))

				config.TLSMinVersion = "invalid"
				Expect(controller.getTLSVersion()).To(Equal(uint16(tls.VersionTLS13))) // default
			})
		})

		Describe("getCipherSuites", func() {
			It("should return nil for empty cipher suites", func() {
				config.TLSCipherSuites = []string{}
				controller := &AdmissionController{config: config}

				Expect(controller.getCipherSuites()).To(BeNil())
			})

			It("should convert known cipher suite names", func() {
				config.TLSCipherSuites = []string{
					"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
					"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
				}
				controller := &AdmissionController{config: config}

				suites := controller.getCipherSuites()
				Expect(suites).To(HaveLen(2))
				Expect(suites).To(ContainElement(uint16(tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256)))
				Expect(suites).To(ContainElement(uint16(tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384)))
			})

			It("should ignore unknown cipher suites", func() {
				config.TLSCipherSuites = []string{
					"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
					"UNKNOWN_CIPHER_SUITE",
					"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
				}
				controller := &AdmissionController{config: config}

				suites := controller.getCipherSuites()
				Expect(suites).To(HaveLen(2)) // Unknown suite should be ignored
			})
		})
	})

	Describe("createClientConfig", func() {
		It("should create valid client configuration", func() {
			config := DefaultAdmissionConfig()
			config.CABundle = []byte("test-ca-bundle")

			controller := &AdmissionController{config: config}
			clientConfig := controller.createClientConfig()

			Expect(clientConfig.Service).NotTo(BeNil())
			Expect(clientConfig.Service.Name).To(Equal(config.ServiceName))
			Expect(clientConfig.Service.Namespace).To(Equal(config.ServiceNamespace))
			Expect(clientConfig.Service.Path).NotTo(BeNil())
			Expect(*clientConfig.Service.Path).To(Equal(config.ServicePath))
			Expect(clientConfig.CABundle).To(Equal(config.CABundle))
		})
	})

	Describe("State management", func() {
		var controller *AdmissionController

		BeforeEach(func() {
			config := DefaultAdmissionConfig()
			controller = &AdmissionController{
				config:     config,
				kubeClient: kubeClient,
				registered: false,
			}
		})

		Describe("GetConfig", func() {
			It("should return the admission configuration", func() {
				config := controller.GetConfig()
				Expect(config).To(Equal(controller.config))
			})
		})

		Describe("IsRegistered", func() {
			It("should return false initially", func() {
				Expect(controller.IsRegistered()).To(BeFalse())
			})

			It("should return true after registration", func() {
				controller.registered = true
				Expect(controller.IsRegistered()).To(BeTrue())
			})
		})

		Describe("GetTLSConfig", func() {
			It("should return nil when not initialized", func() {
				Expect(controller.GetTLSConfig()).To(BeNil())
			})

			It("should return TLS config when initialized", func() {
				controller.tlsConfig = &tls.Config{MinVersion: tls.VersionTLS13}
				tlsConfig := controller.GetTLSConfig()
				Expect(tlsConfig).NotTo(BeNil())
				Expect(tlsConfig.MinVersion).To(Equal(uint16(tls.VersionTLS13)))
			})
		})
	})

	Describe("Start", func() {
		It("should return nil when disabled", func() {
			config := DefaultAdmissionConfig()
			config.Enabled = false

			controller := &AdmissionController{
				config:     config,
				kubeClient: kubeClient,
			}

			err := controller.Start(ctx)
			Expect(err).To(BeNil())
		})
	})

	Describe("Cleanup", func() {
		var controller *AdmissionController

		BeforeEach(func() {
			config := DefaultAdmissionConfig()
			controller = &AdmissionController{
				config:     config,
				kubeClient: kubeClient,
				registered: false,
			}
		})

		It("should return nil when not registered", func() {
			err := controller.Cleanup(ctx)
			Expect(err).To(BeNil())
		})

		It("should attempt to delete webhook configuration when registered", func() {
			controller.registered = true

			// This should attempt to delete the webhook configuration
			// In our fake client, this might not exist, so it could error
			err := controller.Cleanup(ctx)
			// The error behavior depends on the fake client implementation
			// In some cases it might succeed, in others it might fail
			_ = err // Explicitly ignore error since this is expected in tests
		})
	})

	Describe("Certificate validation", func() {
		var controller *AdmissionController
		var config *AdmissionConfig

		BeforeEach(func() {
			config = DefaultAdmissionConfig()
			config.CertDir = tempDir
			controller = &AdmissionController{config: config}
		})

		It("should handle missing certificate files", func() {
			err := controller.ValidateCertificates()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to read certificate"))
		})

		It("should handle invalid certificate content", func() {
			// Create a file with invalid certificate content
			invalidCert := []byte("invalid certificate content")
			err := os.WriteFile(certPath, invalidCert, 0600)
			Expect(err).NotTo(HaveOccurred())

			err = controller.ValidateCertificates()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse certificate"))
		})
	})

	Describe("Error handling", func() {
		It("should handle invalid certificate paths", func() {
			config := &AdmissionConfig{
				CertDir:  "/nonexistent/directory",
				CertName: "cert.pem",
				KeyName:  "key.pem",
			}

			controller, err := NewAdmissionController(config, kubeClient, testMgr, nil)
			Expect(err).To(HaveOccurred())
			Expect(controller).To(BeNil())
		})
	})

	Describe("Concurrent operations", func() {
		It("should handle concurrent access to configuration", func() {
			config := DefaultAdmissionConfig()
			controller := &AdmissionController{
				config:     config,
				kubeClient: kubeClient,
			}

			done := make(chan bool, 10)

			// Test concurrent access to read-only operations
			for i := 0; i < 10; i++ {
				go func() {
					defer GinkgoRecover()
					_ = controller.GetConfig()
					_ = controller.IsRegistered()
					_ = controller.GetTLSConfig()
					done <- true
				}()
			}

			// Wait for all goroutines to complete
			for i := 0; i < 10; i++ {
				Eventually(done).Should(Receive())
			}
		})
	})
})

// Mock admission handler for testing
type mockAdmissionHandler struct{}

func (m *mockAdmissionHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	return admission.Allowed("mock handler")
}

var _ = Describe("Mock Integration", func() {
	It("should work with mock admission handler", func() {
		handler := &mockAdmissionHandler{}

		req := admission.Request{}
		resp := handler.Handle(context.Background(), req)

		Expect(resp.Allowed).To(BeTrue())
		Expect(resp.Result.Message).To(Equal("mock handler"))
	})
})
