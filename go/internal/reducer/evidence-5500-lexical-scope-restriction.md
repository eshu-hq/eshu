# Evidence: #5500 lexical-scope-aware candidate restriction

Optional precision upgrade on top of the #5376 P0 rev-2 repo-wide Rails
controller ancestry walk (`go/internal/rubycontroller`). This note records the
prove-the-theory-first proof, the measured before/after performance, and the
schema-epoch/backfill assessment.

## Theory under test

`onwardHop` restricts base-ref candidate resolution to the lexical-prefix names
real Ruby constant lookup would try — `P::R`, then each enclosing prefix of the
walked class's own namespace `P`, then top-level `::R` — before falling back to
the pre-#5376 broad, unscoped `ExactMatches(ref)`/`SuffixMatches(ref)` search.
The theory: this can only ADD a more specific exact identity (the final
candidate tried is always the bare `ref`, i.e. the pre-#5500 lookup), so it can
never drop a match the prior lookup found, while letting a previously
`suffix_only_ambiguous` ref that has a real, lexically-scoped in-corpus
referent resolve EXACTLY and become downgrade-eligible.

## Correctness proof (TDD, red-then-green)

- `go/internal/rubycontroller/controller_test.go` —
  `TestDecide/lexical-scope_match_resolves_exactly_and_downgrades` (the fixed
  false-keep: a namespaced `Base` ref previously stayed
  `suffix_only_ambiguous`/KEEP forever even though its true lexical referent
  definitively reaches the reject-set; now resolves exactly and DOWNGRADES) and
  `TestDecide/lexical-scope_match_resolves_through_an_outer_enclosing_scope`
  (the preserved-correctness case: a referent declared one lexical level OUT
  from the walked class's own namespace must still resolve, mirroring Ruby's
  `Module.nesting` innermost-to-outermost search order).
  `TestDecideLexicalScopeOuterMatchUsesExactPath` pins the Terminal value to
  prove the NEW exact-resolution path is actually engaged for the
  outer-scope case, not merely the pre-#5500 broad-suffix-probe fallback (which
  happens to reach the same Keep/Reason here).
  `TestDecideLexicalScopeIsNoOpForTopLevelClass` proves the restriction is a
  documented no-op for a top-level (non-namespaced) walked class — this is why
  every pre-existing `TestDecide` case (all of which use top-level classKeys)
  needed zero changes.
- `go/internal/reducer/code_root_verdicts_lexical_scope_test.go` —
  `TestBuildCodeRootVerdictsLexicalScopeRestriction` reproduces both cases
  through the REAL production registry (`rubyRepoWideControllerRegistry`), and
  `code_root_verdicts_integration_test.go`'s
  `TestBuildCodeRootVerdictsLexicalScopeRestrictionFromRealParserEmissions`
  reproduces the downgrade case end-to-end through the REAL Ruby parser (a
  genuine `module Admin; class OrdersController < Base; end; end` /
  `module Admin; class Base < ActiveRecord::Base; end; end` /
  `module Reporting; class Base < ActiveRecord::Base; end; end` three-file
  corpus) — no hand-built qualified names, matching the #5376 anti-masking
  harness precedent.
- Red-then-green, checked live by temporarily reverting
  `go/internal/rubycontroller/controller.go` to `HEAD` and rerunning both new
  reducer-level tests: both FAIL on the pre-#5500 commit (verdict
  `confirmed`/`suffix_only_ambiguous` instead of `downgraded`) and PASS after
  the fix.
- Full regression suites stay green with ZERO changes: `TestDecide` (13
  pre-existing cases), `TestBuildCodeRootVerdictsP0Rev2` (13 cases, the #5376
  P0 rev-2 false-positive theorem suite), `TestBuildCodeRootVerdictsFromRealParserEmissions`,
  and the entire `go/internal/parser/ruby` suite. Every pre-existing case uses a
  top-level (non-namespaced) walked classKey, so `classNamespaceOf` returns `""`
  and the restriction is provably inert for all of them — this is WHY the #5376
  false-positive theorem (A1-A4) needs no re-proof beyond rerunning its
  existing suite unchanged, which it does, green.

## Performance measurement (representative corpus)

