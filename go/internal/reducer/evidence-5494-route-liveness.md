# Evidence: #5494 route-fact-based Rails controller liveness

Route-fact join extending the #5376 repo-wide Rails-controller dead-code-root
verdict builder. This note records the prove-theory-first proof for the new
route-fact SQL load, the schema-epoch assessment, the end-to-end correctness
proof against real Postgres, and the operator observability added.

## Problem

The #5376 ancestry walk (`rubycontroller.Decide`) can confirm/downgrade a
`ruby.rails_controller_action` root only on whether its class extends a Rails
controller base. A routable controller (extends `ApplicationController`) was
always kept, even when NO route in `config/routes.rb` ever reaches that
specific action -- "routable" is not "routed". #5494 joins the SAME verdict
row against repo-wide Rails route facts and downgrades an action ONLY when the
route surface inside every `Rails.application.routes.draw` block the parser
observed is exactly modeled (see "P0 fix" below for what that precisely means
and does not mean).

## False-positive risk assessed first (why this is not a naive "no route =
dead" join)

The Ruby parser's Rails route extraction is deliberately EXACT-ONLY
(`internal/parser/ruby/doc.go`): it captures a literal
`to: "controller#action"` target inside `Rails.application.routes.draw`. It
does **not** expand `resources`/`resource` DSL macros (idiomatic Rails' most
common routing style, generating 7 RESTful routes per call) into per-action
routes, and it does not resolve a namespaced target such as
`to: "admin/posts#show"` into a clean handler. Joining a downgrade against
`route_entries` alone, naively, would false-positive on the overwhelming
majority of real Rails apps.

### P0 fix (coordinator review of head 26ba26d2d): the first ambiguity
detector was itself an allow-list, and reopened the risk

The original fix (committed at 26ba26d2d) special-cased exactly two shapes as
"ambiguous": a `resources`/`resource` macro, and an explicit `to:` target that
failed to parse. A separate-context review caught that this was still an
allow-list layered on the pre-existing HTTP-verb allow-list
(`get`/`post`/`put`/`patch`/`delete`/`head`/`options`), and that several
extremely common Rails route constructs fell through BOTH allow-lists --
neither captured as an exact route NOR flagged ambiguous -- so a controller
routed ONLY through one of them would have been silently downgraded to
`route_unreachable`: a live controller called dead, the exact false positive
#5494 exists to prevent. The missed constructs: `root "welcome#index"` (the
single most universal Rails route, since `root` is not an HTTP verb method);
`match "/x", to:, via:` (`match` is not an HTTP verb method either); any
gem-provided route macro (`devise_for`, or literally any other gem's routing
DSL method -- unbounded, cannot be enumerated); `controller:`/`action:`
keyword-pair routes (no `to:` pair, so the old detector's `to:`-presence check
never fired); bare/relative paths (`get "about", to:`) and interpolated paths
(`get "/api/#{version}/x", to:`) (both rejected by the exact-path check
*before* the old ambiguity branch ran); and non-string `to:` targets (a
constant or lambda).

**Fix (fail-safe by default, not allow-list extension):**
`internal/parser/ruby/framework_routes_ambiguity.go`'s
`rubyScanRailsDrawBlockForAmbiguity` now walks the `do_block` of every
`Rails.application.routes.draw` call the parser finds and flags
`has_unmodeled_routes = true` for the file when the block contains ANY call
that does not resolve into an exact route entry (`railsExactRouteEntry`) --
the inverse of the old rule. Enumerating "known problem shapes" is unbounded
(any Rails engine or gem can add its own routing DSL method); the only safe
rule is "ambiguous unless proven exact". This is deliberately over-inclusive
(a stray `Rails.env.production?` guard call inside the block also sets the
flag), but over-inclusive ambiguity only ever biases the join toward KEEP,
never toward a wrong downgrade. `RubyRailsRouteFacts.HasUnmodeledRoutes`
(reducer) is a repo-wide OR of the per-file signal: if true, EVERY controller
action in the repo keeps regardless of `RoutedHandlers`. A downgrade requires
`HasAnyRouteEvidence=true` (routes.rb was actually observed) AND
`HasUnmodeledRoutes=false` (every call the parser saw in every routes.draw
block resolved into an exact route) AND no matching `RoutedHandlers` entry.
This mirrors the #5376 keep-biased shape: downgrade only on positive evidence
from an exactly-modeled surface, never an unexamined absence -- see
`evaluateRouteLiveness`'s doc comment in `code_root_verdicts_routes.go` for
the precise (and deliberately non-overclaiming) statement of what
`HasUnmodeledRoutes=false` does and does not prove.

