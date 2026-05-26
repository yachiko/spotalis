/*
Copyright 2024 The Spotalis Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package unit

// Shared test fixture strings flagged by goconst. Defined once for the whole
// tests/unit package so each test file can reference them by name.
const (
	annotationEnabled        = "spotalis.io/enabled"
	annotationMinOnDemand    = "spotalis.io/min-on-demand"
	annotationSpotPercentage = "spotalis.io/spot-percentage"

	labelCapacityType  = "karpenter.sh/capacity-type"
	labelKubernetesOS  = "kubernetes.io/os"
	labelNodeLifecycle = "node.kubernetes.io/lifecycle"

	stringTrue        = "true"
	stringPct70       = "70%"
	stringInvalid     = "invalid"
	stringDefault     = "default"
	stringOnDemand    = "on-demand"
	stringApp         = "app"
	stringDeployment  = "Deployment"
	stringStatefulSet = "StatefulSet"

	testNodeName        = "test-node"
	testDeploymentName  = "test-deployment"
	testStatefulSetName = "test-statefulset"
	testContainerName   = "test-container"
	testImageNginx      = "nginx:latest"

	stringSpot  = "spot"
	stringTest  = "test"
	stringLinux = "linux"
	stringMyApp = "myapp"
)
