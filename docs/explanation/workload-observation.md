# Workload Observation

Spotalis observes Deployment and StatefulSet Pods through controller-owner UIDs. A
Deployment owns ReplicaSets, which own Pods; a StatefulSet owns Pods directly.
Matching workload labels are necessary but never sufficient: the complete Kubernetes
selector is applied and the controller-owner UID chain is verified.

Snapshots are ephemeral cache-backed reads keyed by workload UID. They contain the
workload generation, observation time, lifecycle state, assigned-node capacity, and
placement intent for every owned Pod. Resource versions are diagnostic metadata, not
globally ordered numbers.

Each Pod has exactly one lifecycle state: terminal, terminating, unscheduled Pending,
scheduled non-Ready, or Ready. Assigned capacity is a separate value: spot,
on-demand, or unknown. Only assigned Nodes prove capacity; node selectors and affinity
express placement intent and do not prove where a Pod runs. Unknown capacity is never
counted as on-demand.

Admission accepts snapshots no older than five seconds. A stale snapshot, missing Node,
or classification failure selects on-demand conservatively and remains observable as
uncertainty. Controllers may consume cache-backed snapshots but must not treat unknown
capacity as safe for elective disruption.
