# Contributing to Spotalis

## Documentation Placement Guide (Diátaxis)
Pick the quadrant by user intent:

Intent | Quadrant | Hallmarks | Anti-Patterns
------ | -------- | --------- | ------------
Learn by doing first success | Tutorial | Linear steps, no branches | Exhaustive option lists
Solve a specific task | How-to | Ordered steps, decision points | Theory digressions
Look up a fact/API | Reference | Tables, precise definitions | Narratives, opinions
Understand rationale | Explanation | Why, trade-offs | Step-by-step instructions

### Decision Checklist
1. Is there a clear success end-state? → Tutorial.
2. Is user mid-task with context? → How-to.
3. Is it schema or invariant? → Reference.
4. Is it rationale or trade-off? → Explanation.

### Style Rules (Excerpt)
- Active voice, present tense.
- Prefer short sentences (<20 words).
- One concept per paragraph.
- Code fences: specify language (`yaml`, `bash`, `go`).
- Related Docs footer with ≥2 cross-links.
- Stability markers: Stable / Provisional / Experimental (Reference & Explanation only).

### Stability Markers
Format: `(Stability: Stable)` after H1 or in tables.

### Doc Update Checklist for PRs
- [ ] Chose correct quadrant
- [ ] Added/updated Related Docs footer
- [ ] Included stability marker if reference/explanation
- [ ] No duplicated canonical YAML (only in configuration.md)
- [ ] Cross-links use relative paths

### Examples
- Tutorial: `tutorials/run-kind.md`
- How-to: `how-to/tune-spot-percentage.md`
- Reference: `reference/annotations.md`
- Explanation: `explanation/architecture.md`

### When Unsure
Open PR draft with rationale section; maintainers will guide placement.
