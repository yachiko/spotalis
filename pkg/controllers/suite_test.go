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

package controllers

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Test fixture constants shared across the controllers test suite.
const (
	testAppLabel        = "app"
	testAppName         = "test"
	testContainerName   = "test-container"
	testImageNginx      = "nginx:latest"
	testDeploymentName  = "test-deployment"
	testStatefulSetName = "test-statefulset"
	testServiceName     = "test-service"
	testPodName         = "test-pod"
	testStringVersion   = "version"
	testNodeSpot        = "spot-node-1"
	testNodeOnDemand    = "ondemand-node-1"
	testNonexistent     = "nonexistent"
	nodeInstanceTypeKey = "node.kubernetes.io/instance-type"
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controllers Suite")
}
