# Changelog

All notable changes to Spotalis are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- PR validation workflow `lint` job no longer fails on a `mmake deps` typo.

### Changed
- Go toolchain bumped to 1.26. `Dockerfile`, `Makefile`, GitHub Actions, and `CONTRIBUTING.md` updated in lockstep.
- Dependency refresh: `k8s.io/{api,client-go,apimachinery}` 0.34.2 → 0.36.1, `sigs.k8s.io/controller-runtime` 0.22.4 → 0.24.1, `ginkgo` 2.25 → 2.27, plus minor refreshes across the tree.
- `ENVTEST_K8S_VERSION` bumped to 1.36.0 to match the new `k8s.io/api`.
- Integration test entry points consolidated: a single `TestIntegration` in `tests/integration/kind_suite_test.go` replaces 13 per-file `Test*Integration` functions that called `RunSpecs` independently (now rejected by Ginkgo 2.27).
- Helm install instructions now use a from-source flow until a chart repository is published.

### Removed
- Dead `Collector.StartMetricsCollection` placeholder and its two unit tests.
- Redundant `TestCheckPDBStatus` runner in `pkg/controllers/eviction_pdb_test.go` (the package already has `TestControllers`).
- Local-developer artifacts that should not have been tracked (`git_config` is now gitignored).

### Added
- `CONTRIBUTING.md`, `SECURITY.md`, `CHANGELOG.md`.

## [0.0.5] - 2025-12-09

### Added
- Pod disruption budget pre-checks before rebalancing operations.
- StatefulSet ordinal-aware deletion when scaling down rebalanced sets.
- Configurable webhook labels via `NodeClassifierConfig`.
- Admission state tracker to coordinate burst-scenario admissions.
- New integration tests covering burst load, rapid scaling, and PDB scenarios.
- `make tag` target.

### Changed
- Pod rebalancing now uses the Eviction API to respect PDBs (previously deleted pods directly).
- Burst-scenario pod distribution stability improvements.

### Fixed
- Termination grace period is now respected during eviction testing.

## [0.0.4] - 2025-11-22

### Changed
- All Go module dependencies updated to their latest versions.
- Repository ownership changed from `ahoma` to `yachiko`; image repo and references updated to `yachiko/spotalis`.

### Removed
- Copilots constitution / spec-driven scaffolding leftovers.

## [0.0.3] - 2025-11-22

Retag of `v0.0.2` (no diff).

## [0.0.2] - 2025-11-22

### Fixed
- Remaining `ahoma` references migrated to `yachiko`.

## [0.0.1] - 2025-11-07

Initial public release.

### Added
- Annotation-first spot vs on-demand replica distribution for Deployments and StatefulSets.
- Mutating admission webhook injecting capacity-type node selectors.
- Label-only enablement model (`spotalis.io/enabled=true`).
- Configuration loader with defaults, YAML file, and environment-variable layering.
- Prometheus metrics, health endpoints, leader election.
- Production-ready Helm chart in `deploy/helm`.
- Diátaxis-structured documentation (`docs/tutorials`, `docs/how-to`, `docs/reference`, `docs/explanation`).
- Apache License 2.0.

[Unreleased]: https://github.com/yachiko/spotalis/compare/v0.0.5...HEAD
[0.0.5]: https://github.com/yachiko/spotalis/compare/v0.0.4...v0.0.5
[0.0.4]: https://github.com/yachiko/spotalis/compare/v0.0.3...v0.0.4
[0.0.3]: https://github.com/yachiko/spotalis/compare/v0.0.2...v0.0.3
[0.0.2]: https://github.com/yachiko/spotalis/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/yachiko/spotalis/releases/tag/v0.0.1
