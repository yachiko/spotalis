# Implementation Plan: Kubernetes Workload Replica Manager

**Branch**: `001-build-kubernetes-native` | **Date**: September 10, 2025 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/Users/ahoma/Projects/side/spotalis/specs/001-build-kubernetes-native/spec.md`

## Execution Flow (/plan command scope)
```
1. Load feature spec from Input path
   → If not found: ERROR "No feature spec at {path}"
2. Fill Technical Context (scan for NEEDS CLARIFICATION)
   → Detect Project Type from context (web=frontend+backend, mobile=app+api)
   → Set Structure Decision based on project type
3. Evaluate Constitution Check section below
   → If violations exist: Document in Complexity Tracking
   → If no justification possible: ERROR "Simplify approach first"
   → Update Progress Tracking: Initial Constitution Check
4. Execute Phase 0 → research.md
   → If NEEDS CLARIFICATION remain: ERROR "Resolve unknowns"
5. Execute Phase 1 → contracts, data-model.md, quickstart.md, agent-specific template file (e.g., `CLAUDE.md` for Claude Code, `.github/copilot-instructions.md` for GitHub Copilot, or `GEMINI.md` for Gemini CLI).
6. Re-evaluate Constitution Check section
   → If new violations: Refactor design, return to Phase 1
   → Update Progress Tracking: Post-Design Constitution Check
