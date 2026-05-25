# Security Policy

## Supported Versions

Spotalis is pre-1.0. Only the latest `0.x` release receives security fixes.

| Version | Supported |
| ------- | --------- |
| 0.0.x   | ✅        |
| < 0.0.5 | ❌        |

## Reporting a Vulnerability

Please report security vulnerabilities **privately** via GitHub Security Advisories:

1. Open the project's [Security tab](https://github.com/yachiko/spotalis/security/advisories).
2. Click **Report a vulnerability**.
3. Provide a clear description, reproduction steps, and the impact you observed.

Do not open a public issue or PR for suspected vulnerabilities.

## What to Expect

- **Acknowledgement** within 5 business days.
- **Initial assessment** (severity, scope, affected versions) within 10 business days.
- **Fix or mitigation timeline** communicated once the assessment is complete.
- **Public disclosure** coordinated with the reporter; advisory and patched release published together.

## Out of Scope

- Misconfiguration of clusters that deploy Spotalis (e.g. running with overly permissive RBAC the chart does not request).
- Third-party dependencies whose upstream maintainers have an active advisory channel — please report there first and we'll backport the fix.

## Defensive Measures

Spotalis is shipped as a distroless, non-root image and runs with the minimal RBAC the controller and webhook need. PRs that tighten the default permissions are welcome.
