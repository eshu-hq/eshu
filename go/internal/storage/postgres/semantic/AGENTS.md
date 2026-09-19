# AGENTS.md — Postgres semantic-extraction queue store guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for Postgres storage conventions.
3. `extraction_queue.go`, `extraction_queue_lifecycle.go`,
   `extraction_queue_skip.go`, and `extraction_queue_observability.go`
   for the store, lifecycle, skip policy, and status snapshot.
4. `../semantic_extraction_queue_test.go` and
   `../semantic_extraction_observability_test.go` for the store contract
   (kept at root beside the shared queue fakes and the bootstrap registry
   they assert against).

## Invariants

- Keep the claim, fence, retry, dead-letter, skip, and snapshot SQL
  byte-identical when moving code; predicate or lifecycle changes need
  their own issue with EXPLAIN and contention proof per
  `eshu-postgres-rigor`.
- Preserve `FOR UPDATE SKIP LOCKED` and the lease fence on every
  mutation exactly; a lost lease returns
  `ErrSemanticExtractionClaimRejected`, never a silent overwrite.
- Keep `ReadSemanticExtractionObservability` and
  `SemanticExtractionObservabilityQuery` exported until the status leaf
  moves; the status page and proof harness read through them.
- Keep the package clause as `package semanticstore`; callers import the
  `storage/postgres/semantic` path without an alias.
- Never import the parent `postgres` package from here.

## Common changes

- Change the DDL only with the bootstrap-definition test's expected
  fragments updated in lockstep; that test asserts exact DDL substrings
  against the embedded migration source of truth.
- Change claim predicates only with the SKIP LOCKED contention test, the
  lease-fence tests, and an idempotency proof.

## Failure modes

- Importing the parent `postgres` package creates an import cycle.
- Dropping `SKIP LOCKED` lets concurrent claimants double-deliver.
- Dropping the lease fence lets a stale worker overwrite a live claim.
- Claiming backoff or non-provider rows poisons the work stream.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/... -count=1
go vet ./internal/storage/postgres/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
