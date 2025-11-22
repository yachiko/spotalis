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

package operator

import (
	"time"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/yachiko/spotalis/internal/server"
	"github.com/yachiko/spotalis/pkg/metrics"
	"k8s.io/client-go/kubernetes/fake"
)

var _ = Describe("Operator Getters and State Methods", func() {
	var (
		testOp     *Operator
		testConfig *Config
	)

	BeforeEach(func() {
		testConfig = &Config{
			MetricsAddr:             ":8080",
			ProbeAddr:               ":8081",
			WebhookAddr:             ":9443",
			LeaderElection:          false,
			LeaderElectionID:        "test-leader",
			Namespace:               "test-ns",
			ReconcileInterval:       10 * time.Second,
			MaxConcurrentReconciles: 5,
			WebhookCertDir:          "/tmp/certs",
			WebhookCertName:         "tls.crt",
			WebhookKeyName:          "tls.key",
			WebhookPort:             9443,
			LogLevel:                "info",
			EnablePprof:             false,
			EnableWebhook:           false,
			ReadOnlyMode:            false,
			NamespaceFilter:         []string{"ns1", "ns2"},
			APIQPSLimit:             50.0,
			APIBurstLimit:           100,
		}

		testOp = &Operator{
			config:     testConfig,
			namespace:  testConfig.Namespace,
			kubeClient: fake.NewSimpleClientset(),
			started:    false,
		}
	})

	Describe("GetConfig()", func() {
		It("should return the operator configuration", func() {
			config := testOp.GetConfig()
			Expect(config).NotTo(BeNil())
			Expect(config.MetricsAddr).To(Equal(":8080"))
			Expect(config.Namespace).To(Equal("test-ns"))
			Expect(config.LeaderElectionID).To(Equal("test-leader"))
		})

		It("should return same instance across multiple calls", func() {
			config1 := testOp.GetConfig()
			config2 := testOp.GetConfig()
			Expect(config1).To(BeIdenticalTo(config2))
		})
	})

	Describe("IsReady()", func() {
		Context("when operator is not started", func() {
			It("should return false", func() {
				Expect(testOp.IsReady()).To(BeFalse())
			})
		})

		Context("when operator is started", func() {
			BeforeEach(func() {
				testOp.started = true
			})

			It("should return true", func() {
				Expect(testOp.IsReady()).To(BeTrue())
			})
		})

		Context("when operator started then stopped", func() {
			BeforeEach(func() {
				testOp.started = true
			})

			It("should return false after stopping", func() {
				testOp.started = false
				Expect(testOp.IsReady()).To(BeFalse())
			})
		})
	})

	Describe("GetID()", func() {
		Context("without leader election manager", func() {
			It("should return 'unknown'", func() {
				Expect(testOp.GetID()).To(Equal("unknown"))
			})
		})

		Context("with leader election manager", func() {
			BeforeEach(func() {
				testOp.leaderElectionManager = &LeaderElectionManager{
					config: &LeaderElectionConfig{
						Identity: "test-operator-pod-123",
					},
				}
			})

			It("should return identity from leader election manager", func() {
				Expect(testOp.GetID()).To(Equal("test-operator-pod-123"))
			})
		})
	})

	Describe("IsLeader()", func() {
		Context("without leader election manager", func() {
			It("should return true (single instance leader)", func() {
				Expect(testOp.IsLeader()).To(BeTrue())
			})
		})

		Context("with leader election manager", func() {
			BeforeEach(func() {
				testOp.leaderElectionManager = &LeaderElectionManager{
					config: &LeaderElectionConfig{
						Identity: "test-pod",
					},
				}
			})

			It("should return false when not leader", func() {
				testOp.leaderElectionManager.isLeader = false
				Expect(testOp.IsLeader()).To(BeFalse())
			})

			It("should return true when is leader", func() {
				testOp.leaderElectionManager.isLeader = true
				Expect(testOp.IsLeader()).To(BeTrue())
			})
		})
	})

	Describe("IsFollower()", func() {
		Context("without leader election manager", func() {
			It("should return false (single instance)", func() {
				Expect(testOp.IsFollower()).To(BeFalse())
			})
		})

		Context("with leader election manager", func() {
			BeforeEach(func() {
				testOp.started = true // IsFollower requires IsReady() to be true
				testOp.leaderElectionManager = &LeaderElectionManager{
					config: &LeaderElectionConfig{
						Identity: "test-pod",
					},
				}
			})

			It("should return true when not leader", func() {
				testOp.leaderElectionManager.isLeader = false
				Expect(testOp.IsFollower()).To(BeTrue())
			})

			It("should return false when is leader", func() {
				testOp.leaderElectionManager.isLeader = true
				Expect(testOp.IsFollower()).To(BeFalse())
			})
		})
	})

	Describe("GetHealthStatus()", func() {
		Context("when operator is not ready", func() {
			It("should return not ready status", func() {
				status := testOp.GetHealthStatus()
				Expect(status.Leadership).To(Equal("leader")) // lowercase
				Expect(status.Status).To(Equal("unhealthy"))  // unhealthy when not ready
			})
		})

		Context("when operator is ready without leader election", func() {
			BeforeEach(func() {
				testOp.started = true
			})

			It("should return ready leader status", func() {
				status := testOp.GetHealthStatus()
				Expect(status.Leadership).To(Equal("leader")) // lowercase
				Expect(status.Status).To(Equal("healthy"))    // healthy when ready
			})
		})

		Context("when operator is ready as follower", func() {
			BeforeEach(func() {
				testOp.started = true
				testOp.leaderElectionManager = &LeaderElectionManager{
					config: &LeaderElectionConfig{
						Identity: "test-pod",
					},
					isLeader: false,
				}
			})

			It("should return ready follower status", func() {
				status := testOp.GetHealthStatus()
				Expect(status.Leadership).To(Equal("follower")) // lowercase
				Expect(status.Status).To(Equal("healthy"))
			})
		})

		Context("when operator is ready as leader", func() {
			BeforeEach(func() {
				testOp.started = true
				testOp.leaderElectionManager = &LeaderElectionManager{
					config: &LeaderElectionConfig{
						Identity: "test-pod",
					},
					isLeader: true,
				}
			})

			It("should return ready leader status", func() {
				status := testOp.GetHealthStatus()
				Expect(status.Leadership).To(Equal("leader")) // lowercase
				Expect(status.Status).To(Equal("healthy"))
			})
		})
	})

	Describe("GetMetrics()", func() {
		Context("without metrics collector", func() {
			It("should return zero metrics", func() {
				metrics := testOp.GetMetrics()
				Expect(metrics.WorkloadsManaged).To(BeZero())
				Expect(metrics.SpotInterruptions).To(BeZero())
				Expect(metrics.LeaderElectionChanges).To(BeZero())
			})
		})

		Context("with metrics collector", func() {
			BeforeEach(func() {
				collector := metrics.NewCollector()
				testOp.metricsCollector = collector
			})

			It("should return metrics from collector", func() {
				metricsResult := testOp.GetMetrics()
				Expect(metricsResult.WorkloadsManaged).To(BeNumerically(">=", 0))
			})
		})
	})

	Describe("GetGinEngine()", func() {
		It("should return nil when not initialized", func() {
			Expect(testOp.GetGinEngine()).To(BeNil())
		})

		It("should return gin engine when set", func() {
			gin.SetMode(gin.ReleaseMode)
			engine := gin.New()
			testOp.ginEngine = engine

			Expect(testOp.GetGinEngine()).To(Equal(engine))
		})
	})

	Describe("GetHealthChecker()", func() {
		It("should return nil when not initialized", func() {
			Expect(testOp.GetHealthChecker()).To(BeNil())
		})

		It("should return health checker when set", func() {
			checker := server.NewHealthChecker(nil, testOp.kubeClient, "test-ns")
			testOp.healthChecker = checker

			Expect(testOp.GetHealthChecker()).To(Equal(checker))
		})
	})

	Describe("GetMetricsServer()", func() {
		It("should return nil when not initialized", func() {
			Expect(testOp.GetMetricsServer()).To(BeNil())
		})

		It("should return metrics server when set", func() {
			collector := metrics.NewCollector()
			metricsServer := server.NewMetricsServer(collector)
			testOp.metricsServer = metricsServer

			Expect(testOp.GetMetricsServer()).To(Equal(metricsServer))
		})
	})

	Describe("GetWebhookServer()", func() {
		It("should return nil when not initialized", func() {
			Expect(testOp.GetWebhookServer()).To(BeNil())
		})
	})

	Describe("GetLeaderElectionDebugInfo()", func() {
		Context("without leader election", func() {
			It("should return 'no leader election manager'", func() {
				Expect(testOp.GetLeaderElectionDebugInfo()).To(Equal("no leader election manager"))
			})
		})

		Context("with leader election manager", func() {
			BeforeEach(func() {
				testOp.leaderElectionManager = &LeaderElectionManager{
					config: &LeaderElectionConfig{
						Identity: "test-pod-123",
						Enabled:  true,
					},
					isLeader:      true,
					currentLeader: "test-pod-123",
				}
			})

			It("should return formatted debug info", func() {
				info := testOp.GetLeaderElectionDebugInfo()
				Expect(info).To(ContainSubstring("Identity=test-pod-123"))
				Expect(info).To(ContainSubstring("IsLeader=true"))
				Expect(info).To(ContainSubstring("CurrentLeader=test-pod-123"))
				Expect(info).To(ContainSubstring("Enabled=true"))
			})
		})
	})

	Describe("DefaultOperatorConfig()", func() {
		It("should create config with default values", func() {
			config := DefaultOperatorConfig()

			Expect(config.MetricsAddr).To(Equal(":8080"))
			Expect(config.ProbeAddr).To(Equal(":8081"))
			Expect(config.WebhookAddr).To(Equal(":9443"))
			Expect(config.LeaderElectionID).To(Equal("spotalis-controller-leader"))
			Expect(config.ReconcileInterval).To(Equal(30 * time.Second))
			Expect(config.MaxConcurrentReconciles).To(Equal(10))
			Expect(config.APIQPSLimit).To(Equal(float32(20.0))) // Actual default is 20.0
			Expect(config.APIBurstLimit).To(Equal(30))          // Actual default is 30
		})

		It("should have webhook enabled by default", func() {
			config := DefaultOperatorConfig()
			Expect(config.EnableWebhook).To(BeTrue()) // Webhook is enabled by default
		})

		It("should have pprof disabled by default", func() {
			config := DefaultOperatorConfig()
			Expect(config.EnablePprof).To(BeFalse())
		})

		It("should not be in read-only mode by default", func() {
			config := DefaultOperatorConfig()
			Expect(config.ReadOnlyMode).To(BeFalse())
		})

		It("should have leader election enabled by default", func() {
			config := DefaultOperatorConfig()
			Expect(config.LeaderElection).To(BeTrue())
		})
	})
})
