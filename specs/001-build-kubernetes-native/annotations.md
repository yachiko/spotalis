# Annotations Specification

Following annotations can be set on Deployments and StatefulSets Kubernetes resources:

### spotalis.io/enabled

Allowed values:
- "true"
- "false"

Description:
Base annotation. Only workloads with that annotation set to "true" SHALL be managed and monitored by the application.

### spotalis.io/min-on-demand

Allowed values: 0-999

Description:
Configures the MINIMUM number of replicas that SHALL run on on-demand type nodes at any given moment. This ensures availability guarantees and SLA compliance. This constraint takes precedence over cost optimization.

### spotalis.io/spot-percentage

Allowed values: 0-100%

Description:
Configures what percentage of the total replicas for the workload SHALL be deployed on spot instances for cost optimization. Percentage symbol (%) MUST be included in annotation value. Applied after minimum on-demand requirements are satisfied.

## Allocation Algorithm

The system uses the following algorithm to determine replica placement:

1. **Satisfy minimum on-demand first**: Allocate `spotalis.io/min-on-demand` replicas to on-demand nodes
2. **Apply spot percentage**: Calculate desired spot replicas as `(total replicas * spot-percentage / 100)`
3. **Allocate remaining replicas**: Any remaining replicas go to on-demand nodes (safe default)

### Examples

**Example 1: Normal Operation**
```yaml
replicas: 10
spotalis.io/min-on-demand: "2"
spotalis.io/spot-percentage: "60%"
```
Result: 2 minimum on-demand + 6 spot (60% of 10) + 2 remaining → on-demand
Final: 4 on-demand, 6 spot

**Example 2: Small Scale**
```yaml
replicas: 3
spotalis.io/min-on-demand: "2"
spotalis.io/spot-percentage: "60%"
```
Result: 2 minimum on-demand + 1 spot (60% of 3, rounded down)
Final: 2 on-demand, 1 spot

**Example 3: Constraint Conflict**
```yaml
replicas: 4
spotalis.io/min-on-demand: "3"
spotalis.io/spot-percentage: "75%"
```
Result: 3 minimum on-demand (priority) + 1 remaining (spot wants 3 but only 1 available)
Final: 3 on-demand, 1 spot

## Annotation Requirements

1. **Both annotations are complementary**: Both `spotalis.io/min-on-demand` and `spotalis.io/spot-percentage` can be used together
2. **Minimum on-demand takes precedence**: Availability requirements override cost optimization
3. **Safe defaults**: Remaining replicas default to on-demand nodes for stability

## Additional Requirements

1. When `spotalis.io/enabled` annotation is set to "true" but no other annotations are set, then NO changes SHALL be made to the workload
2. Invalid annotation values SHALL be logged as ERROR level log record. The workload with invalid annotations MUST not be evaluated further
3. If `spotalis.io/min-on-demand` exceeds total replica count, the system SHALL log an ERROR and not manage the workload
4. Percentage calculations SHALL be rounded down for spot instances (favor stability over cost optimization)
5. The system SHALL validate that percentage values include the "%" symbol 