Benchmark: `BenchmarkBuildCodeRootVerdictsNamespacedCorpus` (worst-case: 500
controllers nested 2 module levels deep, 20% with a base unresolvable anywhere
in the lexical chain — the shape the issue documents as inflating
`SuffixAmbiguousKept` on a heavily namespaced corpus) and
`BenchmarkBuildCodeRootVerdictsNamespacedCorpusTypical` (the same shape at the
evidence-5376-doc's own ~99%-correctly-based ratio), both in
`go/internal/reducer/code_root_verdicts_lexical_scope_test.go`. Compared
against the pre-#5500 commit on the identical corpus (`go test
./internal/reducer -run='^$' -bench=BenchmarkBuildCodeRootVerdictsNamespacedCorpus -benchmem -benchtime=50x -count=8`, `benchstat` old vs new):

```
BuildCodeRootVerdictsNamespacedCorpus-12   sec/op    16.92m ± 6%  ->  25.72m ± 15%   +51.99% (p=0.000 n=8)
BuildCodeRootVerdictsNamespacedCorpus-12   B/op      12.48Mi ± 0% ->  20.35Mi ± 0%   +62.99% (p=0.000 n=8)
BuildCodeRootVerdictsNamespacedCorpus-12   allocs/op 215.8k ± 0%  ->  220.1k ± 0%     +1.99% (p=0.000 n=8)
```

A first implementation (`lexicalCandidates` pre-building a `[]string` of every
candidate via `strings.Split`/`strings.Join`) measured a nearly-identical
ns/op regression with a WORSE allocs/op (+2.83%, one extra slice allocation per
onward hop). It was replaced with `lexicalExactMatch` walking the namespace
directly via `strings.LastIndex` (no segment-slice, no candidates-slice, one
string concat per level tried) — this is the version reflected above.

Root cause (confirmed via `go tool pprof -alloc_objects`, 90% of allocation
volume traced to `strings.genSplit` inside the PRE-EXISTING (#5376)
`rubyRepoWideControllerRegistry.SuffixMatches`): allocation COUNT barely moved
(+2%); the +63-76% B/op growth is each `walkClass`/`onwardHop` recursion level's
existing `cloneChain`/`cloneVisited` copies getting BIGGER, because the fix
makes the walk go one level DEEPER for every ref that now resolves exactly
instead of stopping early at the keep-biased `suffix_only_ambiguous` branch.
This is not implementation waste; it is the direct, expected cost of computing
a materially more precise verdict instead of bailing out ambiguous. Wall-clock
ns/op was noisy on this shared, concurrently-loaded machine (a same-corpus
rerun later showed no significant ns/op difference at n=5); the B/op and
allocs/op deltas were reproducible and directionally identical across every
run and are the reported evidence.

No-Regression Evidence: the walk keeps the SAME asymptotic bound
(O(#classes + #roots)), the SAME `MaxWalkDepth` = 10 cap, and the SAME
resolved-class-key cycle guard — no unbounded blowup risk is introduced.
`BuildCodeRootVerdicts` runs once per reducer partition per generation cycle as
part of the existing CodeReachability projection pass (in-memory only, no new
I/O, no new lock/lease/queue path); it is not a per-request or Cypher hot path.
An extra ~9 ms on a synthetic 500-controller worst-case corpus is immaterial
next to the reducer cycle's existing multi-millisecond-to-multi-second SQL and
graph-write stages (see evidence-5376-code-root-verdicts.md's own Q1-Q3
`EXPLAIN ANALYZE` numbers).

Observability Evidence / No-Observability-Change: `BuildCodeRootVerdicts`
already reports `CodeRootVerdictStats.SuffixAmbiguousKept`, `Confirmed`, and
`Downgraded` via the existing `verdicts_confirmed`/`verdicts_downgraded`
reducer logs (see `evidence-5376-code-root-verdicts.md`'s Observability
Evidence). The #5500 restriction reuses these unchanged; an operator watching
`SuffixAmbiguousKept` trend down on a namespaced corpus after this change is
the expected, observable precision signal — no new metric or log is required.

## Schema-epoch / backfill assessment

The lexical-scope restriction changes VERDICT SEMANTICS for an
already-namespace-qualified corpus (a `suffix_only_ambiguous` verdict can now
resolve `downgraded` or `confirmed`/`accepted` for identical, unchanged source),
but it changes NEITHER the `code_root_verdicts` schema NOR any node/edge
identity: `CodeRootVerdictRow`'s key (scope/generation/repository/entity_id) is
unchanged, no new column, no new graph label/edge type. This is exactly the
case `CodeReachabilityVerdictSchemaEpoch`
(`go/internal/storage/postgres/code_reachability.go`) exists for — bumped `1 ->
2`. The loader's `coalesce(reach_verdict_epoch, 0) < $2` disjunct
(`code_reachability_loader.go`) re-schedules every already-indexed repository's
watermark exactly once (self-extinguishing: the runner re-stamps the new epoch
after the one re-projection, per the #5376 P1 upgrade-backfill loader-plan
proof already recorded in `evidence-5376-code-root-verdicts.md`), so every
already-indexed repo automatically re-projects its controller verdicts under
the new lexical-scope-aware logic with no separate backfill script, no
watermark reset, and no graph migration. This is forward-only and safe to ship
in the same PR as the epoch bump.
