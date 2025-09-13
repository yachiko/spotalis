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

package config

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Suite")
}

var _ = BeforeSuite(func() {
	// Set up any global test configuration
	By("Setting up the test environment")
})

var _ = AfterSuite(func() {
	// Clean up any global test configuration
	By("Tearing down the test environment")
})

// Helper function to set environment variables for testing
func setEnvVars(envVars map[string]string) {
	for key, value := range envVars {
		err := os.Setenv(key, value)
		Expect(err).NotTo(HaveOccurred())
	}
}

// Helper function to clean up environment variables after testing
func cleanupEnvVars(envVars map[string]string) {
	for key := range envVars {
		err := os.Unsetenv(key)
		Expect(err).NotTo(HaveOccurred())
	}
}

// Helper function to create a temporary YAML config file for testing
func createTempConfigFile(content string) (*os.File, error) {
	tmpFile, err := os.CreateTemp("", "config-test-*.yaml")
	if err != nil {
		return nil, err
	}

	_, err = tmpFile.WriteString(content)
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return nil, err
	}

	err = tmpFile.Close()
	if err != nil {
		os.Remove(tmpFile.Name())
		return nil, err
	}

	return tmpFile, nil
}

// Helper function to clean up temporary files
func cleanupTempFile(file *os.File) {
	if file != nil {
		os.Remove(file.Name())
	}
}
