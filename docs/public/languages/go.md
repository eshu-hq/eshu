# Go Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `go`
- Family: `language`
- Parser: `DefaultEngine (go)`
- Entrypoint: `go/internal/parser/go_language.go`
- Fixture repo: `tests/fixtures/ecosystems/go_comprehensive/`
- Unit test suite: `go/internal/parser/engine_test.go`
- Integration validation: compose-backed fixture verification (see `../reference/local-testing.md`)

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Functions | `functions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathGo` | Compose-backed fixture verification | - |
| Structs | `structs` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathGo` | Compose-backed fixture verification | - |
| Interfaces | `interfaces` | supported | `interfaces` | `name, line_number` | `node:Interface` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathGo` | Compose-backed fixture verification | - |
| Imports | `imports` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathGo` | Compose-backed fixture verification | - |
| Function calls | `function-calls` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathGo` | Compose-backed fixture verification | - |
| Variables | `variables` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathGo` | Compose-backed fixture verification | - |
| Methods (receivers) | `methods-receivers` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathGo` | Compose-backed fixture verification | - |
| Generics | `generics` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_python_semantics_test.go::TestDefaultEngineParsePathGoRichSemanticMetadata` | Compose-backed fixture verification | - |
| Embedded SQL queries | `embedded-sql-queries` | supported | `embedded_sql_queries` | `function_name, function_line_number, table_name, operation, line_number, api` | `relationship:SQL link hints consumed by sql_links materialization` | `go/internal/parser/go_embedded_sql_test.go::TestDefaultEngineParsePathGoEmbeddedSQLQueries` | Compose-backed fixture verification | - |
| net/http route truth | `net-http-route-truth` | supported | `framework_semantics.net_http.route_entries` | `method, path, handler` for exact literal patterns and identifier handlers | `HANDLES_ROUTE` when reducer can resolve the exact handler | `go/internal/parser/go_dead_code_registrations_test.go::TestDefaultEngineParsePathGoEmitsDeadCodeRegistrationRoots`, `go/internal/reducer/handles_route_intents_test.go` | Shared reducer route projection proof | Exact standard-library registrations emit route entries; ambiguous wrappers, unknown mux receivers, and nonliteral patterns do not fabricate handlers. |
| Third-party router route truth | `third-party-router-route-truth` | supported | `framework_semantics.{gin,echo,chi,fiber}.route_entries` | `method, path, handler` for constructor-proven routers, literal route paths, literal group prefixes, and identifier handlers | `HANDLES_ROUTE` when reducer can resolve the exact handler | `go/internal/parser/go_dead_code_registrations_test.go::TestDefaultEngineParsePathGoEmitsThirdPartyRouteEntries`, `go/internal/reducer/handles_route_intents_test.go::TestBuildHandlesRouteIntentRowsEmitsGoFrameworkRouteMatches`, `go/internal/query/content_reader_framework_routes_test.go::TestParseFrameworkSemanticsExtractsGoFrameworkRoutes` | Parser-to-reducer-to-query route-entry proof | Gin, Echo, Chi, and Fiber exact route registrations emit route entries only when the router receiver is proven by a known constructor or literal group and the handler is an identifier. Dynamic paths, unknown receivers, method values, adapter wrappers, middleware chains, closures, generated routers, and runtime registrations remain unclaimed. |
| Outbound contracts | `outbound-contracts` | partial | - | - | - | Support-maturity guardrails | Explicit unsupported-contract wording on this page | HTTP/gRPC/topic client calls do not create deterministic cross-repo outbound contract edges today. |

## Framework And Library Support

Supported today:

- Standard-library HTTP registrations and signatures are modeled as derived
  roots.
- Exact `net/http` route registrations emit `framework_semantics.net_http`
  route entries for literal patterns and identifier handlers. Go 1.22
  `METHOD /path` patterns preserve the method; legacy patterns use `ANY`.
  `HANDLES_ROUTE` is projected only when the reducer resolves the exact handler.
- Exact Gin, Echo, Chi, and Fiber route registrations emit
  `framework_semantics.{gin,echo,chi,fiber}.route_entries` when a router variable
  is proven by `gin.New`/`gin.Default`, `echo.New`, `chi.NewRouter`, or
  `fiber.New`; literal `Group` prefixes are joined into the emitted path for
  Gin, Echo, and Fiber. Handlers must be identifier symbols.
- Cobra command registrations and signatures are modeled as derived roots.
- controller-runtime `Reconcile`, exported package API outside `cmd`,
  `internal`, and `vendor`, interface implementations, function values,
  generic constraints, type references, and dependency-injection callbacks are
  modeled as derived roots.

Not claimed today:

- Generated routers, middleware chains, dynamic or nonliteral route patterns,
  unknown receivers, method values, adapter functions, closures, webhook and
  worker registrations, reflection, build tags, plugin behavior, and broad
  public API surfaces remain exactness blockers.
- HTTP, gRPC, topic, and generated-client outbound contract extraction is not
  emitted as deterministic cross-repo contract truth today.

## Parser Performance

`Parse` (`go/internal/parser/golang/language.go`) used to build several of its
per-file indexes with independent full-tree walks over the same syntax tree.
Epic #4831 tracks collapsing the Go parser's full-tree walk count; #4839
consolidated three of those redundant walks without changing any observable
output:

- **Constructor-return dedup**: `constructorReturns` was computed once in
  `Parse` and then recomputed from scratch inside
  `goCollectSemanticDeadCodeRoots` (used only by dead-code evidence
  collection). The already-computed map is now threaded through
  `goDeadCodeEvidence` into `goCollectSemanticDeadCodeRoots`, removing the
  second full-tree walk entirely.
- **Merged file-level index walk**: `goImportAliasIndex`, the former
  `goConstructorReturnTypes`, and the former `goLocalNameBindings` each walked
  the whole file separately to build three independent, non-overlapping
  indexes. `goCollectFileLevelIndexes`
  (`go/internal/parser/golang/file_level_indexes.go`) now builds all three in
  one `shared.WalkNamed` pass with a per-node-kind dispatch, preserving each
  original visitor's exact logic and append order.
- **Merged receiver-binding sub-walks**: `goLocalReceiverBindings`'s two
  pre-passes, the former `goLocalMapValueTypes` and `goLocalInterfaceNames`,
  walked the file separately before its own main walk. They are now built
  together by `goCollectLocalMapValueTypesAndInterfaceNames`
  (`go/internal/parser/golang/map_receiver_types.go`) in one pass, since the
  two node-kind sets are disjoint; `goLocalReceiverBindings`'s own main walk is
  unchanged.

`goImportAliasIndex` remains a standalone full-tree-walk function because
package pre-scan passes (`package_prescan_evidence.go`,
`package_interface_prescan.go`) call it on other files' roots, not `Parse`'s
own root; only the three walks that were redundant against `Parse`'s single
root were merged.

Accuracy proof: a differential-equivalence harness
(`go/internal/parser/golang/equivalence_dump_test.go`, guarded by
`GO_PARSE_DUMP`) parses every `*.go` file under `go/internal/parser` with four
`shared.Options` variants (default, `IndexSource`, `EmitDataflow`,
module-scope variables) and hashes each payload. Comparing the pre- and
post-merge dumps showed the parser's output was byte-for-byte identical
(symmetric set difference `0/0`) for every corpus file whose own source was
not part of this change; the only differing entries were the six files this
change edited (their own line numbers and declarations changed, as expected)
plus one new file. See epic #4831 and issue #4839.

Performance Evidence: this change removes 3 of the Go parser's per-file
full-tree `shared.WalkNamed` passes (constructor-return dedup + a merged
3-way file-level index walk + a merged receiver-binding sub-walk). The win is
structural (fewer identical traversals of the same tree); the redundancy is
provable by inspection.

No-Regression Evidence: parser output is byte-identical old-vs-new (the `0/0`
differential above), so no accuracy regression is possible; the change only
removes duplicate traversal work.

No-Observability-Change: this is a pure structural walk consolidation with no
runtime-behavior change and no new metric/span/log surface. Per-file Go parse
timing is already covered by the existing parser/pre-scan parse-stage
telemetry; no new operator signals are warranted.

- **Dead-code resolution re-walk elimination (#4920):** `goCollectSemanticDeadCodeRoots`
  (`go/internal/parser/golang/dead_code_semantic_roots.go`) was running two
  independent full-tree `walkNamed` traversals for each file — one to resolve
  every `var_spec`, `short_var_declaration`, `assignment_statement`,
  `composite_literal`, `parameter_declaration`, `field_declaration`,
  `function_declaration`, `return_statement`, and `call_expression` against
  walk-1's declaration maps, and a third via
  `goMarkGenericConstraintInterfaceRoots` to resolve
  `type_parameter_declaration` nodes. These walks were redundant because walk-1
  (declaration collection) already visits every named node in the tree.
  Resolution-candidate node pointers are now gathered (cloned) during walk-1 in
  typed slices, and the resolution logic runs as plain in-memory `for` loops
  over the gathered pointers — zero additional tree traversals.
  `goMarkGenericConstraintInterfaceRoots` now takes a `[]*tree_sitter.Node`
  instead of re-walking from `root`. This eliminates exactly two
  `shared.WalkNamed` full-tree traversals per file.

Performance Evidence: `BenchmarkParse/go` (10,035-LOC Go file, 5-count benchstat comparison):
  - **BEFORE** (merge-base cd83c3d75): ~696ms/op, ~602 MB/op, ~4,831,786 allocs/op
  - **AFTER** (gather-then-resolve): ~683ms/op, ~598 MB/op, ~4,695,185 allocs/op
  - Delta: **-13ms (-1.9%)**, **-136,601 allocs/op (-2.8%)**
  (`go test ./internal/parser -bench 'BenchmarkParse/go' -benchmem -count=5`)

Accuracy proof: the same differential-equivalence harness produces byte-for-byte
identical output (symmetric set difference `0/0`) across multiple corpus sizes
(9 files × 4 variants = 36 entries narrow; 28 files × 4 variants = 112 entries
broad, both `diff`/`comm -3` clean). This harness runs under `GO_PARSE_DUMP`
and is a manual differential, not a CI gate. The walk-count test
(`walk_count_test.go`, using `shared.SetWalkNamedHookForTest`) asserts the
per-file `shared.WalkNamed` count dropped from 60 to 58 (exactly the 2 removed
dead-code resolution re-walks). See epic #4917 and issue #4920.

No-Observability-Change: this is a pure structural walk consolidation with no
runtime-behavior change and no new metric/span/log surface. Per-file Go parse
timing is already covered by the existing parser/pre-scan parse-stage
telemetry; no new operator signals are warranted.

- **Framework-semantics import gate (#5219):** `goHTTPFrameworkSemantics`
  (`go/internal/parser/golang/framework_routes.go`) builds a parent-lookup and
  walks the whole tree for every Go file, but it can only emit
  `framework_semantics` route entries when the file imports `net/http` or one
  of the `goRouteFrameworkConstructors` routers (gin, echo, chi, fiber): its
  net/http path is gated on a net/http alias (`goHTTPRegistrationBaseKnown`)
  and every third-party receiver is gated on a constructor import
  (`goRouteFrameworkConstructor`). `Parse` now skips the call for a file that
  imports none of them via `goFileImportsRouteFramework`
  (`go/internal/parser/golang/framework_semantics_gate.go`), where the pass
  provably returns `(nil, false)`. The route-truth capabilities in the table
  above (`net-http-route-truth`, `third-party-router-route-truth`) are
  unchanged; only files that could never register a route stop paying for the
  walk.

Performance Evidence: profiling `Engine.ParsePath` over the kubernetes corpus
(17,490 `.go` files) attributed 9.1% of parse-stage CPU to
`goHTTPFrameworkSemantics`, and 94.7% of those files import none of the gated
paths. A focused benchmark on a dense framework-free file measured the
eliminated walk at 4.25 ms/op against 48.5 ms/op for the whole parse (8.1% of
that file's parse), so the corpus-level saving is ~8% × 94.7% ≈ 7.6% of parse
with no output change.

Accuracy proof: the `framework_semantics` payload is byte-identical old-vs-new
(the skipped pass already returned `(nil, false)` for these files). Pinned by
`TestGoHTTPFrameworkSemanticsGateSkipsFilesWithoutFrameworkImports` /
`...RunsForNetHTTPImport` and `TestGoFileImportsRouteFrameworkCoversEveryGatedImport`
in `framework_semantics_gate_test.go`, plus the walk-count assertion in
`walk_count_test.go` dropping from 58 to 55 (the 3 removed framework-pass
walks). See issue #5219.

No-Observability-Change: a pure gate on already-parsed import data; no new
metric, span, log, or runtime-behavior surface. Per-file Go parse timing stays
covered by the existing parse-stage telemetry.

## Known Limitations
- Generic type constraints may not be fully captured
- Channel types not separately tracked
