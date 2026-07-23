# rubycontroller

Shared Rails-controller superclass-chain decision. One copy, two callers.

## Why this package exists

Issue #5337 taught the Ruby parser to root Rails controller actions as
dead-code roots by walking a class's declared superclass chain to an accepted
Rails base. That walk is **same-file only**: it uses a per-file registry built
top-down as classes are parsed. A controller whose real base lives in another
file (or a reopened class) falls through to a name-suffix guess and is
over-kept.

Issue #5376 closes that residual at the reducer, where full repo-wide class
ancestry exists. The reducer's downgrade rule MUST re-run the **identical**
decision the parser used — same accepted-base set, same suffix fallbacks, same
keep-biased asymmetries — only with a repo-wide multimap registry instead of the
same-file one. If the two decisions ever drifted, the reducer could downgrade a
controller the parser rooted (a hard false positive that recommends deleting
reachable code). Extracting the decision here makes drift structurally
impossible: both callers import the same `Decide`.

## Contract (identity-carrying resolution, #5376 P0 rev-2)

The walk operates on **resolved class identities** (class keys), never on ref
strings after the first resolution, so an impostor's ancestry can never
masquerade as a ref's.

- `Registry` exposes `ExactMatches(ref)` (offset-0), `SuffixMatches(ref)`
  (offset>0 STRICT suffix), `EntryMatches(ctx)` (last-segment multimap, entry
  hop only), and `DeclaredBasesOf(classKey)` (exact key lookup, no re-matching).
  The parser backs it with a same-file table whose `SuffixMatches` is **always
  empty** (making same-file behavior provably unchanged); the reducer backs it
  with a repo-wide qualified-name registry.
- `Decide` resolves the method's defining class via `EntryMatches`, then walks
  each candidate's chain, returning keep/downgrade plus provenance (`Chain`,
  `Terminal`, `Reason`). `IsRailsController` is `Decide(...).Keep`.
- **A proper-suffix (offset>0) match may never feed a downgrade.** Downgrade-
  capable onward resolution requires an offset-0 exact match. A suffix-only ref
  runs a confirm-only probe whose downgrade evidence is structurally discarded:
  it confirms or keeps (`suffix_only_ambiguous`), never downgrades.
- The reject-set (`ActiveRecord::Base`, `ApplicationRecord`, …) is reachable
  only through exact doors: a literal ref with zero corpus suffix matches, or
  the terminal of a fully exact-resolved chain.
- The conventional simple names `Base`/`API` with zero corpus candidates keep.
- Cycle guard keys on resolved class keys; depth-capped at `MaxWalkDepth`.

## Lexical-scope-aware candidate restriction (#5500, optional precision upgrade)

Alongside the broad, unscoped `ExactMatches(ref)` lookup, `onwardHop` also
tries the lexical-prefix names real Ruby constant lookup would try for a base
ref seen inside the walked class's own namespace `P` (`P`'s qualified name
minus its own last segment): `P::ref`, then each enclosing prefix of `P`
outward (mirroring `Module.nesting` search order), then top-level `::ref`
(`lexicalExactMatch`). `P` is derived purely from the walked classKey's own
qualified name (`classNamespaceOf`), so it is `""` — and the restriction a
documented no-op — for a top-level class or the parser's same-file registry
(whose class keys are always simple names).

**`lexicalExactMatch` returns the UNION of every level's `ExactMatches` hit —
it never stops at the first hit.** `classNamespaceOf` cannot distinguish a
genuinely nested-module-block declaration (`module Admin; class Foo < Bar;
end; end`, where Ruby's `Module.nesting` really does include `Admin`) from the
compact colon form (`class Admin::Foo < Bar`, where `Module.nesting` does NOT
include `Admin` unless the file also lexically wraps it) — `qualifiedClassName`
produces the identical qualified name for both. Stopping at the first
inner-scope hit would let a coincidentally same-named class at an inner
candidate level SILENTLY MASK the true bare top-level referent for a
compact-colon declaration (since `SuffixMatches` only returns STRICT offset>0
matches, the true offset-0 top-level referent would be unreachable any other
way once masked) — a real false-downgrade defect caught by review and fixed;
see `evidence-5500-lexical-scope-restriction.md`'s P0 section.

This changes NOTHING about the offset-0-vs-suffix downgrade rule above: it only
changes how EXACT matches are found. A base that previously had only a broad,
unscoped `SuffixMatches` candidate can now resolve EXACTLY when its true,
lexically-scoped referent is in-corpus, letting it confirm or downgrade instead
of staying `suffix_only_ambiguous` forever. It can never drop a match the prior
lookup found: the bare `ref` is always one of the unioned candidates, identical to
the pre-#5500 `ExactMatches(ref)` call. See
`evidence-5500-lexical-scope-restriction.md` in `go/internal/reducer` for the
correctness proof, the measured performance cost, and the schema-epoch
assessment.

### Absolute references are exempt from the namespace search (#5733)

A base declared with an explicit leading `::` (`class Foo < ::Base`) is real
Ruby's absolute-constant-path marker: it resolves starting at Object with NO
`Module.nesting` search, unlike the bare, relative `Base`. The two spellings
are different resolution rules that happen to share a trailing name, so
collapsing them loses correctness-relevant information.

`superclassQualifiedName` (`go/internal/parser/ruby/nodes.go`) preserves the
leading `::` in the emitted `qualified_bases` fact instead of stripping it.
`rubyRepoWideControllerRegistry.DeclaredBasesOf` (the reducer's repo-wide
registry) returns it verbatim. `normalizeBases` (helpers.go) is where the
marker is finally consumed: it splits each declared base into a `resolvedBase{
Name, Absolute }` pair, stripping `::` from `Name` (so no `Registry` method
ever sees a literal `::`) while carrying `Absolute` forward. `onwardHop` passes
`Absolute` to `lexicalExactMatch`, which — when true — skips the
enclosing-namespace search entirely and degrades to the bare top-level
`reg.ExactMatches(ref)`, exactly like the no-op case for a top-level walked
class.

Before this existed, an absolute reference's `::` was stripped before it ever
reached the lexical-scope search, making it indistinguishable from a relative
reference. For a namespaced controller (`Admin::OrdersController < ::Base`)
with an unrelated, coincidentally-named `Admin::Base` in the corpus, the
namespace search would wrongly exact-match `Admin::Base` and could downgrade
(recommend deleting) a genuinely live controller whose real `::Base` is
external to the corpus (e.g. a gem) — a real false-downgrade caught by review.

## Invariant

Under assumptions A1–A4 (the defining class is in-corpus; no gem constant under
an app-authored namespace; single inheritance; verbatim `qualified_bases`) no
genuine in-corpus controller is downgraded. Every inconclusive outcome keeps.

## Callers

- `go/internal/parser/ruby` — same-file registry adapter.
- `go/internal/reducer` — repo-wide multimap registry (`code_root_verdicts`).
