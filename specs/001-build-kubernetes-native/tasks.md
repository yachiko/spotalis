# Tasks: Kubernetes Workload Replica Manager

**Input**: Design documents from `/Users/ahoma/Projects/side/spotalis/specs/001-build-kubernetes-native/`
**Prerequisites**: plan.md (required), research.md, data-model.md, contracts/api-contracts.md, quickstart.md

## Execution Flow (main)
```
1. Load plan.md from feature directory ✓
   → Extract: Go 1.21+, controller-runtime, Gin, Ginkgo, Kind testing
   → Structure: Single binary with pkg/operator pattern (Karpenter architecture)
2. Load design documents ✓:
   → data-model.md: 7 entities (WorkloadConfiguration, ReplicaState, NodeClassification, etc.)
   → contracts/api-contracts.md: HTTP endpoints and webhook interface
   → research.md: Technology decisions and configuration patterns
   → quickstart.md: Integration test scenarios
3. Generate tasks by category:
   → Setup: Go project, dependencies, Makefile
   → Tests: Contract tests, integration tests with Kind
   → Core: Models, controllers, webhook, operator
   → Integration: Kubernetes client, leader election, metrics
   → Polish: Unit tests, performance validation, documentation
4. Apply task rules:
   → Different files = mark [P] for parallel
   → Same file = sequential (no [P])
   → Tests before implementation (TDD)
5. Number tasks sequentially (T001, T002...)
6. Dependencies: Setup → Tests → Core → Integration → Polish
```

## Format: `[ID] [P?] Description`
- **[P]**: Can run in parallel (different files, no dependencies)
- Include exact file paths in descriptions

