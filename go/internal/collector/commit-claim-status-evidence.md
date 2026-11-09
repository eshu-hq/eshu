# Commit lane status evidence (#10450)

The single-call commit lane admits a complete result whose statuses are
all non-signaling (class `complete`, no partial flag) and still rejects
any failure, warning, partial, or other-class status with the existing
terminal status verdict before the fence probe. Code:
`commit_claim_facts.go` (`countSignalingStatuses`); the handler path
flows through the same check via `CheckCommitClaimCarrier`.

- No-Regression Evidence (#10450): admission-predicate-only change. The
  envelope mapping, fence probe, and single committer write are untouched,
  so an admitted complete-marker carrier executes byte-identical
  instructions to the always-admitted zero-status shape. Baseline: the
  zero-status and signaling-rejection tests pinning the prior disposition
  pass unmodified. After: `go test ./internal/collector/
  ./internal/collector/extensionhost/ -count=1` passes (collector 0.135s,
  extensionhost 0.639s) and `-race` passes (1.217s, 28.367s) on the stub
  harness with no database backend (no Postgres/Neo4j involved: pure
  in-memory admission). Input shape: single-call complete results with
  0..n statuses; satellites emit exactly one. Row counts: admitted
  carriers record one envelope per fact as before; rejected carriers
  record 0 envelopes with 0 store or committer calls. No new queries,
  locks, goroutines, or retries; the predicate is a bounded scan of the
  statuses slice.
- No-Observability-Change (#10450): no new metrics, spans, or log lines.
  Refusals keep the existing error shape, which the handler already maps
  to its 400 invalid envelope; commits flow through the existing silent
  committer boundary, so fact counters stay silent as before.
