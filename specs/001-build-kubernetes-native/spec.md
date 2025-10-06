# Feature Specification: Kubernetes Workload Replica Manager

**Feature Branch**: `001-build-kubernetes-native`  
**Created**: September 10, 2025  
**Status**: Draft  
**Input**: User description: "Build Kubernetes-native application that will monitor Kubernetes workloads to make sure that there is required number of replicas running on on-demand or spot nodes, depending on configuration. System behaviour should be configurable via annotations on monitored workloads, deployments and statefulsets. The application should also handle mutating webhook to dynamically modify pods nodeSelectors to keep target state. Healthchecks should be implemented. Application itself needs to be stateless, but Kubernetes context shall work as state storage. The application shall monitor state in controller loop. The application should have ability to reschedule pods by deleting them, of course it should consider disruption budgets, with optional override. Application shall be configured via yaml configuration file with optional environment variables override. The application should support multiple instances running concurrently, through leader election."

---

## User Scenarios & Testing *(mandatory)*

### Primary User Story
As a Kubernetes platform operator, I need an automated system that ensures critical workloads maintain their required number of replicas on appropriate node types (spot vs on-demand) to optimize costs while maintaining availability. The system should intelligently manage pod placement and rescheduling based on configuration policies I define through workload annotations.

### Acceptance Scenarios
1. **Given** a deployment with 3 replicas configured for spot nodes via annotations, **When** one spot node is terminated, **Then** the system must detect the replica shortage and schedule a replacement pod on another spot node
2. **Given** a deployment configured to prefer spot nodes but allow on-demand fallback, **When** no spot nodes are available, **Then** the system must schedule pods on on-demand nodes and move them back to spot nodes when available
3. **Given** a statefulset with pod disruption budget of min 2 available, **When** the system needs to reschedule a pod, **Then** it must respect the disruption budget and only delete pods when safe to do so
4. **Given** multiple instances of the replica manager running, **When** they start up, **Then** only one instance should become the active leader while others remain on standby

### Edge Cases
- What happens when no nodes of the preferred type (spot/on-demand) are available?
- How does the system handle workloads without replica management annotations?
- What occurs when disruption budgets prevent pod rescheduling?
- How does the system behave during leader election failures or leadership transitions?

## Requirements *(mandatory)*

### Functional Requirements
- **FR-001**: System MUST continuously monitor Kubernetes deployments and statefulsets for replica count discrepancies
- **FR-002**: System MUST read workload configuration from Kubernetes annotations to determine node type preferences (spot vs on-demand)
- **FR-003**: System MUST implement a mutating webhook to modify pod nodeSelectors based on configuration policies
- **FR-004**: System MUST respect pod disruption budgets when rescheduling pods, with configurable override capability
- **FR-005**: System MUST support leader election to enable multiple concurrent instances with only one active controller
- **FR-006**: System MUST provide health check endpoints for monitoring system status
- **FR-007**: System MUST be stateless and use Kubernetes API as the source of truth for all state information
- **FR-008**: System MUST support configuration via consolidated YAML file with environment variable overrides using `SPOTALIS_` prefix
- **FR-009**: System MUST operate in a controller loop pattern for continuous monitoring with configurable intervals
- **FR-010**: System MUST be able to delete and reschedule pods to maintain target replica distribution
- **FR-011**: System MUST distinguish between spot and on-demand nodes through node labels or taints, labels MUST be configurable, with default being labels used by Karpenter
- **FR-012**: System MUST handle only workloads that have `enabled` annotation set to true
- **FR-013**: System MUST define annotation schema for workload configuration, defined in [annotations.md](annotations.md)
- **FR-014**: System MUST implement retry logic for failed operations with exponential backoff and adequate log records
- **FR-015**: System MUST provide structured logging using Go slog with JSON output and metrics collection for operational observability
- **FR-016**: System MUST support optional namespace filtering through namespaceSelector configuration to accommodate multi-tenant clusters and limit scope of monitoring
- **FR-017**: System MUST allow configurable controller loop intervals to prevent API server overload, with sensible defaults (e.g., 30s) and minimum limits (e.g., 5s)
- **FR-018**: System MUST use dependency injection pattern for service lifecycle management and testability
- **FR-019**: System MUST follow single binary architecture integrating controller, webhook, and HTTP servers in unified operator following Karpenter pattern 
  
### Key Entities
- **Workload Configuration**: Annotation-based policy defining replica distribution preferences, node type requirements, and rescheduling behavior
- **Replica State**: Current vs desired replica count and distribution across node types for monitored workloads
- **Node Classification**: Categorization of cluster nodes as spot or on-demand based on labels/taints
- **Leadership State**: Current leader instance information for coordinating multiple replica manager instances
- **Disruption Policy**: Pod disruption budget constraints and override settings for safe rescheduling operations

## Dependencies & Assumptions

### Dependencies
- **Kubernetes Cluster**: Version 1.26+ with admission controller support
- **Node Labeling**: Nodes labeled with spot/on-demand identifiers (default: Karpenter labels)
- **RBAC**: ServiceAccount with permissions for deployments, pods, nodes, events, leases
- **WebhookConfiguration**: MutatingWebhookConfiguration created externally
- **Network**: Webhook endpoint accessible from API server
- **Go Runtime**: Go 1.21+ for structured logging (slog) support
- **Uber Dig**: Dependency injection framework v1.19.0+ for service lifecycle management

### Assumptions  
- Spot nodes may terminate with minimal notice
- On-demand nodes provide higher availability than spot nodes
- Pod disruption budgets accurately reflect application requirements
- Users understand annotation schema and configure workloads appropriately

---

## Review & Acceptance Checklist
*GATE: Automated checks run during main() execution*

### Content Quality
- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

### Requirement Completeness
- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous  
- [x] Success criteria are measurable
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

---

## Execution Status
*Updated by main() during processing*

- [ ] User description parsed
- [ ] Key concepts extracted
- [ ] Ambiguities marked
- [ ] User scenarios defined
- [ ] Requirements generated
- [ ] Entities identified
- [ ] Review checklist passed

---