P0 regression coverage (all RED against the pre-fix allow-list, GREEN after,
verified by reverting the three parser files to the pre-fix commit and
re-running): `TestParseFlagsUnmodeledRailsRouteConstructs`
(`internal/parser/ruby/framework_routes_ambiguity_test.go`, 8 subtests: `root`
shorthand, `root to:`, `match`+`via:`, `devise_for`, `controller:`/`action:`,
bare path, interpolated path, non-string `to:`),
`TestBuildCodeRootVerdictsRootOnlyRoutedControllerKept`
(`internal/reducer/code_root_verdicts_routes_test.go`), and
`TestCodeReachabilityRailsRouteFactsLoaderKeepsRootOnlyRoutedController`
(`internal/storage/postgres/code_reachability_route_liveness_live_test.go`,
real Postgres, real production loader path).

### P1 fix (re-review of head 362556b02): same defect class, narrower trigger

`isRailsRoutesDraw` matched ONLY `method=="draw"`, and gated BOTH the
ambiguity scan (`rubyScanRailsDrawBlockForAmbiguity` is only invoked when this
returns true) and exact-route context resolution
(`rubyResolveRouteContext`'s `case "call"` branch). `Rails.application.routes.append`
and `.prepend` are real, documented Rails APIs engines and gems use to insert
routes after/before the main set, with the identical
`Rails.application.routes` receiver shape but a different method name. A call
inside an `.append`/`.prepend` block resolved into NEITHER an exact
`route_entries` handler NOR `has_unmodeled_routes` -- invisible to both
mechanisms -- so an action routed ONLY inside one of them, in a repo whose
plain `.draw` block was otherwise fully exact, would have been silently
downgraded to `route_unreachable`.

Fix: widened `rubyRailsRouteSetMethods` (now shared by both consumers, moved
to `framework_routes_ambiguity.go`) to `{"draw", "append", "prepend"}`; the
receiver check is identical for all three, so nothing else changes. Also
widened `rubyDoBlockChild` to match tree-sitter-ruby's one-line curly-brace
`{ ... }` "block" node kind in addition to the multi-line `do...end`
"do_block" kind (rare in practice -- RuboCop's default style enforces
`do...end` for multi-line blocks -- but a fail-safe scan cannot assume a style
guide is followed).

P1 regression coverage (RED against the pre-P1-fix code, verified by
reverting the two parser files to the pre-P1-fix commit and re-running, GREEN
after restoring): `TestParseFlagsUnmodeledRoutesInsideAppendAndPrependBlocks`
(append- and prepend-only blocks with an unmodeled `root` route),
`TestParseCapturesExactRouteInsideAppendBlock` (an append-only block with a
fully exact route is captured normally, not forced into ambiguity),
`TestParseFlagsUnmodeledRouteInCurlyBraceDrawBlock` (all three in
`internal/parser/ruby/framework_routes_ambiguity_test.go`), and
`TestBuildCodeRootVerdictsAppendOnlyRoutedControllerKept`
(`internal/reducer/code_root_verdicts_routes_test.go`) -- at the reducer's
abstraction level an append-only-routed controller produces the identical
input shape as the P0 root-only-routed case, so the new ground this last test
actually pins is the reducer-contract half of the same regression; the
parser-level tests are what prove the append/prepend/curly-brace block itself
is now scanned at all. Live-Postgres coverage
(`TestCodeReachabilityRailsRouteFactsLoaderRoundTrip`,
`TestCodeReachabilityRailsRouteFactsLoaderKeepsRootOnlyRoutedController`) was
re-run unchanged and still passes -- the loader and reducer contract for
`RubyRailsRouteFacts` did not change in this fix, only which parser-observed
constructs feed it.

## Schema-epoch assessment

**Backfill required, not forward-only.** A repo already stamped at a
pre-#5494 epoch has its ancestry-confirmed verdicts computed WITHOUT ever
consulting route facts -- ancestry-confirmed rows do not change shape, so
nothing marks them stale. Without a bump, an already-indexed repo's unrouted
controller actions would stay silently mis-confirmed forever (the loader's
`reach_updated_at`/`completed_at` watermark check has no reason to re-fire
once a repo is caught up). `CodeReachabilityVerdictSchemaEpoch` is bumped to
**3** (`internal/storage/postgres/code_reachability.go`), reusing the
identical #5376 P1 upgrade-backfill mechanism
(`evidence-5376-code-root-verdicts.md`, "P1 upgrade-backfill (Option C)"): the
pending-inputs loader re-schedules any repo whose watermark predates the
current epoch exactly once, then stamps the new epoch in the same transaction
as the verdict/reachability replacement (anti-loop, proven by the existing
`TestCodeReachabilityUpgradeBackfillRoundTripAndAntiLoop` /
`TestCodeReachabilityUpgradeBackfillZeroVerdictAntiLoop` live tests, unchanged
by this PR since the mechanism is generic over the epoch constant).

**Rebase note (origin/main advanced past #5500 before this PR's promotion):**
this feature was originally developed bumping the epoch 1 -> 2. #5500
(lexical-scope-aware Ruby controller ancestry candidate restriction, merged as
b5f3cfdc4) landed on `main` independently in the meantime and ALSO bumped the
epoch 1 -> 2, for its own unrelated verdict-semantics change (which base refs
resolve exactly vs. stay `suffix_only_ambiguous`). Rebasing #5494 onto that
`main` collided on the same constant; resolved by keeping #5500's epoch 2 and
advancing #5494's bump to epoch **3**, layered on top -- an already-indexed
repo stamped at epoch 2 (post-#5500, pre-#5494) still needs exactly one more
re-projection to pick up route-liveness, which epoch 3 forces. Epoch 1 (#5376)
and epoch 2 (#5500) are unaffected; the mechanism re-schedules a repo exactly
once per epoch it is behind, so a repo jumping straight from epoch 0 or 1 to 3
re-projects once, not three times.

## Theory under test

1. Adding `entity_name` to the existing roots load
   (`listCodeReachabilityRootsSQL`) is a free extra column read from the
   already-fetched heap tuple -- no plan change.
2. A NEW per-repo Rails route-fact load reading `fact_records` (NOT
   `shared_projection_intents`) can reuse the existing partial index
   `fact_records_framework_routes_repo_path_idx` (already committed for
   `internal/query`'s `ListFrameworkRoutes` live-evidence endpoint) IF the
   query's WHERE clause repeats that index's exact predicate.
3. Reading from `fact_records` instead of the `handles_route`
   shared-projection-intent domain avoids a new ordering/readiness dependency:
   `handles_route` intents can complete in a LATER phase than the
   `code_calls`/`inheritance_edges` domains the reachability loader currently
   gates on (`shared_projection_readiness.go`), so gating on it risked
   mis-reading "not yet materialized" as "no route" -- a false positive.
   Reading raw parser facts directly sidesteps that hazard entirely, and any
   staleness in `fact_records` (an older generation's route fact still
   present) only ever biases the join toward KEEP, never toward a wrong
   downgrade (see `evaluateRouteLiveness` doc comment).

## Setup (representative-volume synthetic corpus)

A throwaway Postgres 18 (isolated container, `eshu-5494-pg-proof`, not the
shared golden-corpus/replay gate database) was seeded via
`go/cmd/proof5494` (throwaway, not part of the build graph) with: 500
"other" repos x 20 file facts = 10,000 unrelated `fact_records` rows (bulking
the table so the repo_id filter is exercised against a large table, same
methodology as the #5376 evidence note); one `proof5494-ruby-big` repo with
300 controllers x 8 actions = 2,400 `ruby.rails_controller_action` roots, 8,000
filler Ruby methods (10,700 `content_entities` rows for the repo), and 300
exact Rails route facts (one route per controller, targeting only that
controller's `action0` -- `action1..action7` are genuinely unrouted, the
representative dead-route shape #5494 must detect).

## EXPLAIN (ANALYZE, BUFFERS) -- query shapes

Q1 -- roots load with the added `entity_name` column
(`repo_id + dead_code_root_kinds` array-nonempty filter, `proof5494-ruby-big`,
2,400 of 10,700 rows for the repo match):

```
Sort (actual time=5.524..5.567 rows=2400 loops=1)
  ->  Seq Scan on content_entities (actual time=0.046..3.654 rows=2400 loops=1)
        Filter: (repo_id = ... AND jsonb_array_length(...) > 0 AND jsonb_typeof(...) = 'array')
        Rows Removed by Filter: 8300
        Buffers: shared hit=254
Execution Time: 5.700 ms
```

A Seq Scan is the planner's genuine choice at this table size (11,059 total
rows) -- matches the pre-#5494 plan shape exactly (same Filter, same node
type); `entity_name` is read from the already-fetched heap tuple, confirmed
free. No regression: this is byte-identical to the #5376 roots-load plan with
one added SELECT column.

Q2 -- NEW Rails route-facts load
(`listCodeReachabilityRailsRouteFactsSQL`, `proof5494-ruby-big`, 300 matching
file facts of 10,300 fact_records rows):

```
Nested Loop Left Join (actual time=0.044..0.385 rows=300 loops=1)
  Buffers: shared hit=23
  ->  Index Scan using fact_records_framework_routes_repo_path_idx on fact_records file
        Index Cond: ((payload ->> 'repo_id') = 'proof5494-ruby-big')
        Filter: (...->'rails') IS NOT NULL
        Buffers: shared hit=23
  ->  Function Scan on jsonb_array_elements entries
Execution Time: 0.423 ms
```

The committed partial index is used (23 buffer hits, 0.42 ms), confirming
theory #2.

**OLD-shape comparison** (same query, WITHOUT the redundant clauses that
repeat the index's exact predicate -- only `repo_id` + the residual
`rails IS NOT NULL` filter):

```
Nested Loop Left Join (actual time=4.664..4.879 rows=300 loops=1)
  ->  Seq Scan on fact_records file (actual time=4.640..4.713 rows=300 loops=1)
        Filter: (fact_kind = 'file' AND repo_id = ... AND rails IS NOT NULL)
        Rows Removed by Filter: 10000
        Buffers: shared hit=392
Execution Time: 4.917 ms
```

Without repeating the index predicate, Postgres falls back to a Seq Scan over
the whole `fact_records` table: **17x more buffer hits (392 vs 23), ~12x
slower (4.9ms vs 0.4ms)** at this modest 10,300-row scale. This proves the
redundant predicate-matching clauses in the shipped query
(`file.payload->'parsed_file_data'->'framework_semantics' IS NOT NULL` +
`jsonb_array_length(...frameworks...) > 0`) are load-bearing, not decorative,
confirming theory #2's "IF" clause.

## Correctness proof (real Postgres, real production path)

`internal/storage/postgres/code_reachability_route_liveness_live_test.go`
(`TestCodeReachabilityRailsRouteFactsLoaderRoundTrip`, requires
`ESHU_POSTGRES_DSN`) seeds four repos through the real
`fact_records`/`content_entities` schema and runs the ACTUAL production
loaders (`loadCodeReachabilityRoots`, `loadCodeReachabilityRubyClasses`,
`loadCodeReachabilityRailsRouteFacts`) feeding the real
`reducer.BuildCodeRootVerdicts` -- not a hand-built fixture:

- **Positive**: `PostsController.index` has an exact route -> confirmed,
  `route_evidence=routed`.
- **Negative**: `OrdersController.orphan` has zero matching route in an
  exact-only, observed route surface -> downgraded,
  `reason=route_unreachable`.
- **Ambiguous**: `WidgetsController.orphan` has zero matching route, but the
  repo also has a `resources`/`resource` macro anywhere
  (`has_unmodeled_routes=true`) -> stays confirmed,
  `route_evidence=unmodeled_routes_present`.
- **No data**: `GadgetsController.orphan` has no `routes.rb` fact observed at
  all -> stays confirmed, `route_evidence=no_route_data`.

All four pass. Reducer-only regression coverage (no DB) in
`code_root_verdicts_test.go`
(`TestBuildCodeRootVerdictsRouteLiveness`,
`TestRouteDowngradedRootRemovedFromBFSRootSet`) and the runner harness proof in
`code_reachability_projection_runner_test.go`
(`TestCodeReachabilityProjectionRunnerExcludesRouteUnreachableControllerFromBFS`)
were RED before the fix (verified by temporarily stubbing
`evaluateRouteLiveness` to always return `RouteEvidenceNoData` and observing
the expected assertions fail) and are GREEN after.

## Golden-corpus impact assessed

`tests/fixtures/ecosystems/ruby_rails_app` (the B-12 `ruby_rails_app` golden
scope, `#5378 Detector 1`) has NO `routes.rb` file at all, so
`HasAnyRouteEvidence=false` for that repo and #5494 evaluates to
`RouteEvidenceNoData` for every controller action, leaving the existing
`WidgetsController#index` (suppressed) / `LegacyReportsController#generate`
(cleanup_ready) golden assertions byte-identical. No cassette or B-12 snapshot
update is required for the fixture as it exists today.

This repo-wide `find tests/fixtures -iname routes.rb` returns nothing, so
there is also **no golden-corpus regression coverage of route liveness
end-to-end** through the real ingest -> reduce -> project -> query pipeline
(flagged P2 in the coordinator review). Filed as a tracked follow-up rather
than added in this change, since it requires a full Docker-orchestrated B-7
gate run (bootstrap + replay + drain + diff) to regenerate the snapshot:
[#5723](https://github.com/eshu-hq/eshu/issues/5723), linked under
"Spun-off follow-ups" in epic #5588.

## Performance Evidence

Q1 and Q2 above, on representative synthetic volume (10,700 `content_entities`
rows / 10,300 `fact_records` rows for the target repo, 500 unrelated repos in
the same tables). Q2 is index-backed (0.42 ms, 23 buffer hits) and gated
behind the existing `codeReachabilityRootsHaveRailsController` check (only
Ruby repos with at least one controller-action root pay for it, same gate
#5376 already uses for the Ruby class-registry load).

No-Regression Evidence: Q1's plan and Filter are byte-identical to the
pre-#5494 roots load (one added SELECT column, read from the already-fetched
heap tuple). The reachability BFS traversal, edge loader, and write path are
untouched by this change; only the verdict-builder input and the
`ruby.rails_controller_action` root-grant decision are extended.

## Observability Evidence

`CodeRootVerdictStats` gains `RouteDowngraded`, `RouteConfirmed`,
`RouteAmbiguousKept`, `RouteNoData` (`internal/reducer/code_root_verdicts.go`).
The runner (`code_reachability_projection_runner.go`) surfaces
`VerdictsRouteDowngraded` on `CodeReachabilityProjectionResult` and logs
`verdicts_route_downgraded` on every `"code reachability projection completed"`
cycle log, plus `route_downgraded` on the existing
`"code root controller verdicts downgraded"` per-partition log line. An
operator can see, per cycle, how many controller actions were newly flagged
dead by route evidence, how many were confirmed by a positive route match, how
many were kept by the dynamic-route ambiguity floor, and how many repos have no
route data at all -- the same shape as the existing #5376
`verdicts_suffix_ambiguous_kept` / `verdicts_inconclusive_missing_context`
counters this PR sits alongside.
