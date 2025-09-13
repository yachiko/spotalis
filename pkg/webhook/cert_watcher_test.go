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
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CertificateWatcher", func() {
	var (
		ctx         context.Context
		cancel      context.CancelFunc
		tempDir     string
		certPath    string
		keyPath     string
		watcher     *CertificateWatcher
		reloadCount int
	)

	// Mock reload callback
	reloadCallback := func(cert tls.Certificate) {
		reloadCount++
		// Store certificate for potential future verification
		_ = cert
	}

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())
		reloadCount = 0

		// Create temporary directory for certificates
		var err error
		tempDir, err = ioutil.TempDir("", "cert-watcher-test-*")
		Expect(err).NotTo(HaveOccurred())

		certPath = filepath.Join(tempDir, "tls.crt")
		keyPath = filepath.Join(tempDir, "tls.key")

		// Create initial test certificates
		err = createValidTestCertificates(certPath, keyPath)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
	})

	Describe("NewCertificateWatcher", func() {
		It("should create a new certificate watcher", func() {
			watcher = NewCertificateWatcher(certPath, keyPath, reloadCallback)

			Expect(watcher).NotTo(BeNil())
			Expect(watcher.certPath).To(Equal(certPath))
			Expect(watcher.keyPath).To(Equal(keyPath))
			Expect(watcher.onReload).NotTo(BeNil())
			Expect(watcher.watcher).To(BeNil()) // Not started yet
		})

		It("should handle nil callback", func() {
			watcher = NewCertificateWatcher(certPath, keyPath, nil)

			Expect(watcher).NotTo(BeNil())
			Expect(watcher.onReload).To(BeNil())
		})

		It("should handle empty paths", func() {
			watcher = NewCertificateWatcher("", "", reloadCallback)

			Expect(watcher).NotTo(BeNil())
			Expect(watcher.certPath).To(BeEmpty())
			Expect(watcher.keyPath).To(BeEmpty())
		})
	})

	Describe("Start", func() {
		BeforeEach(func() {
			watcher = NewCertificateWatcher(certPath, keyPath, reloadCallback)
		})

		Context("with valid certificate files", func() {
			It("should start watching certificates", func() {
				// Start watcher in background
				done := make(chan error, 1)
				go func() {
					done <- watcher.Start(ctx)
				}()

				// Give watcher time to start
				time.Sleep(50 * time.Millisecond)

				// Cancel context to stop watcher
				cancel()

				// Wait for watcher to stop
				Eventually(done, 2*time.Second).Should(Receive(BeNil()))
			})
		})

		Context("with invalid certificate directory", func() {
			It("should return error for non-existent directory", func() {
				invalidPath := filepath.Join(tempDir, "nonexistent", "cert.pem")
				watcher = NewCertificateWatcher(invalidPath, keyPath, reloadCallback)

				err := watcher.Start(ctx)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("with context cancellation", func() {
			It("should stop when context is cancelled", func() {
				// Start watcher in background
				done := make(chan error, 1)
				go func() {
					done <- watcher.Start(ctx)
				}()

				// Give watcher time to start
				time.Sleep(50 * time.Millisecond)

				// Cancel context
				cancel()

				// Should stop gracefully
				Eventually(done, 2*time.Second).Should(Receive(BeNil()))
			})

			It("should stop immediately if context is already cancelled", func() {
				cancel() // Cancel before starting

				err := watcher.Start(ctx)
				Expect(err).To(BeNil()) // Should exit gracefully
			})
		})
	})

	Describe("reloadCertificate", func() {
		BeforeEach(func() {
			watcher = NewCertificateWatcher(certPath, keyPath, reloadCallback)
		})

		It("should handle missing certificate files", func() {
			// Remove certificate file
			os.Remove(certPath)

			err := watcher.reloadCertificate()
			Expect(err).To(HaveOccurred())
			Expect(reloadCount).To(Equal(0)) // Callback should not be called on error
		})

		It("should handle invalid certificate content", func() {
			// Write invalid certificate content
			err := ioutil.WriteFile(certPath, []byte("invalid cert"), 0644)
			Expect(err).NotTo(HaveOccurred())

			err = watcher.reloadCertificate()
			Expect(err).To(HaveOccurred())
			Expect(reloadCount).To(Equal(0))
		})
	})

	Describe("Error handling", func() {
		It("should handle file system watcher errors gracefully", func() {
			watcher = NewCertificateWatcher(certPath, keyPath, reloadCallback)

			// Start watcher in background
			done := make(chan error, 1)
			go func() {
				done <- watcher.Start(ctx)
			}()

			// Give watcher time to start
			time.Sleep(50 * time.Millisecond)

			// Remove the entire directory to trigger watcher errors
			os.RemoveAll(tempDir)

			// Wait a bit for error to be processed
			time.Sleep(200 * time.Millisecond)

			// Cancel context to stop watcher
			cancel()
			Eventually(done, 2*time.Second).Should(Receive(BeNil()))
		})
	})
})

// Helper functions for testing

func createValidTestCertificates(certPath, keyPath string) error {
	// For testing purposes, create placeholder files
	// In a real test environment, you'd generate actual certificates
	certContent := []byte("dummy cert content for testing")
	keyContent := []byte("dummy key content for testing")

	if err := ioutil.WriteFile(certPath, certContent, 0644); err != nil {
		return err
	}
	return ioutil.WriteFile(keyPath, keyContent, 0600)
}

func updateCertificateFile(certPath, keyPath string) error {
	// Create updated certificates (same content but triggers file system event)
	return createValidTestCertificates(certPath, keyPath)
}
