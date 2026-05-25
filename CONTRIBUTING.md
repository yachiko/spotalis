# Contributing to Spotalis

Thanks for your interest in improving Spotalis. This guide covers what you need to develop, test, and submit changes.

## Prerequisites

| Tool   | Version | Why                                          |
| ------ | ------- | -------------------------------------------- |
| Go     | 1.24+   | Matches `go.mod` and the Dockerfile builder. |
| Docker | recent  | Building the controller image, Kind nodes.   |
| Kind   | recent  | Local cluster for integration tests.         |
| Helm   | v3.x    | Installing/linting the chart in `deploy/helm`. |
| Make   | any     | Driver for all common tasks.                 |

## Local Development Workflow

```bash
make deps              # Download / tidy modules
make build             # Build the controller binary
make run               # Run against your current kube context
make test              # Unit tests (uses envtest)
make test-integration  # Kind-based integration tests
make lint              # golangci-lint
make fmt               # Format imports / Go files
```

`make test` will download envtest binaries on first run (controlled by `ENVTEST_K8S_VERSION` in the `Makefile`). `make test-integration` spins up a Kind cluster — expect several minutes.

For an end-to-end loop with a local cluster:

```bash
make kind              # Create the Kind cluster
make build-deploy      # Build image, load to Kind, deploy via Helm
make kind-port-forward # Expose metrics/health on localhost
```

## Commit Style

We use [Conventional Commits](https://www.conventionalcommits.org/). Look at `git log` for examples. Common types:

- `feat:` — new functionality
- `fix:` — bug fix
- `chore:` — maintenance, dependency bumps, repo hygiene
- `docs:` — documentation only
- `test:` — adding or refactoring tests
- `refactor:` — internal cleanup with no behavior change

Scope where it adds clarity: `feat(webhook): …`, `fix(controller): …`.

Keep commits small and focused — one logical change per commit. Branch from `main`.

## Pull Request Checklist

- [ ] `make lint` is clean.
- [ ] `make test` passes.
- [ ] `make test-integration` passes when you change controller/webhook logic.
- [ ] User-facing changes have a `CHANGELOG.md` entry under `## [Unreleased]`.
- [ ] Behavior changes touch the matching reference doc in the same PR (see `docs/style.md`).
- [ ] New annotations / labels are reflected in `docs/reference/labels-and-annotations.md`.

## Documentation Placement

Spotalis docs follow the [Diátaxis](https://diataxis.fr/) framework:

| You're writing…                                | Goes in           |
| ---------------------------------------------- | ----------------- |
| Step-by-step intro for new users               | `docs/tutorials/` |
| Goal-oriented "how do I…" recipe               | `docs/how-to/`    |
| Lookup-style key/field/metric listing          | `docs/reference/` |
| Background, rationale, design trade-offs       | `docs/explanation/` |

See `docs/style.md` for tone, headings, and formatting conventions, and `docs/index.md` for the documentation map.

## Reporting Security Issues

Please don't open a public issue for security vulnerabilities. See `SECURITY.md` for the disclosure process.

## Code of Conduct

Be respectful. Disagreement is fine; personal attacks are not.

## License

By contributing you agree your contributions are licensed under the project's [Apache-2.0 license](LICENSE).
