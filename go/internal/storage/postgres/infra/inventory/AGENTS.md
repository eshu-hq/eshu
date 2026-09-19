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
- **Do not import the parent `postgres` package.** It imports this one.

## Verification

```bash
cd go && go test ./internal/storage/postgres/infra/inventory -count=1
ESHU_POSTGRES_DSN=... go test ./internal/storage/postgres/infra/inventory -run Live -race -count=1
```
