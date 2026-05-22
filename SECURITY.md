# Security Policy

## Supported versions

Wave is pre-1.0. Until v1.0.0 ships, only the **latest minor release**
receives security fixes. After v1.0.0 this table will expand to cover
the most recent two minors.

| Version | Supported |
|---------|-----------|
| latest 0.x minor | ✅ |
| older 0.x minors | ❌ |

## Reporting a vulnerability

**Do not file a public GitHub issue for security bugs.** Reports filed
publicly give attackers time to exploit before a fix is out.

Please use
[**GitHub Security Advisories**](https://github.com/luowensheng/wave/security/advisories/new)
to report privately. A maintainer is notified directly and no public
ticket is created.

Please include:

- A description of the issue and its impact
- Steps to reproduce (or a minimal proof-of-concept)
- The Wave version(s) affected
- Any suggested mitigation

## What to expect

- **Acknowledgement** within **3 business days**.
- **Initial assessment** within **7 business days** — severity, scope,
  whether it's a duplicate.
- **Fix and disclosure** within **90 days** for high/critical issues,
  longer for low-severity if a coordinated release schedule helps.
- **Credit** in the release notes for the reporter (opt-out available).

If we can't reach you for clarification within 30 days the report may
be closed without a fix; you can re-open it any time.

## Scope

Vulnerabilities in:

- The `wave` binary and library code under this repository
- Official Docker images published from this repository
- The default `examples/apps/` configurations (only inasmuch as they
  reveal a flaw in Wave itself, not in user-modified configs)

Out of scope:

- Vulnerabilities in third-party plugins not maintained in this repo
- Configuration issues in user-deployed Wave servers (e.g., user
  declines to set `cors_origins` or `auth:`)
- Reports relying on physical access to the host or compromised
  credentials supplied by the operator
- Denial-of-service via unbounded request bodies when the operator
  has explicitly disabled body-size limits

## Safe harbor

We will not pursue legal action against researchers who:

- Make a good-faith effort to follow this policy
- Avoid privacy violations, data destruction, and service degradation
- Do not exploit the vulnerability beyond what's necessary to confirm it
- Give us reasonable time to remediate before public disclosure

## Hall of fame

Reporters who responsibly disclose are credited at
`docs/security/hall-of-fame.md` (created on first valid report).