## Path Conventions
Single project with Karpenter architecture pattern:
- **cmd/controller/**: Main binary entry point
- **pkg/**: Public packages (operator, controllers, webhook, apis)
- **internal/**: Private packages (config, annotations, server)
- **tests/**: All test categories (contract, integration, e2e, unit)

## Phase 3.1: Setup
- [x] T001 Create Go project structure following plan.md architecture in cmd/, pkg/, internal/, tests/
- [x] T002 Initialize Go module with go.mod and required dependencies (controller-runtime, Gin, Ginkgo)
- [x] T003 [P] Create Makefile with targets: run, test, docker-build, test-integration
- [x] T004 [P] Configure golangci-lint and gofmt tools in .golangci.yml
- [x] T005 [P] Create Docker and docker-bake.hcl configuration in build/docker/

## Phase 3.2: Tests First (TDD) ✅ COMPLETE
**CRITICAL: These tests MUST be written and MUST FAIL before ANY implementation**

### Contract Tests (HTTP Endpoints)
- [x] T006 [P] Contract test GET /healthz endpoint in tests/contract/healthz_test.go
- [x] T007 [P] Contract test GET /readyz endpoint in tests/contract/readyz_test.go  
- [x] T008 [P] Contract test GET /metrics endpoint in tests/contract/metrics_test.go
- [x] T009 [P] Contract test POST /mutate webhook in tests/contract/webhook_test.go

### Integration Tests (User Scenarios from quickstart.md)
- [x] T010 [P] Integration test basic workload deployment with annotations in tests/integration/basic_deployment_test.go
- [x] T011 [P] Integration test spot node termination scenario in tests/integration/spot_termination_test.go
- [x] T012 [P] Integration test configuration change scenario in tests/integration/config_change_test.go
- [x] T013 [P] Integration test scale up/down scenario in tests/integration/scaling_test.go
- [x] T014 [P] Integration test leader election scenario in tests/integration/leader_election_test.go
- [x] T015 [P] Integration test multi-tenant namespace filtering in tests/integration/namespace_filtering_test.go
- [x] T016 [P] Integration test performance tuning scenario in tests/integration/performance_test.go

## Phase 3.3: Core Implementation (ONLY after tests are failing)

### Data Models (from data-model.md) ✅ COMPLETE
- [x] T017 [P] WorkloadConfiguration struct in pkg/apis/configuration.go
- [x] T018 [P] ReplicaState struct with calculation methods in pkg/apis/replica_state.go  
- [x] T019 [P] NodeClassification struct and NodeType enum in pkg/apis/node_classification.go
- [x] T020 [P] LeadershipState struct in pkg/apis/leadership_state.go
- [x] T021 [P] DisruptionPolicy struct with safety calculations in pkg/apis/disruption_policy.go
- [x] T022 [P] ControllerConfiguration struct in pkg/apis/controller_config.go
- [x] T023 [P] NamespaceFilter and APIRateLimiter structs in pkg/apis/runtime_state.go

### Core Services and Controllers ✅ COMPLETE  
- [x] T024 [P] Annotation parser service in internal/annotations/parser.go
- [x] T025 [P] Node classifier service in internal/config/node_classifier.go
- [x] T026 [P] Deployment controller reconciliation logic in pkg/controllers/deployment_controller.go
- [x] T027 [P] StatefulSet controller reconciliation logic in pkg/controllers/statefulset_controller.go
- [x] T028 [P] Webhook mutation handler in pkg/webhook/mutate.go
- [x] T029 [P] Metrics collection and Prometheus integration in pkg/metrics/collector.go

### HTTP Server and Main Entry Point ✅ COMPLETE
- [x] T030 Health check handler implementation in internal/server/health.go
- [x] T031 Metrics endpoint handler in internal/server/metrics.go
- [x] T032 Webhook server setup and routing in internal/server/webhook.go
- [x] T033 Main operator setup following Karpenter pattern in pkg/operator/operator.go
- [x] T034 Main binary entry point in cmd/controller/main.go

## Phase 3.4: Integration

### Kubernetes Integration
- [x] T035 Kubernetes client configuration and RBAC setup in pkg/operator/kubernetes.go
- [x] T036 Leader election implementation using controller-runtime in pkg/operator/leader_election.go
- [x] T037 Controller registration and manager setup in pkg/controllers/manager.go
- [x] T038 [P] Webhook admission configuration and TLS setup in pkg/webhook/admission.go
- [x] T039 [P] Configuration loading from YAML and environment variables in internal/config/loader.go

### Operational Features
- [x] T040 Structured logging with slog integration in pkg/utils/logging.go
- [x] T041 Graceful shutdown and signal handling in pkg/operator/shutdown.go
- [x] T042 [P] Rate limiting and API server protection in pkg/utils/rate_limiter.go
- [x] T043 [P] Multi-tenant namespace filtering implementation in pkg/controllers/namespace_filter.go

## Phase 3.5: Polish

### Unit Tests
- [ ] T044 [P] Unit tests for WorkloadConfiguration validation in tests/unit/configuration_test.go
- [ ] T045 [P] Unit tests for ReplicaState calculations in tests/unit/replica_state_test.go
- [ ] T046 [P] Unit tests for NodeClassification logic in tests/unit/node_classification_test.go
- [ ] T047 [P] Unit tests for annotation parsing in tests/unit/annotations_test.go
- [ ] T048 [P] Unit tests for webhook mutation logic in tests/unit/webhook_test.go

### Performance and Documentation
- [ ] T049 Performance validation tests (<5s reconciliation, <100MB memory) in tests/e2e/performance_test.go
- [ ] T050 [P] Kind cluster setup for local testing in tests/e2e/kind_cluster_test.go
- [ ] T051 [P] Update README.md with installation and usage instructions
- [ ] T052 [P] Create deployment manifests in build/k8s/
- [ ] T053 Remove code duplication and optimize imports
- [ ] T054 Run manual testing scenarios from quickstart.md

## Dependencies

### Sequential Dependencies
- **Setup First**: T001-T005 before all other tasks
- **Tests Before Implementation**: T006-T016 must complete and fail before T017-T054
- **Models Before Services**: T017-T023 before T024-T029
- **Services Before Integration**: T024-T034 before T035-T043
- **Core Before Polish**: T035-T043 before T044-T054

### File-Level Dependencies (Same File = Sequential)
- T030, T031, T032 (internal/server/) must be sequential
- T033, T034 (main operator/binary) must be sequential  
- T035, T036, T037 (Kubernetes integration) must be sequential

## Parallel Execution Examples

### Contract Tests (can run simultaneously)
```bash
# Phase 3.2 - All contract tests in parallel
Task: "Contract test GET /healthz endpoint in tests/contract/healthz_test.go"
Task: "Contract test GET /readyz endpoint in tests/contract/readyz_test.go"  
Task: "Contract test GET /metrics endpoint in tests/contract/metrics_test.go"
Task: "Contract test POST /mutate webhook in tests/contract/webhook_test.go"
```

### Data Models (can run simultaneously)
```bash
# Phase 3.3 - All model structs in parallel
Task: "WorkloadConfiguration struct in pkg/apis/configuration.go"
Task: "ReplicaState struct with calculation methods in pkg/apis/replica_state.go"
Task: "NodeClassification struct and NodeType enum in pkg/apis/node_classification.go"
Task: "LeadershipState struct in pkg/apis/leadership_state.go"
```

### Integration Tests (can run simultaneously)
```bash
# Phase 3.2 - All integration scenarios in parallel
Task: "Integration test basic workload deployment with annotations in tests/integration/basic_deployment_test.go"
Task: "Integration test spot node termination scenario in tests/integration/spot_termination_test.go"
Task: "Integration test multi-tenant namespace filtering in tests/integration/namespace_filtering_test.go"
```

## Notes
- [P] tasks = different files, no dependencies, can run in parallel
- All tests in Phase 3.2 MUST fail before implementing Phase 3.3
- Follow TDD: write test → see it fail → implement → see it pass
- Commit after each completed task
- Use Kind clusters for integration testing
- Follow Karpenter single binary architecture pattern
- Configurable controller loop intervals (30s default, 5s minimum)
- Support multi-tenant namespace filtering

## Validation Checklist
- ✓ All HTTP endpoints have contract tests (T006-T009)
- ✓ All quickstart scenarios have integration tests (T010-T016)  
- ✓ All data model entities have implementations (T017-T023)
- ✓ Core controller logic implemented (T024-T029)
- ✓ Single binary operator pattern followed (T033-T034)
- ✓ Kubernetes integration complete (T035-T043)
- ✓ Unit test coverage for critical logic (T044-T048)
- ✓ Performance requirements validated (T049)
