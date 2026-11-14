// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

# Evidence: hosted-readiness failure envelope and log line (#10658)

Hot-file evidence for `status_hosted_readiness.go`'s `getHostedReadiness`
failure path. The `verify-performance-evidence.sh` gate classifies
`status_hosted_readiness.go` as a changed hot file by content, not by
location: `is_hot_path_by_content` matches the `MATCH` in the
`readHostedRepositoryCount` Cypher string
(`status_hosted_readiness.go:160`,
`"MATCH (r:Repository) RETURN count(r) as count"`). `go/internal/query`
carries no `is_hot_path_by_location` arm, and that Cypher statement is
untouched by this change. This note carries the required `No-Regression
Evidence` and `Observability Evidence` markers.

## What changed

`getHostedReadiness` used to answer an unmapped status snapshot read
failure with a bare 500 (`WriteServerFailure` only) and no log line: no
error code, no capability, nothing an operator can grep at 3 AM. Failures
now go through `writeHostedReadinessReadFailure`: the existing
`WriteGraphReadError` fence verdicts are unchanged, a bare
`context.DeadlineExceeded` from the bounded 5s snapshot read (which
carries no fence sentinel, so the shared mapper cannot see it) answers
the same retryable 503 (`backend_unavailable` under
`hostedReadinessStatusCapability`, with `Retry-After`), and anything else
answers the shared failure envelope via `tracing.ServerFailureEnvelope`
(500 `internal_error` under the same capability, 499 on client cancel)
plus one ERROR log line (`check=status_snapshot`,
`failure_class=status_snapshot_unavailable`, the wrapped error,
`request_id`), skipped on client cancel. Error text never reaches the
body.

No-Regression Evidence (#10658): this is not a performance change: no
Cypher statement, graph write, query plan, loop, retry/backoff, or lease
behavior changed. Every added statement sits on an already-failing
response path — the handler has already decided to return 5xx before any
of them runs — so the success-path row shape, ordering, and snapshot
read budget are untouched. A snapshot read that outruns its bounded
budget previously answered 500; it now answers the same retryable 503
the fence verdicts already use, with no new round trip, lock,
transaction, or graph call. Verified with `cd go && go test
./internal/query/ -run 'HostedReadiness' -count=1` (ok, this session)
and `cd go && go test ./internal/query/... -count=1` (full tree, all
ok, zero failures, this session), plus `go vet ./internal/query/`
(clean) and `git diff --check` (clean). The new regression tests
(`status_hosted_readiness_failure_test.go`) were RED before the fix —
the envelope 500 body was bare, the budget timeout answered 500, and no
ERROR line was logged — and GREEN after.

Observability Evidence (#10658): one new log signal — `hosted readiness
status read failed` at ERROR, emitted only on the unmapped 500 path
(never on client cancel or a mapped 503), carrying `check`,
`failure_class`, `error`, and `request_id` (from
`X-Correlation-ID`/`X-Request-ID` or generated). No new metric,
instrument, label, span, or dashboard panel; the route stays under the
existing `eshu_dp_api_request_duration_seconds` /
`eshu_dp_api_request_errors_total` middleware. The new "Hosted readiness
read failures (#10658)" row in
`docs/public/observability/telemetry-coverage-runtime-surfaces.md` names
the handler, both signals, and the fault-only emission rule; `scripts/verify-telemetry-coverage.sh`
(with `ESHU_TELEMETRY_COVERAGE_BASE=origin/main`) agrees with no new
untracked stages (this session).
