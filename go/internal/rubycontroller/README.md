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

## Invariant

Under assumptions A1–A4 (the defining class is in-corpus; no gem constant under
an app-authored namespace; single inheritance; verbatim `qualified_bases`) no
genuine in-corpus controller is downgraded. Every inconclusive outcome keeps.

## Callers

- `go/internal/parser/ruby` — same-file registry adapter.
- `go/internal/reducer` — repo-wide multimap registry (`code_root_verdicts`).
