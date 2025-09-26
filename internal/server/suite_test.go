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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Server Suite")
}

var _ = BeforeSuite(func() {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)
})

// Helper functions for testing HTTP handlers

// createTestEngine creates a new Gin engine for testing
func createTestEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

// performRequest performs an HTTP request against a Gin engine and returns the response
func performRequest(engine *gin.Engine, method, path string, body interface{}) *httptest.ResponseRecorder {
	var req *http.Request
	var err error

	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			panic(err) // This is a test helper, panic is acceptable
		}
		req, _ = http.NewRequestWithContext(context.Background(), method, path, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequestWithContext(context.Background(), method, path, http.NoBody)
	}

	Expect(err).NotTo(HaveOccurred())

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	return recorder
}

// performRequestWithHeaders performs an HTTP request with custom headers
func performRequestWithHeaders(engine *gin.Engine, method, path string, body interface{}, headers map[string]string) *httptest.ResponseRecorder {
	var req *http.Request
	var err error

	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			panic(err) // This is a test helper, panic is acceptable
		}
		req, _ = http.NewRequestWithContext(context.Background(), method, path, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequestWithContext(context.Background(), method, path, http.NoBody)
	}

	Expect(err).NotTo(HaveOccurred())

	// Set custom headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	return recorder
}

// parseJSONResponse parses a JSON response into the provided interface
func parseJSONResponse(recorder *httptest.ResponseRecorder, v interface{}) error {
	return json.Unmarshal(recorder.Body.Bytes(), v)
}
