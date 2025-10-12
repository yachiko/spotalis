# Spotalis

Spotalis is a lightweight Kubernetes controller that optimizes workload replica placement across spot and on-demand capacity using simple annotations (no CRDs).

> Fast start, minimal surface area, observable safety.

## Why Spotalis (At a Glance)
Annotation-first • Single binary + Helm • Safe spot adoption (min on-demand floor) • No custom APIs to version.

## Quickstart
See the tutorial for a first successful run on Kind:
- Tutorial: `docs/tutorials/run-kind.md`

Production (Helm) installation (repository URL placeholder until published):
```bash
helm repo add spotalis https://example.invalid/spotalis
helm repo update
helm upgrade --install spotalis spotalis/spotalis \
  --namespace spotalis-system --create-namespace
```

## Documentation Hub (Diátaxis)
Need | Go To | Purpose
---- | ----- | -------
First working deployment | `docs/tutorials/run-kind.md` | End-to-end success
Adjust spot vs on-demand mix | `docs/how-to/tune-spot-percentage.md` | Task steps
All workload labels & annotations | `docs/reference/labels-and-annotations.md` | Lookup
Configuration schema & env vars | `docs/reference/configuration.md` | Lookup
Metrics & ports | `docs/reference/metrics.md`, `docs/reference/runtime-ports.md` | Lookup
Internal state & strategy rationale | `docs/reference/state-management.md`, `docs/explanation/replica-distribution-strategy.md` | Understanding
System architecture & design trade-offs | `docs/explanation/architecture.md`, `docs/explanation/design-choices.md` | Rationale

## Core Concepts Snapshot
- Enabled via workload or namespace label `spotalis.io/enabled=true` (label-only, no enabling annotation).
- Distribution formula (details in strategy doc) balances desired spot percentage with minimum on-demand replicas.
- Leader election ensures a single active controller instance.

## Requirements
- Kubernetes: (specify tested versions) TBD
- Helm (for production deploy)
- Go (if building locally)

## Repo Guide
- Helm chart: `deploy/helm/`
- Examples: `examples/configs/`
- Source: `cmd/controller`, `pkg/`, `internal/`

## Contributing
See `CONTRIBUTING.md` for development and documentation placement guidelines.

## License
Apache 2.0 (TBD if different)

---
Legacy expanded conceptual and example content will be migrated into the structured docs under `docs/`. If something you expect is missing, open an issue referencing this README.
