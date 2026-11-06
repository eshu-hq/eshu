# #10182: Dropped-Publisher Widening for the Anchored Code-Call Legs

`LoadActiveCodeCallSymbolDefinitionFacts` anchors package and Go keys on
producer manifests read from the active generation. The #10104 union legs
cover variant (a), empty discovery. Variant (b) remains: discovery comes back
changed-but-nonempty — a delta drops publisher A while publisher B still
anchors the group — so the anchored base scan binds the active-only set at
$5 and A's keepable base definitions go unread, silently. The fix widens the
anchored base $5 to the union of the leg's active producers and the
baselined delta-active targets (B1, decided against a standing-union extra
leg on measured tallies + EXPLAIN below). No active scan changes; the match
on each definition's own package id gates the wider base scopes the same way
it gates union legs.

## Performance Evidence:

Source: branch `drive/10182-variant-b`. Disposable PostgreSQL 18.6 on a
shared developer host, schema from `ApplyBootstrap`, ANALYZEd, best of 5
runs per statement, literal parameters, page 500. Fixture mirrors the #10104
note at the same scale: two scopes, each with a full generation holding
2,500 file facts plus one producer manifest and one anchored definition;
scope A's delta renames the manifest while scope B's delta touches an
unrelated file, and both deltas become active.

RED pre-fix: the firing load returns `[call-vb-b-base]`, want both base
definitions. GREEN post-fix: both base definitions load with tally
`active=1 targets=1 base=1 resolve=2 inputs=1 other=1` — the widened base
pass adds no statement — and `union_base_found: 2`.

EXPLAIN (ANALYZE, BUFFERS) on the derived anchored base text with the
production bindings: standing base ($5 `{B}`) 8.84 ms vs B1 base ($5
`{A,B}`) 17.84 ms, both index-driven on
`fact_records_scope_generation_keyset_idx`, neither seq-scanning
`fact_records`. The widening price is row-proportional (the rescued scope's
rows). The #10003 counterfactual — the all-full anchored read with the base
generations active at 14.07 ms — gives B1/all-full 1.27x, under the 1.5x
bar. The standing-union alternative totals the standing page plus an equal
union page (~26.7 ms, ~1.9x over the counterfactual) and adds one base
statement per non-empty anchored group on every delta read, regressing the
manifest-stable `base=2` pin; the minus-producers union variant reads the
same rows as B1 in two statements instead of one. Base width scales with the
number of baselined delta-active scopes — logged per load as
`package_base_scope_count` / `go_base_scope_count` — so the 1.5x bar
re-proves per corpus shape and is not claimed beyond the measured fixture.

## Observability Evidence:

No new instrument. The firing load moves the existing
`eshu_dp_code_call_symbol_definition_read_delta_union_total` counter
(`union_base_found: 2`, asserted live) and logs `package_producer_scope_count=1`
next to `package_union_scope_count=0` and the new `package_base_scope_count=2`
on the existing `code call symbol definition load` line (asserted live). A
base width above the producer width is the widened anchored scan (#10182);
a non-zero union width is a base-only union scan (#10104).
