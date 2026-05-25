# AGENTS.md - security alert runtime guidance

## Scope

This package owns the claim-driven hosted provider security-alert runtime. It
may resolve credentials, call allowlisted provider APIs, and return source facts
to `collector.ClaimedService`.

## Rules

- Emit only `security_alert.repository_alert` source facts.
- Never emit reducer-owned impact, readiness, suppression, or remediation truth.
- Never include repository names, alert URLs, tokens, or credential env values in
  metric labels or failure messages.
- Keep provider calls behind explicit credentials and repository allowlists.
- Classify provider failures for retry/terminal handling instead of returning
  raw upstream bodies.
- Add or update `ClaimedSource.NextClaimed` tests for new provider behavior.
