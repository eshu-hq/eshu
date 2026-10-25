# Agent instructions: runtime declaration family

Read `doc.go` and `README.md` first.

## Invariants

- MUST NOT import root package `query`. Root's `handler.go` owns the
  router and imports this package for its handler field, so the reverse
  import cycles. If a change needs something only root exposes, either a
  leaf equivalent already exists (`querycontract`, `tracing`) or it does
  not belong in this family.
- Capabilities are registered in `query/contract`
  (`capability_matrix.go`), not here -- root owns the router and always
  links into production. This package only declares the `Support`
  constructor the TestMain registers. Do not register in this package's
  non-test code.
- `declarationHandlerTracer` MUST stay a package-local var seeded from
  `tracing.HandlerTracer`. The span guard test swaps it; a second
  tracer var, or seeding from anywhere else, breaks its isolation or
  changes emitted spans.
- `startQueryHandlerSpan` is a family-local copy of root's
  `handler_tracing.go` helper (mirroring
  `supply/chain/handler_tracing.go`). It MUST stay behavior-identical to
  both sources: `handler_tracing_test.go` pins the emitted span name and
  route/capability attributes, so drift fails loudly. Do not extend it
  with family-specific semantics; add a new helper instead.
- Every read MUST stay bounded (limit clamp + limit+1 probe, 10s graph
  budget, at-most-100 repo_ids, keyset cursor). Widening a bound changes
  the queryplan-pinned page shapes in
  `go/internal/queryplan/testdata/source-coverage/second.yaml`; re-pin
  that file in the same change.
- The out-of-grant empty page MUST NOT read the graph. Probing would let
  a scoped caller distinguish "out of grant" from "granted but empty".
- A version comparison without the `runtime` filter MUST stay a 400.
  Major numbers are meaningless across runtimes (Node 20 vs Python 20),
  and answering anyway would compare unrelated versions.
- Unknown request fields MUST stay rejected. A misspelled filter that is
  silently dropped answers with every declaration, which reads as a clean
  fleet; the MCP route forwards arguments unselected for exactly this
  reason.
- The Mount registration MUST stay a literal
  `mux.HandleFunc("POST /api/v0/runtime-declarations/query",
  h.listDeclarations)`. The #5584 route-serves-data registry gate matches
  the literal registration text; `listRoute` exists only for the span
  attribute and its test. Keep the two identical.
- A failed graph read answers `querycontract.WriteGraphReadError` first,
  then `tracing.WriteServerFailure` with the step's fixed message
  constant. Never write `err.Error()` into a response body (#7674). The
  constants live in `handler.go`.
- Files must stay under 500 lines. Split by concern rather than growing
  them.

## Exported symbols and why each is exported

Every export below names a staying root caller -- no speculative API. Do
not export a new symbol without adding its caller to this list.

- `Handler` -- root `handler.go` field, `cmd/api` and `cmd/mcp-server`
  wiring, staying root tests.
- `LoadSection`, `Section`, `RuntimeSummary` -- the repository context
  section read in `repository/context.go`.
- `Support`, `Capability` -- the capability id and constructor the family
  TestMain registers and root's matrix row calls.

## Test doubles

Use the in-package fakes (`fakeGraph`, the request builders in
`handler_test.go`). Do NOT import root package `query` from tests: the
former cycles, and this family needs neither (stdlib + own fakes only).
Live tests (`live_*_test.go`, tag `live_nornicdb_answer_truth`) run the
production handler over the production upsert Cypher against a disposable
Neo4j; see `live_helpers_test.go` for the exact invocation.

## Verification (paste all)

```bash
cd go && go build ./internal/query/...
cd go && go vet ./internal/query/ ./internal/query/runtime/declaration/ ./internal/query/querycontract/
cd go && go test ./internal/query/runtime/declaration/ -count=1
cd go && go test ./internal/query/ -count=1
cd go && go test ./internal/queryplan/ -count=1
bash scripts/verify-package-docs.sh
git diff --check
gofumpt -l <touched files, repo-relative>
```
