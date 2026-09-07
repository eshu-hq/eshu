# Agent instructions: codeowners family

Read `doc.go` and `README.md` first.

## Invariants

- MUST NOT import root package `query`. Root's
  `family_codeowners_shim.go` already imports this package for its
  compatibility alias, so the reverse import cycles. If a change needs
  something only root exposes, either a leaf equivalent already exists
  (`querycontract`, `queryauth`, `queryspan`) or it does not belong in
  this family; ask before adding one.
- Capabilities are registered in ROOT
  (`contract_capability_matrix_ext.go`), not here -- root owns the router
  and always links into production. This package only declares the
  `OwnershipSupport` constructor the TestMain registers. Do not register
  in this package's non-test code.
- `queryHandlerTracer` MUST stay a package-local var seeded from
  `queryspan.HandlerTracer`. The span guard test swaps it; a second
  tracer var, or seeding from anywhere else, breaks its isolation or
  changes emitted spans.
- `startQueryHandlerSpan` is a family-local copy of root's
  `handler_tracing.go` helper (mirroring
  `supplychain/handler_tracing.go`). It MUST stay behavior-identical to
  both sources: `handler_tracing_test.go` pins the emitted span name and
  route/capability attributes, so drift fails loudly. Do not extend it
  with family-specific semantics; add a new helper instead.
- Every read MUST stay bounded (limit clamp + limit+1 probe, 10s graph
  budget, manifest lookup cap). Widening a bound changes the
  queryplan-pinned page shapes.
- The out-of-grant empty page MUST NOT read either store. Probing the
  stores would let a scoped caller distinguish "out of grant" from
  "granted but empty".
- The precedence order is the contract: manifest exact/derived first,
  CODEOWNERS last-match second, zero value last. Do not reorder the
  branches in `resolveEffectiveRepositoryOwner`.
- Files must stay under 500 lines. Split by concern rather than growing
  them.

## Exported symbols and why each is exported

Every export below names a staying root caller -- no speculative API. Do
not export a new symbol without adding its caller to this list.

- `Handler` -- root `handler.go` field, `cmd/api` and `cmd/mcp-server`
  wiring, staying root tests (via the root `CodeownersOwnershipHandler`
  alias).
- `CodeownersOwnershipRow` -- the handler response rows and the moving
  tests (kept as-is; JSON shape unchanged).
- `EffectiveRepositoryOwner`, `EffectiveOwnerSourceServiceCatalog`,
  `EffectiveOwnerSourceCodeowners` -- the handler `effective_owner`
  value and its provenance labels (kept as-is).
- `CodeownersOwnershipCyphers`, `CodeownersLastMatchOwnerCypher`, and the
  `Cypher` field of `codeownersOwnershipGraphQuery` -- the staying
  queryplan production-binding test
  (`queryplan_legacy_production_binding_test.go`), which pins the exact
  emitted Cypher text through this package.
- `OwnershipSupport` -- this package's TestMain registration (and the
  follow-up that should point root's matrix row at it).

## No-Cypher-text rule

The queryplan-pinned builders relocate only. If you touch one, run
`go test ./internal/queryplan/` and the in-package binding tests; a
`source_sha256` change means your edit altered declaration bytes --
revert unless the rename was forced, and never let `cypher_sha256`
change. `file:` paths in
`go/internal/queryplan/testdata/{handler-hot-cypher,hot-cypher}.yaml`
and `query-source-coverage.yaml` MUST track the builder's real location.

## Family-local copies (keep byte-identical, change in pairs)

`startQueryHandlerSpan` + `queryHandlerTracer` (root's
`handler_tracing.go`, `supplychain/handler_tracing.go`). Each carries a
provenance comment naming its root source, and `handler_tracing_test.go`
enforces the shared operator contract.

## Test doubles

Use the in-package fakes (`recordingCodeownersGraphReader`,
`fakeCodeownersCorrelationStore`,
`recordingCodeownersLastMatchGraphReader`,
`codeownersScopedTestAuthContext`). Do NOT import root package `query`
or `querytestutil` from tests: the former cycles, and this family needs
neither (stdlib + own fakes only).

## Verification (paste all)

```bash
cd go && go build ./internal/query/...
cd go && go vet ./internal/query/ ./internal/query/codeowners/ ./internal/query/querycontract/
cd go && go test ./internal/query/codeowners/ -count=1
cd go && go test ./internal/query/ -count=1
cd go && go test ./internal/queryplan/ -count=1
bash scripts/verify-package-docs.sh
git diff --check
gofumpt -l <touched files, repo-relative>
```
