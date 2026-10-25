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
- Keep the org fan-out (`org_fanout.go`) bounded: sequential reads, each capped
  by `max_pages`, at most `maxOrganizationFanoutRepositories` per claim, started
  at the committed `FanoutCursorReader` cursor (`fanout_cursor.go`) for an
  allowlist that fits, so a stopper does not pin the repositories behind it (the
  start moves forward to the repository after a failed read; a failed read at
  the last position closes a lap that returns it to the top; a target over the
  cap always starts at the top, never rotates), and a
  stop at the first read failure other than `not_found` (which includes the 403
  the client marked `AlertsDisabled`, classified `not_found` since #8947). Report unread repositories through
  `collection_incomplete_reasons` and the fan-out counter; never move the
  cursor in `NextClaimed` or an in-process store (it commits with the generation,
  through `TargetReadState`); never retry inside a
  claim. Update `testdata/org_fanout_cassette.json` when the request sequence
  changes.
- Build the org read state (`read_state.go`) from the same fan-out outcomes the
  facts use; a repository the fan-out did not read in full MUST appear in it
  with the same reason token, and a repository read in full MUST NOT. The
  reason tokens are mirrored in `query/supply/chain/security_alert_unread_reason.go`
  and the OpenAPI enum; change them together. Repository targets write no
  state. Never put a repository name in a metric label or span attribute.
- Every generation carries `SecurityAlertTargetSet` from
  `TargetConfig.targetSet` (#8838), and its hash stays in the freshness
  digest. Removing it from the digest lets an allowlist change be skipped as
  unchanged, and the added repository then never leaves
  `no_committed_generation`.
