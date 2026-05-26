/*
Copyright 2024 The Spotalis Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package apis

// Test fixture strings shared across the apis package tests, hoisted out
// of individual files to satisfy the goconst linter.
const (
	labelManagedBy        = "managed-by"
	labelTypeKey          = "type"
	valueOther            = "other"
	stringLeader          = "leader"
	stringDefault         = "default"
	stringTestNode        = "test-node"
	labelKubernetesOS     = "kubernetes.io/os"
	labelNodeLifecycle    = "node.kubernetes.io/lifecycle"
)
