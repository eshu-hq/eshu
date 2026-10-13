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
  `relationships`, `scope`, `telemetry`, `workflow`, never the parent
  postgres package. Root-owned behavior arrives through `Store`
  constructors.
- **Partition-keyed publication** — one readiness row and one memo row per
  distinct (scope, generation) partition, never per repository or per
  batch. The hermetic dupkey tests pin this without a database; the live
  proof pins it against real `ON CONFLICT` semantics.
- **Differentials derive, never copy** — root proofs bind the shipped
  constants and exported arg builders. Do not freeze SQL by hand.
- **Test placement** — fake-driven engine tests stay `package backfill`
  with local doubles (`fakes_test.go`); anything needing root
  collaborators (`IngestionStore`, commit path, live DSN) lives in the
  root package.

## Change guidance

- Keep `Store` fields behind the seam: a new durable collaborator needs a
  constructor field, a recording fake in `fakes_test.go`, and root wiring
  in `backfillStore`.
- A behavior change needs a failing test first; a refactor needs proof the
  existing contract holds (hermetic suite plus the root suite).
- Exported SQL or arg surface changes must keep the root differentials
  green; they assert on the shipped text.
- New files carry the MIT SPDX header. Keep every file under 500 lines;
  the dirgate cap for the parent directory is pinned in
  `scripts/lib/dirgate-grandfather.tsv`.
- Run `go test ./internal/storage/postgres/backfill/ ./internal/storage/postgres/`
  from `go/` before handing off.
