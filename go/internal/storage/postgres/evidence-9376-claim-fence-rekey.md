# Claim fence branch re-key: no-regression evidence (#9376)

`commitScopeGeneration` and `reprojectActiveGeneration` re-keyed the
claim-fence branch from session-nil to path identity
(`s.ClaimPlane == nil` → in-tree heartbeat, unchanged; plane configured
without a fence session → explicit fail-closed error).

## No-Regression Evidence:

- Zero SQL change: both paths execute the identical statements in the
  identical order as before. The in-tree path is byte-identical behavior,
  proved by the pre-existing in-tree statement-sequence tests (green,
  untouched). The plane path previously reached the heartbeat only via the
  session-nil branch; it now returns a fail-closed error instead. (A
  failed pin still returns before `BEGIN` at `ingestion.go:174-176`; the
  new defensive return sits after `BEGIN`, covered below.)
- Added cost: one nil-pointer comparison per commit on a path dominated
  by transaction begin, fact persistence, and commit — unmeasurable
  against that budget; no new query, lock, retry, or backoff.
- Transaction and lock behavior unchanged: no lock acquired, released, or
  reordered; no lease, batch, or worker-count change. Fence interleaving
  argument: the fail-closed return follows `BEGIN`, and the existing
  deferred rollback — the same path a heartbeat error takes — guarantees
  no half-fenced transaction can commit; a stale writer gets an error,
  never a commit.
- Retryable handling is intended: the plain error takes the default
  retryable route, and that is safe because every retry must re-pin
  first — a truly stale writer is terminally refused at the pin gate
  (`ReasonStaleFence`), so retry preserves liveness for transients
  without ever letting a stale commit through. No new `Reason` was added
  for this unreachable defensive branch.
- Proof: 16 fence-related storage tests green, including
  `TestPlaneCommitFailedPinNeverFallsBackToSameTxHeartbeat` (failed pin →
  rejection, zero `workflow_claims` statements) and
  `TestPlaneCommitPinsWithoutSameTxHeartbeat` (success → 1 pin, zero
  same-tx heartbeat statements); mutation v1/v2 both failed as predicted
  and were reverted.

## No-Observability-Change:

No new metrics, spans, or logs. The fail-closed error returns through the
existing error path with the existing message conventions; the pre-existing
`TestClaimFenceMismatchLogsAtError` span/log behavior is untouched.
