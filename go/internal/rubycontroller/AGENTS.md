# AGENTS: rubycontroller

This package is the **single source of truth** for the Rails-controller
superclass-chain decision (#5376). Two callers depend on it staying one copy:
the Ruby parser (`go/internal/parser/ruby`) and the reducer code-root verdict
builder (`go/internal/reducer`).

## Hard rules

- NEVER fork this logic back into a caller. If a caller needs different
  ancestry, give it a different `Registry` implementation — never a different
  decision.
- NEVER add a downgrade path that fires on inconclusive evidence. Downgrade
  (`Keep == false`) is allowed only when every resolved path lands on a
  declared non-controller reject branch through an EXACT (offset-0) door.
  Fizzle, cycle, depth cap, unresolved Controller-suffixed bases, unresolved
  qualified bases, and suffix-only matches MUST stay keep-biased.
- NEVER let a proper-suffix (offset>0) match feed a downgrade (#5376 P0 rev-2).
  Suffix candidates participate only via the confirm-only probe. The walk MUST
  operate on resolved class identities, never re-union by ref string per hop.
- The #5500 lexical-scope-aware candidate restriction (`lexicalExactMatch`,
  `classNamespaceOf`) MUST keep the bare `ref` as one of its unioned
  candidates, and MUST return the UNION of every scope level's
  `ExactMatches` hit — NEVER stop at the first hit. `classNamespaceOf` cannot
  tell a genuinely nested-module-block declaration from Ruby's compact colon
  form (`class Admin::Foo < Bar` with no enclosing `module Admin` block); a
  first-hit implementation lets a coincidentally same-named inner-scope class
  SILENTLY MASK the true bare top-level referent for the compact-colon form (a
  real false-downgrade P0 caught by review — see
  `evidence-5500-lexical-scope-restriction.md`). It MUST stay
  `ExactMatches`-only (never call `SuffixMatches` per lexical candidate): that
  would reintroduce the broad, unscoped guessing this restriction exists to
  replace.
- Keep this package a leaf: `strings` and `sort` only. No parser, reducer,
  storage, or telemetry imports. Both callers import it; it imports neither.
- Changing `acceptedControllerBases`, `MaxWalkDepth`, the suffix rule, or the
  keep/downgrade asymmetry is a parser-and-reducer behavior change. Update the
  parser fixture (`ruby.fixture.json`) and the reducer verdict tests together,
  and re-run both packages' tests.

## Verification

```bash
GOCACHE=<worktree>/.gocache go test ./internal/rubycontroller/ \
  ./internal/parser/ruby/ ./internal/reducer/ -count=1
```
