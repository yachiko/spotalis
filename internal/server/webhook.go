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

package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	v1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	webhookMutate "github.com/spotalis/spotalis/pkg/webhook"
)

// WebhookServer provides webhook admission control functionality
type WebhookServer struct {
	server        *webhook.Server
	mutateHandler *webhookMutate.MutationHandler
	scheme        *runtime.Scheme
	decoder       *admission.Decoder

	// TLS configuration
	certPath  string
	keyPath   string
	tlsConfig *tls.Config

	// Server configuration
	port     int
	readOnly bool
}

// WebhookServerConfig contains configuration for the webhook server
type WebhookServerConfig struct {
	Port     int
	CertPath string
	KeyPath  string
	ReadOnly bool
}

// NewWebhookServer creates a new webhook server instance
func NewWebhookServer(config WebhookServerConfig, mutateHandler *webhookMutate.MutationHandler, scheme *runtime.Scheme) *WebhookServer {
	// Create decoder for admission requests
	decoder := admission.NewDecoder(scheme)

	// Create webhook server
	webhookServer := &webhook.Server{
		Port:          config.Port,
		CertDir:       "", // We'll use CertName and KeyName instead
		CertName:      config.CertPath,
		KeyName:       config.KeyPath,
		TLSMinVersion: "1.3",
	}

	return &WebhookServer{
		server:        webhookServer,
		mutateHandler: mutateHandler,
		scheme:        scheme,
		decoder:       decoder,
		certPath:      config.CertPath,
		keyPath:       config.KeyPath,
		port:          config.Port,
		readOnly:      config.ReadOnly,
	}
}

// MutateHandler implements the /mutate webhook endpoint
func (w *WebhookServer) MutateHandler(c *gin.Context) {
	if w.readOnly {
		w.sendAdmissionResponse(c, &v1.AdmissionResponse{
			UID:     "",
			Allowed: true,
			Result: &metav1.Status{
				Message: "webhook in read-only mode - no mutations applied",
			},
		})
		return
	}

	// Parse admission request
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "failed to read request body",
			"code":  "INVALID_REQUEST_BODY",
		})
		return
	}

	var admissionReview v1.AdmissionReview
	if err := w.deserializeAdmissionRequest(body, &admissionReview); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "failed to deserialize admission request",
			"code":    "INVALID_ADMISSION_REQUEST",
			"details": err.Error(),
		})
		return
	}

	if admissionReview.Request == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "admission request is nil",
			"code":  "EMPTY_ADMISSION_REQUEST",
		})
		return
	}

	// Handle the mutation
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	response, err := w.handleMutation(ctx, admissionReview.Request)
	if err != nil {
		// Send error response
		response = &v1.AdmissionResponse{
			UID:     admissionReview.Request.UID,
			Allowed: false,
			Result: &metav1.Status{
				Message: fmt.Sprintf("mutation failed: %v", err),
				Code:    http.StatusInternalServerError,
			},
		}
	}

	// Send the response
	w.sendAdmissionResponse(c, response)
}

// ValidateHandler implements the /validate webhook endpoint (for future use)
func (w *WebhookServer) ValidateHandler(c *gin.Context) {
	// For now, always allow - validation logic can be added later
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "failed to read request body",
			"code":  "INVALID_REQUEST_BODY",
		})
		return
	}

	var admissionReview v1.AdmissionReview
	if err := w.deserializeAdmissionRequest(body, &admissionReview); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "failed to deserialize admission request",
			"code":    "INVALID_ADMISSION_REQUEST",
			"details": err.Error(),
		})
		return
	}

	response := &v1.AdmissionResponse{
		UID:     admissionReview.Request.UID,
		Allowed: true,
		Result: &metav1.Status{
			Message: "validation passed",
		},
	}

	w.sendAdmissionResponse(c, response)
}

// SetReadOnly sets the webhook server to read-only mode
func (w *WebhookServer) SetReadOnly(readOnly bool) {
	w.readOnly = readOnly
}

// IsReadOnly returns whether the webhook server is in read-only mode
func (w *WebhookServer) IsReadOnly() bool {
	return w.readOnly
}

// GetTLSConfig returns the current TLS configuration
func (w *WebhookServer) GetTLSConfig() *tls.Config {
	return w.tlsConfig
}

// handleMutation processes the admission request through the mutation handler
func (w *WebhookServer) handleMutation(ctx context.Context, req *v1.AdmissionRequest) (*v1.AdmissionResponse, error) {
	if w.mutateHandler == nil {
		return &v1.AdmissionResponse{
			UID:     req.UID,
			Allowed: true,
			Result: &metav1.Status{
				Message: "mutation handler not configured",
			},
		}, nil
	}

	// Create admission request wrapper
	admissionRequest := admission.Request{
		AdmissionRequest: *req,
	}

	// Call the mutation handler
	response := w.mutateHandler.Handle(ctx, admissionRequest)

	return &v1.AdmissionResponse{
		UID:       req.UID,
		Allowed:   response.Allowed,
		Result:    response.Result,
		Patch:     response.Patch,
		PatchType: response.PatchType,
	}, nil
}

// sendAdmissionResponse sends the admission response back to Kubernetes
func (w *WebhookServer) sendAdmissionResponse(c *gin.Context, response *v1.AdmissionResponse) {
	admissionReview := v1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: response,
	}

	c.Header("Content-Type", "application/json")
	c.JSON(http.StatusOK, admissionReview)
}

// deserializeAdmissionRequest deserializes the admission request from JSON
func (w *WebhookServer) deserializeAdmissionRequest(body []byte, review *v1.AdmissionReview) error {
	codecs := serializer.NewCodecFactory(w.scheme)
	deserializer := codecs.UniversalDeserializer()

	_, _, err := deserializer.Decode(body, nil, review)
	return err
}

// SetupRoutes configures the webhook routes on the given Gin router
func (w *WebhookServer) SetupRoutes(router *gin.Engine) {
	webhook := router.Group("/webhook")
	{
		webhook.POST("/mutate", w.MutateHandler)
		webhook.POST("/validate", w.ValidateHandler)
	}
}

// GetControllerRuntimeServer returns the controller-runtime webhook server for integration
func (w *WebhookServer) GetControllerRuntimeServer() *webhook.Server {
	return w.server
}

// WebhookHandler provides backward compatibility with contract tests
type WebhookHandler struct {
	server *WebhookServer
}

// NewWebhookHandler creates a new webhook handler (for compatibility)
func NewWebhookHandler() *WebhookHandler {
	return &WebhookHandler{}
}

// SetServer sets the underlying webhook server
func (w *WebhookHandler) SetServer(server *WebhookServer) {
	w.server = server
}

// Mutate handles the /mutate endpoint (compatibility wrapper)
func (w *WebhookHandler) Mutate(c *gin.Context) {
	if w.server == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "webhook server not initialized",
			"code":  "WEBHOOK_SERVER_NOT_INITIALIZED",
		})
		return
	}
	w.server.MutateHandler(c)
}

// Validate handles the /validate endpoint (compatibility wrapper)
func (w *WebhookHandler) Validate(c *gin.Context) {
	if w.server == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "webhook server not initialized",
			"code":  "WEBHOOK_SERVER_NOT_INITIALIZED",
		})
		return
	}
	w.server.ValidateHandler(c)
}

// SetReadOnly sets the webhook to read-only mode (compatibility wrapper)
func (w *WebhookHandler) SetReadOnly(readOnly bool) {
	if w.server != nil {
		w.server.SetReadOnly(readOnly)
	}
}
