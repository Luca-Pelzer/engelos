# Security Policy

## Supported Versions

engelOS is in pre-alpha (Phase 0). Once releases start (Phase 1, late 2026), this
table will be updated:

| Version | Status |
|---|---|
| `main` (HEAD) | Receives security fixes |
| Latest release | Receives security fixes |
| Older releases | No support - please upgrade |

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues,
discussions, or pull requests.**

Instead, please use one of:

1. **GitHub Security Advisories** - preferred, end-to-end encrypted, integrates
   with our patch workflow. Once the repo is public:
   <https://github.com/Luca-Pelzer/engelos/security/advisories/new>

2. **Email** - `security@engelos.org` (not yet active in Phase 0; until then,
   reach Luca via Twitch DMs at twitch.tv/engelswtf).

When reporting, please include:

- A description of the vulnerability
- Steps to reproduce
- Affected versions (including commit SHA if from `main`)
- Potential impact (data disclosure, RCE, auth bypass, etc.)
- Suggested mitigation if you have one

## Response Timeline

We aim for the following response targets. These are best-effort during Phase 0-2:

| Step | Target time |
|---|---|
| Acknowledge receipt | 72 hours |
| Initial assessment | 7 days |
| Patch development | depends on severity |
| Coordinated disclosure | 90 days from report (negotiable) |

Critical issues with active exploitation will be prioritized over the timeline.

## Scope

### In-scope

- The engelOS core daemon (this repository)
- The SDK (`pkg/sdk/`)
- The official Cloud service (`engelos.com`, when launched)
- Official Docker images at `ghcr.io/engelswtf/engelos`

### Out-of-scope

- Third-party plugins (report to the plugin author)
- Self-hosted instances misconfigured by their operator (e.g. exposed without auth)
- Denial-of-service via abusive resource consumption with no auth bypass

## Hall of Fame

Once we have our first reports, we'll list reporters who consent to public credit
here. Until then, this section is intentionally empty.

## Public Disclosure & CVE

When a vulnerability is fixed, we will:

1. Publish a GitHub Security Advisory with a CVE ID where applicable
2. Add an entry to `CHANGELOG.md` under the affected release
3. Notify subscribers via the engelOS Discord and `@engelos` on Twitter
4. Credit the reporter (with consent)

## Why AGPL helps with security

The AGPL license means anyone running engelOS as a service must share their
modifications. This protects users from forks that quietly patch out security
features. If you find such a fork, please tell us.

## Bug Bounty

There is no paid bug bounty program yet. Once we are revenue-positive (Phase 3,
projected 2028), we plan to start one. Until then, our gratitude and (with your
consent) public credit are what we can offer.
