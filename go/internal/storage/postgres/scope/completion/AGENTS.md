# AGENTS.md — Postgres scope completion store guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for the parent scope leaf conventions.
3. `../../../AGENTS.md` for Postgres storage conventions.
4. `queue.go` and `fanout.go` for the lease claim/fanout path,
   `producer_readiness.go` and `quiescence.go` for the readiness floor.

## Invariants

- Keep the package clause as `package completionstore`; callers import the
  `storage/postgres/scope/completion` path without an alias.
- Keep the lease fencing (`FOR UPDATE SKIP LOCKED`), claim/heartbeat/retry
  transitions, and quiescence predicates byte-identical when moving code;
  predicate or lifecycle changes need their own issue with queue proof per
  `eshu-postgres-rigor` (selection, claim, idempotency, stale-lease
  behavior — never serialize workers or shrink batches as the fix).
- Keep `FanoutCrossScopeCompletionQuery` exported: the staying root plan
  tests EXPLAIN the shipped constant. Do not hand-copy the SQL into a
  test.
- Never import the parent `postgres` package from production code here:
  `cmd/reducer` and root `internal/storage/postgres` import this package
  directly, and importing back would be a cycle. Tests import it only for
  the `SQLDB` wrapper type and `ApplyBootstrap`.
- Shared test seed helpers exist as twin copies (Go test-only symbols do
  not cross package boundaries): `seedAWSCloudRuntimeDriftGeneration` and
  `awsCloudRuntimeDriftAdmissionLiveDB` twin the root
  `aws_cloud_runtime_drift_admission_live_helpers_test.go`. Keep each pair
  behavior-identical; the pointer comment names the twin.
- The harness-bound concurrency and snapshot live proofs stay in root until
  the `container/image/` step (they open their database through that
  family's ACK capability harness); the ReducerQueue-driving batch and
  scale-plan proofs stay until the `queue/reducer/` step (unexported `database`
  field). Move each with its owning step, not earlier.

## Common changes

- Changing a claim, retry, or fanout predicate needs the live concurrency
  proofs (staying root files) plus a contention/lease-expiry argument.
- A new producer domain needs a readiness-catalog entry in
  `producer_readiness.go` and a case in `producer_readiness_test.go`.
- Changing the quiescence probe SQL needs `quiescence_test.go` shape cases
  plus the `quiescence_live_test.go` three-state proof updated.

## Failure modes

- Importing the parent `postgres` package from production code creates an
  import cycle.
- Duplicating (instead of exporting) shared production SQL or predicates
  lets the copies drift on locking or lifecycle behavior.
- Treating a collector kind with no registered scope as not-ready defers
  every consumer intent to the full bound with no backoff.
- Dropping `FOR UPDATE SKIP LOCKED` lets two concurrent claimants take the
  same event; dropping the lease-owner match lets a stale claimant
  complete a re-claimed event.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/scope/completion/... -race -count=1
go vet ./internal/storage/postgres/scope/completion/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