7. Plan Phase 2 → Describe task generation approach (DO NOT create tasks.md)
8. STOP - Ready for /tasks command
```

**IMPORTANT**: The /plan command STOPS at step 7. Phases 2-4 are executed by other commands:
- Phase 2: /tasks command creates tasks.md
- Phase 3-4: Implementation execution (manual or via tools)

## Summary
Kubernetes-native application that monitors Deployments and StatefulSets to ensure correct replica distribution between spot and on-demand nodes based on annotation-driven configuration. The system implements a controller pattern with mutating webhook capabilities, leader election for high availability, and respects pod disruption budgets during pod rescheduling operations.

## Technical Context
**Language/Version**: Go 1.21+  
**Primary Dependencies**: k8s.io/client-go, k8s.io/controller-runtime, github.com/gin-gonic/gin, github.com/onsi/ginkgo/v2  
**Storage**: Kubernetes API server (stateless application)  
**Testing**: Ginkgo + Gomega for BDD testing, Kind cluster for integration testing  
**Target Platform**: Kubernetes cluster (containerized application)
**Project Type**: Single project (Kubernetes controller with integrated webhook - Karpenter pattern)  
**Performance Goals**: Handle 1000+ workloads, <5s reconciliation time, <100MB memory usage  
**Constraints**: Stateless operation, leader election coordination, webhook latency <200ms  
**Scale/Scope**: Multi-tenant cluster support, 10k+ pods, HA deployment with 3 replicas

**Technical Details**: Single binary controller following Karpenter architecture. Uses pkg/operator pattern with integrated webhook, Gin for HTTP endpoints, slog for logging, Ginkgo for testing. Controller manager handles all registrations. Development via `make run` command. Kind cluster for integration testing. Configurable namespace filtering for multi-tenant clusters. Adjustable controller loop intervals (default 30s, min 5s) to prevent API server overload.

## Constitution Check
*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

**Simplicity**:
- Projects: 1 (controller with integrated webhook + cli in single binary)
- Using framework directly? Yes (gin, controller-runtime directly)
- Single data model? Yes (Kubernetes resources as state)
- Avoiding patterns? Yes (no Repository/UoW - direct k8s client)

**Architecture**:
- EVERY feature as library? Yes (controller, webhook, config as libraries)
- Libraries listed: 
  - controller: replica management logic
  - webhook: mutating admission webhook
  - config: configuration management
  - metrics: observability
- CLI per library: spotalis --help/--version, spotalis controller --help
- Library docs: llms.txt format planned? Yes

**Testing (NON-NEGOTIABLE)**:
- RED-GREEN-Refactor cycle enforced? Yes (Ginkgo BDD tests first)
- Git commits show tests before implementation? Yes (TDD mandatory)
- Order: Contract→Integration→E2E→Unit strictly followed? Yes
- Real dependencies used? Yes (Kind cluster, real k8s API)
- Integration tests for: controller loops, webhook admission, leader election
- FORBIDDEN: Implementation before test, skipping RED phase

**Observability**:
- Structured logging included? Yes (slog with JSON output)
- Frontend logs → backend? N/A (server-side only)
- Error context sufficient? Yes (correlation IDs, structured context)

**Versioning**:
- Version number assigned? v0.1.0 (MAJOR.MINOR.BUILD)
- BUILD increments on every change? Yes
- Breaking changes handled? Yes (API versioning, migration support)

## Project Structure

### Documentation (this feature)
```
specs/[###-feature]/
├── plan.md              # This file (/plan command output)
├── research.md          # Phase 0 output (/plan command)
├── data-model.md        # Phase 1 output (/plan command)
├── quickstart.md        # Phase 1 output (/plan command)
├── contracts/           # Phase 1 output (/plan command)
└── tasks.md             # Phase 2 output (/tasks command - NOT created by /plan)
```

### Source Code (repository root)
```
# Single project: Kubernetes controller with integrated webhook (Karpenter pattern)
cmd/
└── controller/         # Single main binary entry point
    └── main.go         # Controller + webhook + HTTP server

pkg/
├── operator/           # Operator setup and management
├── controllers/        # All controller logic
├── webhook/           # Webhook handlers (integrated with operator)
├── metrics/           # Observability and metrics
├── apis/              # API types and schemas
└── utils/             # Shared utilities

internal/
├── config/            # Configuration management
├── annotations/       # Annotation parsing and validation
└── server/            # HTTP server helpers

tests/
├── contract/          # API contract tests
├── integration/       # Kind cluster integration tests
├── e2e/              # End-to-end scenarios
└── unit/             # Unit tests

build/
├── docker/           # Dockerfile and build configs
├── k8s/             # Kubernetes manifests
└── docker-bake.hcl  # Docker buildx configuration

configs/
└── samples/         # Sample configuration files
```

**Structure Decision**: Single binary with integrated operator pattern (following Karpenter architecture)

## Phase 0: Outline & Research
1. **Extract unknowns from Technical Context** above:
   - For each NEEDS CLARIFICATION → research task
   - For each dependency → best practices task
   - For each integration → patterns task

2. **Generate and dispatch research agents**:
   ```
   For each unknown in Technical Context:
     Task: "Research {unknown} for {feature context}"
   For each technology choice:
     Task: "Find best practices for {tech} in {domain}"
   ```

3. **Consolidate findings** in `research.md` using format:
   - Decision: [what was chosen]
   - Rationale: [why chosen]
   - Alternatives considered: [what else evaluated]

**Output**: research.md with all NEEDS CLARIFICATION resolved

## Phase 1: Design & Contracts
*Prerequisites: research.md complete*

1. **Extract entities from feature spec** → `data-model.md`:
   - Entity name, fields, relationships
   - Validation rules from requirements
   - State transitions if applicable

2. **Generate API contracts** from functional requirements:
   - For each user action → endpoint
   - Use standard REST/GraphQL patterns
   - Output OpenAPI/GraphQL schema to `/contracts/`

3. **Generate contract tests** from contracts:
   - One test file per endpoint
   - Assert request/response schemas
   - Tests must fail (no implementation yet)

4. **Extract test scenarios** from user stories:
   - Each story → integration test scenario
   - Quickstart test = story validation steps

5. **Update agent file incrementally** (O(1) operation):
   - Run `/scripts/update-agent-context.sh [claude|gemini|copilot]` for your AI assistant
   - If exists: Add only NEW tech from current plan
   - Preserve manual additions between markers
   - Update recent changes (keep last 3)
   - Keep under 150 lines for token efficiency
   - Output to repository root

**Output**: data-model.md, /contracts/*, failing tests, quickstart.md, agent-specific file

## Phase 2: Task Planning Approach
*This section describes what the /tasks command will do - DO NOT execute during /plan*

**Task Generation Strategy**:
- Load `/templates/tasks-template.md` as base structure
- Generate tasks from Phase 1 design documents (contracts, data model, quickstart)
- Follow TDD principle: Tests before implementation for every component
- Use Go project layout structure for task organization

**Task Categories**:
1. **Setup Tasks**: Project structure, dependencies, build configuration
2. **Operator Framework**: Single binary entry point, manager setup, controller registration  
3. **Contract Tests**: HTTP endpoint tests, webhook interface tests, metrics tests
4. **Model Implementation**: Core data structures and validation logic
5. **Controller Implementation**: Reconciliation loop, leader election, caching
6. **Webhook Implementation**: Integrated admission webhook handlers
7. **Integration Tests**: Kind cluster tests, end-to-end scenarios
7. **Documentation**: CLI help, API docs, deployment guides

**Ordering Strategy**:
- **Phase 1**: Project setup and contract tests (all [P] parallel)
- **Phase 2**: Core models and validation (foundation layer)
- **Phase 3**: Controller components (depends on models)
- **Phase 4**: Webhook components (depends on models)  
- **Phase 5**: Integration tests (depends on controller + webhook)
- **Phase 6**: Documentation and deployment artifacts

**Parallelization Markers**:
- [P] for independent file creation tasks
- [P] for contract tests (different API endpoints)
- [P] for unit tests of different packages
- Sequential for integration tests requiring running services

**Expected Task Structure**:
```
001. [P] Setup Go project structure and dependencies
002. [P] Create docker-bake.hcl build configuration  
003. [P] Implement health check endpoint contract test
004. [P] Implement metrics endpoint contract test
005. [P] Implement webhook contract test
006. Implement WorkloadConfiguration model with validation
007. Implement ReplicaState calculation logic
008. Implement NodeClassification caching
009. Implement integrated HTTP server (metrics + webhook)
010. Implement controller reconciliation loop
011. Implement leader election integration
012. Implement webhook admission logic
013. Implement pod mutation logic
014. [P] Create Kind cluster integration test setup
015. Integration test: Basic workload reconciliation
016. Integration test: Spot node termination scenario
017. Integration test: Leader election failover
018. Integration test: Webhook pod mutation
019. E2E test: Complete quickstart scenario
020. Generate CLI documentation
021. Create deployment manifests
```

**Estimated Output**: 25-30 numbered, ordered tasks following TDD principles and Go best practices

**IMPORTANT**: This phase is executed by the /tasks command, NOT by /plan

## Phase 3+: Future Implementation
*These phases are beyond the scope of the /plan command*

**Phase 3**: Task execution (/tasks command creates tasks.md)  
**Phase 4**: Implementation (execute tasks.md following constitutional principles)  
**Phase 5**: Validation (run tests, execute quickstart.md, performance validation)

## Complexity Tracking
*Fill ONLY if Constitution Check has violations that must be justified*

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| [e.g., 4th project] | [current need] | [why 3 projects insufficient] |
| [e.g., Repository pattern] | [specific problem] | [why direct DB access insufficient] |


## Progress Tracking
*This checklist is updated during execution flow*

**Phase Status**:
- [x] Phase 0: Research complete (/plan command)
- [x] Phase 1: Design complete (/plan command)
- [x] Phase 2: Task planning complete (/plan command - describe approach only)
- [ ] Phase 3: Tasks generated (/tasks command)
- [ ] Phase 4: Implementation complete
- [ ] Phase 5: Validation passed

**Gate Status**:
- [x] Initial Constitution Check: PASS
- [x] Post-Design Constitution Check: PASS
- [x] All NEEDS CLARIFICATION resolved
- [x] Complexity deviations documented

---
*Based on Constitution v2.1.1 - See `/memory/constitution.md`*