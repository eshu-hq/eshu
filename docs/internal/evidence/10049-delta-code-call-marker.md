# #10049: Code-Call Marker on Delta Generations

A git delta that moves a function gives it a new content-entity id (the id
hashes the line number), and the git fact builder returned before the
`code_call_materialization` follow-up on delta generations, so nothing
rewrote the moved caller's CALLS edges until the next full generation. The
marker now fires on delta generations (streamed before the delta early
return in `fact_builder.go`), with the delta pre-count bumped by one. Two
prerequisites make it safe and are already on main: the symbol-definition
read overlays the delta on the active generation (#10003 row 1, #10124), so
callees in unchanged files still resolve under the file-scoped refresh; and
an unusable delta scope fails the work item closed (#10023, #10114) instead
of falling back to the whole-repository retract. The overlay only runs for
requested keys, and the tree-sitter Go parser emits same-package calls as
keyless name-only edges, so the delta requests nothing and cross-file
callees stay unresolved while the refresh retracts their old edges (live
B-7: only the same-file edge survived). The handler therefore derives the
definitions' own `scip-go gomod` keys for provably same-package calls on
delta generations only (`SamePackageGoCalleeKeysForDeltaRepos`: bare
unqualified Go name in a file whose imports carry no alias — the parser
records blank and dot imports with their `_`/`.` alias since #10301, so a
file with an aliased import still derives nothing); full generations keep
the keyless path byte-identical.

The handler's three symbol-to-runtime domains (`handles_route`, `runs_in`,
`invokes_cloud_action`) retract repo-wide and have no file-scoped retract.
Pairing their repo-wide refresh with a delta's changed-file rows would
delete every unchanged file's runtime edges, so the handler gates them off
delta input entirely (`runtimeDomainEnvelopes` in
`materialization/refresh.go`): a delta repository emits no per-edge rows
and no refresh for those domains, exactly as before the marker fired on
deltas, and its runtime edges refresh at the next full generation. The
`code_calls` side of the same pass owns a file-scoped retract and does run.

Proof, all hermetic in this change:

- `TestBuildStreamingGenerationDeltaEmitsWholeRepoResolveFollowups` (RED
  first): a delta generation emits exactly one `code_call_materialization`
  follow-up.
- `TestCodeCallMaterializationHandlerDeltaMovedCallerKeepsUnchangedCalleeEdge`:
  a caller in the changed file keeps its CALLS edge to a callee defined
  only in an unchanged file (supplied through the active symbol-definition
  load), under a file-scoped refresh over the changed file only.
- `TestCodeCallMaterializationHandlerDeltaGatesRuntimeDomains` (RED first):
  a delta emits zero rows in all three runtime domains; the full control
  emits per-edge rows plus one refresh per domain.
- `TestCodeCallMaterializationHandlerDeltaUnkeyedSamePackageCalls` (RED
  first, live-shape follow-up): real parser-shaped keyless Go edges on a
  delta resolve all four CALLS rows once the handler derives the callees'
  own keys for the overlay; before, only the same-file row survived with
  three unresolved callees — the live B-7 signature.
- `TestSamePackageGoCalleeKeysForDeltaRepos`: the key-derivation edge cases
  (keyed/qualified/foreign/malformed names, dot/blank/aliased imports,
  missing package identity, non-delta repos, empty delta set).
- `TestCodeCallMaterializationHandlerDeltaImportBearingSamePackageCalls`
  (RED first, #10301): the moved caller lives in a file WITH a plain import
  (no dot-import), so its keyless same-package call resolves against the
  unchanged file's definition; the companion dot-imported file's call derives
  no key and stays unresolved — the fail-safe side.
- `TestCodeCallProjectionRunnerFileAndWholeRefreshInterleaveDrains`: a
  file-scoped and a whole-scope refresh pending for one repository in one
  acceptance unit — the fenced shape cannot retract, write, or mark on an
  adversarial first partition attempt, the leading shape always progresses,
  and both creation orders drain with each row marked once and the edge
  written once. Conflict domain: `code_calls` intent partitions per
  repository, ordered by the existing refresh fence and partition leases;
  no new lock, queue, or serialization was added.
- B-12: the dr-5 `expected_failure` marker is removed (the moved-caller
  edge is now a plain required edge) and `code_call_symbols` leaves
  `unexpressed_readers`, because dr-1 to dr-3 and dr-5 express it.

## No-Regression Evidence:

Full generations keep the #10049 follow-up stream order and per-generation
fact kinds and counts; the #10301 parser change adds one `alias` string to
blank/dot import facts, so payload bytes move for files with those imports.
`TestGenerationContentVersionPinsCollectorOutput` is re-pinned in this change
with an epoch advance (epoch `9a6c02597e8ae5ea`), pinned by the new
`src/import_bindings.go` fixture carrying a blank import, a dot import, and
a bare call. The advance fixes same-commit recollection: a re-sync at an
unchanged commit under the new epoch stores one new generation instead of
being dropped as a duplicate.

Pre-upgrade window: generations stored under older epochs still carry
alias-free import facts. Delta derivation reading those facts sees blank/dot
imports as alias-free and may derive same-package keys for dot-imported bare
names, until the repository stores a generation under the new epoch. The new
work is bounded per delta: one additional work item whose handler loads
the delta generation's changed-file facts and the referenced symbol keys
(the #10124 overlay measured that read at a 1.25x counterfactual against
the all-full read, with the base scan only for reached delta scopes). The
runtime-envelope filter is one early-out pass over the already-loaded
envelopes (no pass at all when no repository is on a delta). A delta with
an unusable scope dead-letters on attempt 1 before the symbol load, so a
bad delta costs no symbol query and no graph write. End-to-end delta
wall time is NOT_CHECKED here: the live delta-reader leg runs at promotion
(the shared host was under another lane's live proof during this change).
No-Regression Evidence (#10301, measured): the derivation micro-benchmark
is committed as `BenchmarkSamePackageGoCalleeKeysForDeltaRepos` with its
pre-#10301 baseline `BenchmarkPreAliasGateSamePackageGoCalleeKeys` in
`go/internal/reducer/code/call/shared/symbol_index_bench_test.go` (the
baseline is the verbatim base-`a218b94c42e` gate, measurement only, so the
figures are committed and re-runnable, not uncommitted). Backend go1.26.10
linux/arm64, GOMAXPROCS 16 (ARM implementer 0x41 part 0xd84). Input shape:
one delta repo, 13 files, 42 keyless Go edges — 4 import-free files and 4
plain-import files with 4 calls each over 2 distinct callee names per group,
plus dot/blank/named-import skips, one missing-identity file, and one
non-delta file with 2 calls each. Seventeen rounds at `-benchtime=10000x`:
baseline 4339–4431 ns/op (1473 B/op, 34 allocs/op, 2 keys/op), after
7695–7944 ns/op (2754 B/op, 51 allocs/op, 4 keys/op). The +~3.4 µs/op is the
intended recall gain: 4 more files (16 more edges) derive keys under the
relaxed gate, so Gamma/Delta now resolve where the baseline skipped them.
Cost scales with the file's own import list (one short-circuiting alias
check per entry) plus one key per distinct callee through the existing
sort/Compact dedupe. #10301 import-aware derivation adds no new envelope
scan — it reuses the already-loaded `parsed_file_data` maps. Terminal rows
are pinned by `TestCodeCallMaterializationHandlerDeltaImportBearingSamePackageCalls`:
exactly one CALLS edge (`entity:plain-caller->entity:ledger-total`) and
exactly one requested overlay key; the dot-imported file's call stays
unresolved by design. Full generations are untouched: non-delta repositories
derive no keys (unit test asserts empty). Observability Evidence (#10301):
no new instrument. The delta path exercises the existing signals named in
the Observability section below: the handler completion log reports
`symbol_key_count` and `code_call_unresolved_callee_count` (the
unresolved-callee handler test pins `code_call_unresolved_callee_count` at
1.0), and `eshu_dp_code_call_symbol_definition_read_delta_union_total`
counts the overlay read the derived keys trigger. Parser side: no new tree
walk; only blank/dot imports gain one `alias` string each. No new lock,
queue, or serialization was added.

## Observability Evidence:

No new instrument. The delta path now exercises the existing signals: the
handler completion log (`code call materialization completed` with
`file_scoped_repo_count` and the full-refresh fallback counts), the
`eshu_dp_code_call_delta_scope_unusable_total{reason}` counter and its
ERROR log for fenced deltas, the overlay outcome counter
`eshu_dp_code_call_symbol_definition_read_delta_union_total`, and the
standard reducer execution and code-call edge batch telemetry. Docs that
called those signals dormant "until #10049" now describe them as live.

#10301 adds no new instrument; it is visible through the existing delta
signals. The handler completion log reports a nonzero `symbol_key_count`
for import-bearing deltas (0 before the fix, with
`code_call_unresolved_callee_count` at the dropped-edge count), and
`eshu_dp_code_call_symbol_definition_read_delta_union_total` counts the
overlay read the derived keys trigger.
