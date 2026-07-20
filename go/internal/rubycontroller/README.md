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

## Contract

- `Registry` abstracts class ancestry. The parser backs it with a single-valued
  same-file view; the reducer backs it with a repo-wide multimap that unions
  declared bases across reopened and short-name-colliding class definitions.
- `Decide` walks the chain and returns keep/downgrade plus provenance (`Chain`,
  `Terminal`, `Reason`). `IsRailsController` is `Decide(...).Keep`.
- The walk is multi-path (a colliding name evaluates every declared base; ANY
  keeping path keeps), cycle-safe (per-branch visited set), and depth-capped at
  `MaxWalkDepth`.
- Downgrade (`Keep == false`) is returned **only** on positive evidence. Every
  inconclusive outcome keeps.

## Invariant

Every same-file KEEP the parser produced stays KEEP repo-wide. Because the
repo-wide registry is a superset of same-file knowledge, the only decisions that
change are unresolved cross-file bases the repo can now resolve onward — and a
resolved base flips to downgrade only when it lands on a non-controller reject
branch, exactly as the parser would have decided with that same knowledge.

## Callers

- `go/internal/parser/ruby` — same-file registry adapter.
- `go/internal/reducer` — repo-wide multimap registry (`code_root_verdicts`).
