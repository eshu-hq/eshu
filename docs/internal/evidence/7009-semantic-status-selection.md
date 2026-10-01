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
from checkpoint/replay cost and verify that unused status sections disappear
from this route's read series after deployment.
