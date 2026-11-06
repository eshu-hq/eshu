# #10104: Producer-Union Cost for the Anchored Code-Call Legs

`LoadActiveCodeCallSymbolDefinitionFacts` anchors package and Go keys on
producer manifests read from the active generation. When a delta rewrites
the manifest, discovery empties and the anchored legs (with their base
scans) vanished, so base definitions the overlay rules would keep were
silently lost. The fix appends a base-only union leg per anchored group
with empty discovery: `$5` binds the baselined delta-active targets' scope
ids, `$6` the candidate base generation ids (split domains, #9611), gated
on `runBase`. No active scan runs on a union leg; the match on each
definition's own package id gates the wider base scopes.

## Performance Evidence:

Source: branch `drive/10104-producer-union` (commits `c576accce4`
regression + `be63034f7b` forward-port, rebased onto `60f2fe256d44`
with byte-identical code). Disposable PostgreSQL 18.6
(`postgres:18-alpine` container, default settings) on a shared developer
host, schema from `ApplyBootstrap`, ANALYZEd, best of 5 runs per statement,
literal parameters, page 500. Fixture: two scopes (one package-anchored,
one Go-anchored), each with a full generation holding 2,500 file facts plus
one producer manifest and one anchored definition, plus a delta generation
that renames the manifest and becomes active.

RED pre-fix: the firing load returns `[]`, want both base definitions.
GREEN post-fix: both base definitions load with tally
`active=0 targets=1 base=2 resolve=2 inputs=1 other=2` — the firing case
adds only the two union base pages, no new discovery or active statement.
Manifest-stable anchored reads keep `2/1/2/2/1/2`; anchored no-delta reads
keep `2/1/0/0/0/2` plus 2 discovery statements, so the steady state gains
no base, resolve, or inputs statement.

EXPLAIN (ANALYZE, BUFFERS) on the derived anchored base text with the
production bindings: union base pages 14.65 ms (package) and 14.72 ms (Go)
vs the all-full anchored read at 27.60 ms with the base generation active,
counterfactual `(pkg+go)/full` 1.06x under the #10003 1.5x bar. Neither
union plan seq-scans `fact_records`. Measured on a 2-scope lane fixture:
union width scales with the number of baselined delta-active scopes (logged
per load as `package_union_scope_count` / `go_union_scope_count`), so the
1.5x bar re-proves per corpus shape and is not claimed beyond the measured
fixture.

Why safe (bounded): the union leg runs only when `runBase` holds and
discovery is empty — the shape that returns nothing today. Rows still match
on each definition's own package id, and a key resolving to definitions in
several scopes stays unresolved downstream (degraded resolution, not corrupt
truth). Narrowing (#10204): on definition-content collision across scopes,
the package-group union page drops rows no producer anchor could have
published before the overlay — files under a `node_modules` segment (the
manifest read excludes them, the stamp keeps the nearest name), unless the
row carries a queried composer package id (composer has no `node_modules`
exclusion, so those rows are legitimate). A scope-side lookup is
unindexable: `content_files` is last-writer-wins on `(repo_id,
relative_path)`, so the delta's manifest rewrite already discarded the base
content, and facts carry no manifest bodies — the narrowing therefore drops
rows per group instead of scopes. Go union legs narrow nothing (a nested
`go.mod` would make the scope a producer; without one the outer module
stamps an unmatchable path). The base text is derived from the active text
by `deriveDeltaOverlayBaseQuery`, keeping its filters, keyset cursor, and
ordering, and the overlay merge still drops refused or unmatched bases.

No-Regression Evidence (#10204): zero query-constant changes, so the
#10104 EXPLAIN plans still describe the statements byte for byte; the
pinned tallies re-prove the statement shape live
(`ManifestChangingDeltaLive` still `active=0 targets=1 base=2 resolve=2
inputs=1 other=2`, `ManifestStableAnchoredLive` still `2/1/2/2/1/2`,
`AnchoredNoDeltaStatementsLive` still `2/1/0/0/0/2` plus discovery).
`TestLoadActiveCodeCallSymbolDefinitionFactsUnionExcludesVendoredNonProducerLive`
is the failing-first repro on disposable PostgreSQL 18.6: pre-narrowing
the 3-scope firing load returns all three bases, post-narrowing only the
legitimate npm and composer bases, with `package_union_scope_count=3`
proving the scan still covers every scope and `union_base_found: 2`
proving the vendored scope contributes nothing.

No-Observability-Change: no new instrument; the firing load still moves
the existing delta-union counter and logs the same union scope counts on
the existing load line.

## Observability Evidence:

No new instrument. The firing load moves the existing
`eshu_dp_code_call_symbol_definition_read_delta_union_total` counter
(`union_base_found: 2`, asserted live) and logs
`package_union_scope_count=2` / `go_union_scope_count=2` next to the zero
producer counts on the existing `code call symbol definition load` line
(asserted live). The telemetry-coverage verifier agrees with the base.
