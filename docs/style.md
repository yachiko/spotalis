# Documentation Style Guide

Purpose: Shared conventions for all Spotalis docs.

## Voice & Tone
- Concise, task-oriented.
- Assume reader knows Kubernetes basics.
- Avoid marketing language.

## Headings
Level | Use
----- | ----
H1 (#) | Page title (one per file)
H2 (##) | Major sections
H3 (###) | Subsections when necessary (avoid >3 levels)

## Language Conventions
- Use "workload" for Deployment/StatefulSet collectively.
- Use "on-demand" (hyphenated) consistently.
- Numbers: spell out one–nine; use numerals ≥10.

## Code & Examples
- Use fenced blocks with language: yaml, bash, go.
- Keep examples minimal; link to reference for exhaustive fields.
- Single canonical configuration YAML only in `reference/configuration.md`.

## Annotations & Keys
- Always surround annotation keys with backticks.
- Present tables with consistent column order.

## Stability Markers
Add after title or in tables. Legend:
- Stable: Backward compatible, monitored.
- Provisional: May change; gathering feedback.
- Experimental: Behind feature flag or likely to change.

## Related Docs Footer
Each page ends with:
```
## Related
- <link 1>
- <link 2>
```
Minimum 2 links where relevant.

## Admonitions (Optional)
If adopting later tooling. For now, emulate with bold prefixes (e.g., **Note:**, **Warning:**).

## Formatting Anti-Patterns
- Wall-of-text paragraphs >6 lines.
- Mixing rationale into reference tables.
- Repeating identical YAML blocks across pages.

## Changelog Awareness
When changing behavior, update relevant reference page in same PR.
