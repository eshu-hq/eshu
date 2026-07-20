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
  declared non-controller reject branch. Fizzle, cycle, depth cap, and
  unresolved Controller-suffixed bases MUST stay keep-biased.
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
