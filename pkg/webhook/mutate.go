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
	"github.com/gin-gonic/gin"
)

// MutatingHandler handles webhook mutation requests
type MutatingHandler struct {
	// Implementation will be added in Phase 3.3
}

// NewMutatingHandler creates a new mutating webhook handler
func NewMutatingHandler() *MutatingHandler {
	// This will fail until we implement it in T028
	panic("NewMutatingHandler not implemented - test should fail")
}

// Handle processes webhook admission requests
func (h *MutatingHandler) Handle(c *gin.Context) {
	// This will fail until we implement it in T028
	panic("Handle not implemented - test should fail")
}
