// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

# AGENTS.md — backfill guidance for LLM assistants

## Read first

1. `go/internal/storage/postgres/backfill/doc.go` — engine contract and the
   `Store` seam
2. `go/internal/storage/postgres/backfill/store.go` — `Store` fields and the
   two root-supplied constructors
3. `go/internal/storage/postgres/backfill/engine.go` — pass shape
   (`RunAllRelationshipEvidence`, batch walk, fan-in)
4. `go/internal/storage/postgres/ingestion_backfill_seam.go` — root side of
   the seam (`backfillStore`, post-commit tx, skip orchestration)
5. `go/internal/storage/postgres/README.md` and `AGENTS.md` — parent package
   invariants (idempotency, lock order, dedupe fencing)

## Invariants this package enforces

- **No parent import** — the engine imports `db`, `facts`,
  `relationships`, `scope`, `telemetry`, never the parent
  postgres package. Root-owned behavior arrives through `Store`
  constructors.
- **Partition-keyed publication** — one readiness row and one memo row per
  distinct (scope, generation) partition, never per repository or per
  batch. The hermetic dupkey tests pin this without a database; the live
  proof pins it against real `ON CONFLICT` semantics.
- **Differentials derive, never copy** — root proofs bind the shipped
  constants and exported arg builders. Do not freeze SQL by hand.
- **Test placement** — fake-driven engine tests stay `package backfill`
  with local doubles (`fakes_test.go`); targeted engine proofs live in the `targeted` child package. External
  child tests may call root `IngestionStore` entry points.

- **Block telemetry is a closed set** — the `Block*` constants in
  `block_telemetry.go` are the nine blocks of #8082. A failed block ends its
  span with the error, emits one `deferred_backfill_block_failed` line, and
  records no histogram sample; a non-fatal degrade (the memo gate falling back
  to a full load) ends the block with `EndDegraded`: span Error with
  `fallback=true`, one `deferred_backfill_block_degraded` line, histogram
  sample kept, and never a failed line. Never put a scope or
  repository ID on a block. The pass tracer reaches helpers through the
  context (`withBlockTelemetry`), so helpers called from the targeted pass
  stay silent.

- **Unchanged partitions are not republished** (#8082) — the whole-pass fan-in
  skips a partition only when it was a start-of-pass memo hit, got no inserted
  evidence row, its scope is still on that generation, and it still has its
  readiness row. Keep all four conditions; a moved generation must reach the
  locked path so it keeps `generation_advanced_since_batch`.
  Keep `PublishDeferredBackfillPartitions` (the partition-scoped pass's entry)
  publishing every partition. A failed readiness check must fall back to
  publishing all partitions, never to skipping them.

## Change guidance

- Keep `Store` fields behind the seam: a new durable collaborator needs a
  constructor field, a recording fake in `fakes_test.go`, and root wiring
  in `backfillStore`.
- A behavior change needs a failing test first; a refactor needs proof the
  existing contract holds (hermetic suite plus the root suite).
- Exported SQL or arg surface changes must keep the root differentials
  green; they assert on the shipped text.
- New files carry the MIT SPDX header. Keep every file under 500 lines;
  the parent directory is over the dirgate cap, so a new file
  there needs a `Dirgate-Growth:` trailer (`go/cmd/dirgate`).
- Run `go test ./internal/storage/postgres/backfill/ ./internal/storage/postgres/`
  from `go/` before handing off.
