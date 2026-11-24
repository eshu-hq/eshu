# AGENTS.md - storage/postgres/gomodules guidance

## Read first

1. `README.md`
2. `store.go`, including the measurement on `pageQuery`
3. `go/internal/storage/postgres/AGENTS.md`

## Invariants

- One page examines at most `scannercomponent.GoModulePageSize` candidates. The
  go.mod probe must stay inside that window.
- Plan a repository on `has_go_module OR has_go_evidence`. Do not remove the second
  branch: a repository that lost its `go.mod` is otherwise never claimed, so its Go
  evidence never retracts. `has_go_evidence` reads only the scope's active generation,
  so the repository drops out after the trusted `no_go_modules` generation lands.
- The `'go-reachability://'` literal in `pageQuery` must match
  `scannercomponent.GoReachabilityScopeID`; `store_test.go` pins it.
- The page arithmetic matches `scannercomponent.GoModulePageWindow`. Change both
  together and keep the live rotation test green.
- Any change to `pageQuery` needs a fresh `EXPLAIN (ANALYZE, BUFFERS)` on
  representative data, per `eshu-postgres-rigor`.
- `GetGoModuleRepository` is the only single-repository read, for the
  out-of-rotation operator path (eshu-hq/eshu#10002). It narrows the page's
  candidate predicates to `source_key = $1` with the same go.mod and
  Go-evidence filters, and counts total candidates over the page's candidate
  set so the fencer's slot count matches the rotation. Its scope-prefix
  literal must match `scannercomponent.GoReachabilityScopeID` like the page's
  does; `single_repository_test.go` pins the shared predicates. Any change to
  `singleRepositoryQuery` needs the same fresh EXPLAIN treatment.
- `FenceStaleGoEvidence` is the only write. Do not add another or a schema change here.
  Both updates name the scope and the generation the page read, so a successor activated
  after that read makes the fence a no-op.
- The fence's lock order is the `ingestion_scopes` row, then the generation, then each
  holder's work items in `scope_id` order. The scope row first is the #7389 order every
  blocking path uses, Ack included. Do not reverse it to match statement order in
  `projector_queue_sql.go`, which is declaration order, not execution order.
  `TestFenceQueuesBehindAnAckHoldingTheScopeRowLive` deadlocks if it flips.
- The retraction and the holder reopen commit together. Do not move the reopen out of the
  transaction: a crash would leave a retracted generation whose holders were never told.
- Do not take a holder's scope row or any row of the successor's work in the fence.
- Each fence is its own transaction with a `lock_timeout`, and the pass stops at the first
  error. Do not batch fences: one transaction across scopes would hold their rows in page
  order and could wait on another path's order.
- The fence leaves the facts and the superseded generation in place. Only repository
  retirement deletes them.
- Keep the CASE guard on `evidence_left_at`: the subquery must run only for a skewed row.
  The EXPLAIN in (#10055) holds its loop count.
- The holder read answers from `supply_chain_impact_finding_supports`, never the finding facts
  (#11197). `Store.WithSupportsReadiness` is required by `FenceStaleGoEvidence` and fails closed
  on an empty binding. Check 1 runs after the timeouts and before the pointer detach. The holder
  statement (`HolderStatement`) embeds the same predicate text byte-for-byte as its first FROM item,
  so the verdict and the holders are one snapshot: keep it one statement, never add a readiness
  statement after it, never edit the injected text, and never pass its NULL `scope_id` row to the
  reopener. A not-ready verdict rolls the fence back even when holder rows came back. Keep the `ORDER BY ... COLLATE "C"` lock order of the holder reopen.
- `HolderReopener` is injected because no subpackage imports the postgres root. A store with
  no reopener refuses to fence.

## Verification

```bash
ESHU_POSTGRES_DSN=... ESHU_POSTGRES_DISPOSABLE=1 go test -count=1 ./internal/storage/postgres/gomodules
```
