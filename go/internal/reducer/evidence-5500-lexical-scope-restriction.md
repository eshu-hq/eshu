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

## P0 fix (post separate-context review): union-accumulation, not first-hit

A separate-context review (`eshu-code-review`) found that the first shipped
`lexicalExactMatch` returned on the FIRST non-empty `ExactMatches` hit
(innermost scope first) and never tried the remaining outer scopes OR the bare
`ref` once it had a hit. `classNamespaceOf` derives `namespace` purely from the
walked classKey's own qualified name, which CANNOT distinguish Ruby's
nested-module-block declaration form (`module Admin; class Foo < Bar; end;
end`, where `Module.nesting` really does include `Admin`) from the COMPACT
COLON form (`class Admin::Foo < Bar`, where `Module.nesting` does NOT include
`Admin` unless the file also lexically wraps it) — `qualifiedClassName`
(`go/internal/parser/ruby/nodes.go`) produces the IDENTICAL qualified name for
both. Consequence: for a top-level compact-colon `class Admin::OrdersController
< Base`, if an unrelated `Admin::Base` coincidentally exists anywhere in the
corpus, the first-hit implementation returned `[Admin::Base]` and NEVER tried
bare `Base`. `SuffixMatches` only returns STRICT offset>0 matches
(`code_root_verdicts.go`'s `rubyQualifiedNameHasStrictSuffix`), so the true
offset-0 top-level `Base` referent was excluded from the candidate set
entirely — the walk proceeded against the WRONG identity, and a genuinely live
controller extending the real top-level `Base < ApplicationController` could
be wrongly `downgraded`. This is the exact false-positive class #5376/#5500
promise never to reintroduce.

**Fix**: `lexicalExactMatch` now accumulates the UNION of `ExactMatches` across
EVERY tried scope level plus the bare `ref`, instead of returning on the first
hit. This keeps the documented additive-only property (more candidates only
strengthen `onwardHop`'s any-path-keeps aggregation, never weaken it — a
downgrade still requires EVERY unioned candidate to vote downgrade) while
eliminating the masking: the true bare referent for a compact-colon
declaration always stays in the candidate set alongside any coincidentally
same-named inner-scope class.

**Regression tests (failing→green, verified live by reverting only
`lexicalExactMatch` to the first-hit version and rerunning)**:

- `go/internal/rubycontroller/controller_test.go` —
  `TestDecide/compact-colon_form:_coincidental_inner-scope_match_does_not_mask_the_true_top-level_referent`:
  `classes = {"Admin::OrdersController": ["Base"], "Admin::Base":
  ["ActiveRecord::Base"], "Base": ["ApplicationController"]}`. Pre-fix: `Keep =
  false, Terminal = "rejected_base:ActiveRecord::Base"` (the coincidental
  `Admin::Base` masked the true `Base` referent). Post-fix: `Keep = true,
  Reason = accepted` (both candidates stay in play; `Base` →
  `ApplicationController` rescues via any-path-keeps).
- `go/internal/reducer/code_root_verdicts_lexical_scope_test.go` —
  `TestBuildCodeRootVerdictsLexicalScopeCoincidentalInnerMatchDoesNotMask`
  reproduces the same shape through the REAL production registry
  (`rubyRepoWideControllerRegistry`).
- `go/internal/reducer/code_root_verdicts_integration_test.go` —
  `TestBuildCodeRootVerdictsLexicalScopeCompactColonFormDoesNotMaskTrueReferentFromRealParserEmissions`
  reproduces it end-to-end through the REAL Ruby parser with genuine compact
  colon source (`class Admin::OrdersController < Base` with NO enclosing
  `module Admin` block, plus a module-nested unrelated `Admin::Base <
  ActiveRecord::Base` and a genuine top-level `Base < ApplicationController`)
  — no hand-built qualified names.
- All three PASS after the fix; all three FAIL (as the false downgrade) when
  `lexicalExactMatch` is temporarily reverted to the first-hit version. Every
  pre-existing test in this PR (the exact-downgrade case, the outer-scope
  case, the no-op case, the Terminal-pinning case) stays green — the union
  fix is a strict superset of the first-hit behavior for every case where no
  coincidental masking exists.

## Performance measurement (representative corpus, post P0-fix)

Benchmarks (both in
`go/internal/reducer/code_root_verdicts_lexical_scope_test.go`):
`BenchmarkBuildCodeRootVerdictsNamespacedCorpus` (worst-case: 500 controllers
nested 2 module levels deep, 20% with a base unresolvable anywhere in the
lexical chain — the shape the issue documents as inflating
`SuffixAmbiguousKept` on a heavily namespaced corpus) and
`BenchmarkBuildCodeRootVerdictsNamespacedCorpusTypical` (same shape at the
evidence-5376-doc's own ~99%-correctly-based ratio — "the throughput number
that matters for a real Rails corpus"). Compared against the pre-#5500 commit
on the identical corpus (`go test ./internal/reducer -run='^$'
-bench='BenchmarkBuildCodeRootVerdictsNamespacedCorpus$|BenchmarkBuildCodeRootVerdictsNamespacedCorpusTypical$'
-benchmem -benchtime=30x -count=8`, `benchstat` old vs new, both reported —
neither hidden):

```
BuildCodeRootVerdictsNamespacedCorpus-12         sec/op    17.24m ± 7%  -> 22.76m ± 38%  +31.99% (p=0.000 n=8)
BuildCodeRootVerdictsNamespacedCorpusTypical-12  sec/op    16.26m ± 4%  -> 21.44m ±  5%  +31.83% (p=0.010 n=8)
geomean                                                    16.75m      -> 22.09m        +31.91%

BuildCodeRootVerdictsNamespacedCorpus-12         B/op      12.78Mi ± 0% -> 21.70Mi ± 0%  +69.87% (p=0.000 n=8)
BuildCodeRootVerdictsNamespacedCorpusTypical-12  B/op      12.76Mi ± 0% -> 22.44Mi ± 0%  +75.81% (p=0.000 n=8)
geomean                                                    12.77Mi     -> 22.07Mi        +72.82%

BuildCodeRootVerdictsNamespacedCorpus-12         allocs/op 223.3k ± 0%  -> 229.7k ± 0%   +2.84% (p=0.000 n=8)
BuildCodeRootVerdictsNamespacedCorpusTypical-12  allocs/op 211.2k ± 0%  -> 218.2k ± 0%   +3.30% (p=0.000 n=8)
geomean                                                    217.2k      -> 223.9k         +3.07%
```

The Typical (realistic ~99%-correctly-based) corpus is NOT cheaper than the
worst-case corpus here — B/op is actually slightly WORSE for Typical
(+75.81% vs +69.87%), because most of the added cost comes from correctly-based
controllers now resolving via the deeper exact-resolution recursion path
instead of the shallow accepted-literal short-circuit; both numbers are
reported so neither is hidden as "the real-world case is cheap."

An earlier iteration (`lexicalCandidates` pre-building a `[]string` of every
candidate via `strings.Split`/`strings.Join`, and a since-superseded
first-hit `lexicalExactMatch`) measured similar or worse allocation profiles;
each was replaced in place, and the numbers above are for the final,
union-accumulating, `strings.LastIndex`-walking version.

Root cause (confirmed via `go tool pprof -alloc_objects` on the first-hit
version, and structurally unchanged by the union fix): allocation COUNT moves
only ~3%; the +70-76% B/op growth is each `walkClass`/`onwardHop` recursion
level's existing `cloneChain`/`cloneVisited` copies getting BIGGER, because the
fix makes the walk go one level DEEPER — and, after the P0 fix, sometimes walk
MULTIPLE additional candidates per hop — for every ref that now resolves
exactly instead of stopping early at the keep-biased `suffix_only_ambiguous`
branch. This is not implementation waste; it is the direct, expected cost of
computing a materially more precise (and, after the P0 fix, more SAFELY
inclusive) verdict instead of bailing out ambiguous or masking a candidate.
Wall-clock ns/op was noisy on this shared, concurrently-loaded machine across
every run in this investigation; the B/op and allocs/op deltas were
reproducible and directionally identical across every run and are the primary
reported evidence.

No-Regression Evidence: the walk keeps the SAME asymptotic bound
(O(#classes + #roots)), the SAME `MaxWalkDepth` = 10 cap, and the SAME
resolved-class-key cycle guard — no unbounded blowup risk is introduced.
`BuildCodeRootVerdicts` runs once per reducer partition per generation cycle as
part of the existing CodeReachability projection pass (in-memory only, no new
I/O, no new lock/lease/queue path); it is not a per-request or Cypher hot path.
An extra ~5-6 ms on a synthetic 500-controller corpus (worst-case OR typical)
is immaterial next to the reducer cycle's existing multi-millisecond-to-multi-
second SQL and graph-write stages (see evidence-5376-code-root-verdicts.md's
own Q1-Q3 `EXPLAIN ANALYZE` numbers). Per repo rules this in-memory,
once-per-generation cost is not a blocker on its own — the requirement here is
that the evidence be complete and honest about both the worst-case and the
realistic-case numbers, which it now is.

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

**KNOWN EPOCH COLLISION — flagged, not silently resolved**: as of this
worktree's last rebase onto `origin/main` (`8f2f959a9`), `origin/main` still
has `CodeReachabilityVerdictSchemaEpoch = 1`, so this branch's `1 -> 2` bump is
correct for now. However, issue #5494 independently bumps the SAME constant
for a DIFFERENT verdict-semantics change. Whichever of #5500 / #5494 merges
SECOND must rebase and bump to `3` (not re-use `2`), or the second-merged
change's own re-projection trigger will be silently absorbed by the first
merge's `1 -> 2` epoch stamp and never re-run. The orchestrator is sequencing
this merge order; this constant must not be merge-resolved silently by either
branch.

## P1 fix (post separate-context review, #5733): absolute-reference provenance

A separate-context review (`eshu-code-review`, codex) of this PR found that a
superclass declared with an explicit leading `::` (`class
OrdersController < ::Base`, real Ruby's absolute-constant-path marker — it
resolves starting at Object with NO `Module.nesting` search) was
indistinguishable from a bare, relative `Base` by the time it reached
`onwardHop`. Two independent strip points destroyed the marker before the
lexical-scope restriction ever saw it: `superclassQualifiedName`
(`go/internal/parser/ruby/nodes.go`) trimmed the leading `::` when building
`qualified_bases`, and `normalizeBases` (`go/internal/rubycontroller/helpers.go`)
trimmed it again unconditionally on every declared base. Consequence: for a
namespaced controller (`Admin::OrdersController < ::Base`) with an unrelated,
coincidentally-named `Admin::Base` in the corpus, `lexicalExactMatch` wrongly
tried `Admin::Base` as a candidate — real Ruby would never search the
enclosing namespace for an absolute reference — and could resolve it EXACTLY,
letting the walk downgrade a genuinely live controller whose true `::Base` is
external to the corpus (e.g. a gem). Before #5500, the same shape only ever
produced a broad, unscoped `SuffixMatches` candidate and stayed
`suffix_only_ambiguous` (kept); #5500's precision upgrade is what newly made
this reachable as a false EXACT match.

**Fix**: `superclassQualifiedName` now preserves the leading `::` in
`qualified_bases` instead of stripping it (the reducer's
`rubyRepoWideControllerRegistry.DeclaredBasesOf` already returned declared
bases verbatim — only `strings.TrimSpace`, no `::` handling — so no reducer
code change was needed there). `normalizeBases` now returns
`[]resolvedBase{Name, Absolute}` instead of `[]string`: it splits the leading
`::` into `Absolute` and strips it from `Name` before any `Registry` method
ever sees the ref (so `ExactMatches`/`SuffixMatches` implementations are
unaffected — they still only ever receive plain, unqualified-marker refs).
`onwardHop` and `lexicalExactMatch` both take a new `absolute bool` parameter;
when true, `lexicalExactMatch` skips the enclosing-namespace search entirely
and returns only the bare top-level `reg.ExactMatches(ref)` — the SuffixMatches
confirm-only path (step 3) is untouched, since a suffix candidate can never
feed a downgrade regardless of absoluteness. `probeClassConfirm` needed no
`absolute` handling: it never performs namespace-prefixed lexical search to
begin with.

This is a data-availability fix, not a decision-logic reinterpretation of
existing facts: a repo already indexed under the OLD parser has no `::` marker
in its stored `qualified_bases` (the parser never emitted one), so the fixed
`normalizeBases` computes `Absolute=false` for that data exactly as before —
behavior for every already-stored fact is byte-for-byte unchanged. The fix
only takes effect once a repo is re-parsed under the corrected parser and
re-emits `qualified_bases` with the marker preserved, which is the normal,
expected lifecycle of any parser bug fix. Unlike the union-accumulation P0 fix
above (which changes the DECISION for facts already sitting in Postgres, hence
the `CodeReachabilityVerdictSchemaEpoch` bump), this fix requires no epoch
bump: the "same stored facts, different logic" precondition the epoch
mechanism exists for does not hold here.

**Regression tests (failing→green)**:

- `go/internal/parser/ruby/parser_test.go` —
  `TestParseEmitsQualifiedBasesPreservesAbsoluteMarker`: `class
  OrdersController < ::Base` inside `module Admin` now emits
  `qualified_bases: ["::Base"]` (`bases`, the last-segment fact, is unaffected
  at `["Base"]`). Pre-fix: `qualified_bases: ["Base"]` (marker stripped).
- `go/internal/rubycontroller/controller_test.go` — `TestDecide/absolute_top-level_reference_bypasses_lexical_scope_and_stays_ambiguous-keep`:
  `classes = {"Admin::OrdersController": ["::Base"], "Admin::Base":
  ["ActiveRecord::Base"]}`. Pre-fix: `Keep = false, Reason =
  rejected_framework_base` (wrongly resolved onto the coincidental
  `Admin::Base`). Post-fix: `Keep = true, Reason = suffix_only_ambiguous`
  (matches the pre-#5500 behavior for this shape — the real `::Base` is absent
  from the corpus). A companion case,
  `TestDecide/absolute_top-level_reference_resolves_exactly_against_the_real_top-level_class`,
  proves the fix does not simply disable exact resolution for absolute refs:
  with a genuine top-level `Base < ActiveRecord::Base` present (no coincidental
  namespace-mate), the walk still downgrades via `rejected_framework_base`.
- `go/internal/reducer/code_root_verdicts_lexical_scope_test.go` —
  `TestBuildCodeRootVerdictsAbsoluteReferenceBypassesLexicalScope` reproduces
  the same shape through the REAL production registry
  (`rubyRepoWideControllerRegistry`) with a hand-built `QualifiedBases:
  []string{"::Base"}`.
- `go/internal/reducer/code_root_verdicts_integration_test.go` —
  `TestBuildCodeRootVerdictsAbsoluteReferenceFromRealParserEmissions`
  reproduces it end-to-end through the REAL Ruby parser with genuine source
  (`class OrdersController < ::Base` inside `module Admin`, plus a
  module-nested unrelated `Admin::Base < ActiveRecord::Base`) — no hand-built
  qualified names or bases.
- All four PASS after the fix. All four FAIL (as the false downgrade / missing
  marker) against the pre-fix code. Every pre-existing test in this file and
  package stays green — the fix only changes behavior for a base ref that
  literally carries a leading `::`, which no prior test exercised.
