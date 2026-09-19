# AGENTS.md — internal/storage/postgres/infra/inventory guidance for LLM assistants

## Read first

1. `README.md` in this directory — why the table exists and what it mirrors
2. `labels.go` — `Labels` and the graph-only exclusions
3. `derive.go` — `MirrorPaths`, `MirrorRepo`, and the lock-delete-insert SQL
4. `derive_live_test.go` — the real-Postgres proofs (set `ESHU_POSTGRES_DSN`)

## Invariants you must not break

- **Parity with the graph is the contract.** A table count must equal the graph
  count for the same label, filter, and grouping. Any change to `Labels`, the
  dimension columns, or the value normalization needs a live
  graph-versus-table differential, not only unit tests.
- **Know every writer of a label before adding it.** Check every
  `MERGE (x:<Label>` in `storage/cypher` first. A label with a second writer
  (today TerraformModule and TerraformOutput, via the Terraform state
  projector) needs its other nodes identifiable by an indexed property, read
  from the graph by the query layer (`infraMixedWriterGraphSource`), and a
  graph schema index on that property.
- **Keep the transaction order: lock, delete, insert.** The advisory lock must
  be the first statement so the insert's snapshot postdates any other
  deriver's commit for the repository.
- **Never write without a transaction.** `MirrorPaths` and `MirrorRepo` error
  when the database cannot begin one. Do not add a non-transactional
  fallback.
- **No counters or increments.** Rows are keyed by `entity_id` and cleaned by
  `(repo_id, relative_path)`, so retries and duplicate delivery are
  idempotent. A counter would not be.
- **Keep the reconcile digest in step with the derive.** `reconcileDigestSQL`
  hashes the same columns with the same trimming as `insertSelectColumns`;
  `TestReconcileDigestHashesExactlyTheDerivedValues` pins that. A column added
  to one side only makes every repository look drifted, or hides real drift.
- **Reconcile repairs only a repeat mismatch, and only under the lock.** A
  Write commits content rows before its derive, so one mismatch can be a Write
  in progress. The walk reports a first mismatch as `suspect`; only the next
  cycle's re-check may repair, after a locked re-check. Never repair on the
  unlocked digest alone.
- **Only derive-aware writers may run `WriterSessionSQL`.** It tells the
  migration 109 fence triggers to skip a connection's content_entities
  writes. A binary that writes content rows without keeping this table in
  step must not open its pool through `OpenWriterDB`, which
  `runtime.OpenPostgres` uses for every service runtime.
- **Keep the fence upsert a `DO UPDATE`.** Its row lock is what makes a
  repair wait for an open unaware write; `DO NOTHING` takes no lock and lets
  a repair clear the mark while that write's rows are still invisible.
  `TestInfraInventoryFenceTriggerLabels` pins this and the trigger label
  lists to `Labels`.
- **Do not import the parent `postgres` package.** It imports this one.

## Verification

```bash
cd go && go test ./internal/storage/postgres/infra/inventory -count=1
ESHU_POSTGRES_DSN=... go test ./internal/storage/postgres/infra/inventory -run Live -race -count=1
```
