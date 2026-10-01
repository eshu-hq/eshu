# Semantic extraction status read selection (#7009)

## Contract and local proof

`GET /api/v0/status/semantic-extraction` selects the existing semantic queue,
budget, and audit SQL through `StatusStore.ReadStatusSnapshotFiltered`. The
selection returns only `SemanticExtractionStatus`; it never projects a partial
raw snapshot into a full health report. The default mode retains the standard
status sections and its existing optional-section flags. Unsupported or
contradictory selections fail before a transaction or SQL. The route retains
redacted static provider profiles and the existing response envelope and truth.
It also retains the guarded reader checkpoint, read-only repeatable-read
transaction, deadline, rollback, and status telemetry. An unrelated status
section failure cannot fail this route; semantic SQL and freshness failures
still do.

A compiled behavioral regression fails when the endpoint is temporarily
restored to its prior full-read wiring: it observes a full selection rather
than semantic-only. It passes with the changed production endpoint. Focused
store tests assert the actual semantic SQL is the only statement issued;
runtime tests assert one transaction, commit, and rollback on SQL failure.

## Performance Evidence:

Before implementation, a read-only scratch theory probe used the actual
exported semantic SQL and decoder, `StatusStore`, production status handler,
and response envelope under one PostgreSQL read-only repeatable-read snapshot
on the ops-qa standby. The full-first pair took 3.676271583 seconds for the
full read and 0.034205792 seconds for the semantic SQL path. The narrow-first
pair took 0.038045167 seconds for semantic and 2.707573125 seconds for full.
Complete response data and truth matched in both orders, with response hash
`05efd51348c96a2ab6e7f99e9674887e3920662dcfd0aa3a26d74b6638def65b`.
Seven hermetic edge cases passed. This comparison used an empty live queue and
excludes the freshness checkpoint, transport, and representative API/MCP
concurrency. It is theory evidence, not deployed endpoint latency or a claim
that replay or checkpoint failures are fixed. A finished-path paired run remains
required before a latency claim or deployment decision.

## Observability Evidence:

The existing `eshu_dp_status_snapshot_read_duration_seconds{read,outcome}`
continues to time semantic SQL under `read="semantic_extraction"`.
`postgres.status_snapshot{phase,outcome}` and reader-stage telemetry continue
to show transaction and checkpoint failures. No metric label, span name, or
failure classification changes. Operators can distinguish semantic SQL cost
from checkpoint/replay cost in traces correlated with this route. Read-series
metrics also include other routes and background full reports; they cannot
prove that this particular route stopped reading an unrelated section.

## Finished-path observations

A bounded read-only same-snapshot comparison through the changed handler
returned HTTP 200 for semantic-only selection in 0.766944583 seconds, followed
by HTTP 500 for full selection after 19.748284666 seconds. The full failure was
`context deadline exceeded`. This is non-comparable: it proves one successful
semantic read, not response parity, a speedup, deployed p95, or checkpoint
reliability. An earlier full-first 500/500 observation discarded error bodies
and cannot establish independent candidate failure. Both owned forwards closed.
The corrected single narrow-first pair succeeded under a read-only
repeatable-read snapshot on the standby, with a five-second statement timeout.
Semantic selection took 0.067136375 seconds; full selection took 3.328458292
seconds. Both responses were HTTP 200 with identical complete ordered data and
truth and the response hash above. Total diagnostic time was 3.897268916
seconds. Explicit rollback succeeded, database close was checked, and the
owned port-forward was closed and its listener absence verified. This measures
storage selection through the new handler; it does not measure an old-versus-new
complete runtime handler, checkpoint acquisition, transport, deployed p95, or
100-user capacity. It does not establish worst-case semantic backlog latency.

Local old/new production-loader differentials matched for five fixture cases.
Fourteen fixture envelopes matched between binaries using the original and
changed production handler, including errors; the negative parity seed still
failed as intended. Snapshot cancellation and cancellation-after-commit tests
now cover full, filtered, and semantic-only modes. Targeted snapshot tests pass
under the race detector. Bypassing production selection validation made the
invalid-mode storage and runtime tests fail; the actual guard passed both.
Independent final review, attestation, and promotion gates remain required.
