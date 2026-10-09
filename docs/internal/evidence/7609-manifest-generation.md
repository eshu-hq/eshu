# Manifest Read Bound to the Active Generation (#7609)

`PackageManifestsQuery`
(`go/internal/storage/postgres/code/producers/store.go`, the #7623 leaf that
owns the producer manifest reads) scans every stored `package.json` manifest
in `content_files`, which has no generation column and is written before the
Ack that activates the generation. A stored manifest can be ahead of the
active generation and, in one corner, drop a producer candidate (a miss) or
let a second same-named producer resolve alone and bypass the ambiguity
rule. `scopeIDsWhere` scans content as `sql.NullString`: the UNION ALL dirty
leg returns NULL rows that always join the producer set.

The fix makes the producer set the manifest match UNION ALL the dirty scopes:
a scope with a never-activated generation, an unstamped activation, or a
manifest newer than its activation. Extra scopes only add candidates the
anchored scan still gates on each definition's own `package_id`, so the union
keeps the ambiguity rule exact while a dropped scope would bypass it.

Both dirty legs are joins, not correlated EXISTS: the timestamp leg reads a
per-repository `MAX(indexed_at)` aggregate (`MAX >= t` is exactly
`EXISTS >= t`) and the generation leg is an IN semi-join the planner hashes
once. UNION ALL replaces UNION: the consumer sorts and compacts scope ids in
Go, and NULL dirty rows cannot equal non-NULL manifest rows (`content` is
NOT NULL). The `manifest_max` join must stay a LEFT JOIN so manifest-less
scopes still evaluate the generation leg.

The dirty predicate is status-agnostic on purpose (arbiter ruling): Ack
supersedes a refused generation at the next activation, and a delta that does
not touch `package.json` leaves the refused content stored, so
"pending/failed" or "newer than active" both miss the
supersede-then-delta hole. Do not compare `superseded_at` to `activated_at`:
Ack stamps the prior active with the same clock, which would dirty every
scope with two activations.

Residuals (also in the query comment): a manifest deleted by a write with no
generation row leaves no `indexed_at` trace and is uncatchable (second-order:
crash-window write plus ambiguity-relevant load). Retention prunes
superseded never-activated generation rows regardless of `activated_at`
(`generation_retention_sql.go`), so once the signal row ages out, a post-hole
scope (stale manifest; deltas never rewrite the untouched path) goes clean
on both legs and returns to RED. The window opens only past the retention
horizon with an ambiguity-relevant load inside it. The permanent fix is the
generation tag on `content_files` (#7760).

Performance Evidence: EXISTS shape vs join shape on a 3,000-scope fixture
(3,030 manifests, 1% planted never-activated generations, 1% planted ahead
manifests), local PostgreSQL 18, `EXPLAIN (ANALYZE, BUFFERS)`, warm
shared-hit cache. Baseline (correlated EXISTS legs + UNION): 445 ms,
12,298 buffers; the timestamp EXISTS re-scans the materialized manifest once
per scope (SubPlan loops=2970 x 3030 rows). After (per-repo MAX LEFT JOIN +
hashed IN semi-join + UNION ALL): ~10 ms, 367 buffers, every plan node at
loops=1. Scope multiset identical to the EXISTS shape (3090=3090 rows); the
10% never-activated-prevalence probe is flat at 9.7 ms, confirming the
hashed build scales sublinearly in prevalence. Real ops-qa never-activated
prevalence was not measured (no production access); the 1% is a planted
fixture value. The change is safe because the rewrite is scope-preserving
(MAX >= t equals EXISTS >= t; the IN list includes the active row itself
only when the unchanged first disjunct already fires), proven by the
identical multisets plus the RED regression and hole tests. The #7623 leaf
move relocated the statement text unchanged: post-rebase `EXPLAIN (ANALYZE,
BUFFERS)` on seeded rows still shows the Append over the manifest-join leg
and the dirty leg with the materialized manifest CTE (Storage: Memory) and
the hashed IN SubPlan, so the benchmark numbers still describe this shape.

No-Observability-Change: this PR adds no metric, span, or log field and
removes none; operator visibility into the loader is unchanged. The only
runtime delta is the manifest statement's dirty legs.
