# Documentation Index

Single hub for all Spotalis documentation following the Diátaxis framework.

## Quadrants
| Need                 | Category    | Start Here                            | Description                                    |
| -------------------- | ----------- | ------------------------------------- | ---------------------------------------------- |
| First success        | Tutorial    | `tutorials/run-kind.md`               | Deploy on Kind and see mutation & distribution |
| Perform a task       | How-To      | `how-to/tune-spot-percentage.md`      | Adjust replica mix safely                      |
| Look up a key        | Reference   | `reference/labels-and-annotations.md` | All workload keys                              |
| Understand rationale | Explanation | `explanation/architecture.md`         | Architecture flow                              |

## Other Key Pages
| Topic                  | Page                                           |
| ---------------------- | ---------------------------------------------- |
| Configuration schema   | `reference/configuration.md`                   |
| Metrics inventory      | `reference/metrics.md`                         |
| Runtime ports          | `reference/runtime-ports.md`                   |
| State internals        | `reference/state-management.md`                |
| Distribution algorithm | `explanation/replica-distribution-strategy.md` |
| Design trade-offs      | `explanation/design-choices.md`                |
| Glossary               | `reference/glossary.md`                        |

## Stability Legend
| Marker       | Meaning                                                       |
| ------------ | ------------------------------------------------------------- |
| Stable       | Backwards compatibility expected; changes rare and documented |
| Provisional  | Behavior may evolve; avoid automation hard coupling           |
| Experimental | Early preview; subject to removal or breaking changes         |

## Conventions
- Enablement is label-only: `spotalis.io/enabled=true` required on namespace and workload.
- Configuration precedence: defaults < YAML file < environment variables.
- Metrics & health endpoints default to `:8080` and `:8081`.

## Migration Notice
Earlier drafts referenced an enablement annotation. That mechanism is deprecated and replaced with the namespace/workload label. Update any old examples accordingly.

## See Also
- Root README hub: `../README.md`
- Contribution guidelines: `../CONTRIBUTING.md`
- Glossary: `reference/glossary.md`
