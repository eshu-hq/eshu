# AGENTS — cmd/pr-watch

Scoped rules for editing the PR watcher. Load `golang-engineering`.

## Read first

1. `doc.go` — the event contract; every event line is part of it.
2. `.agents/skills/eshu-session-lifecycle/references/babysit-prs.md` — how
   sessions use the watcher.

## Invariants

- **One line per event, once per change, nothing for unchanged state.** Add a
  new event only with a test that polls the same snapshot repeatedly and
  asserts silence.
- **Never cache PR state across polls.** `prState` holds markers of what was
  already reported, never a fact compared against GitHub later without a fresh
  read.
- **Never exit silently.** Every stop path prints a `WATCH` line.
- **Pin the account on every gh call.** Child environments come from `ghEnv`,
  which strips ambient tokens. A new gh call goes through `Watcher.gh`.
- **Rollup state plus totalCount decides green and pending**, never the first
  page of contexts. Checks are paged to find failures past the hundredth.
- **GREEN needs the merge-rule contexts.** `observeGreen` requires every
  context in `Config.Required` (read from the gate registry by
  `resolveRequired`, never a hardcoded list) present and SUCCESS, every check
  read, and no pending check. A change to GREEN needs a test where the pre-wave
  set of always-on checks stays silent. `Run` refuses to start with no
  required contexts.
- **Every child process has a deadline, dies with its whole process group, and git
  never prompts.** New calls go
  through `Watcher.gh` or `Watcher.git`; do not call `w.exec` directly.
- **PR data reaches git only after `validateRefs`, and after `--`.**
- **Read-only toward GitHub.** No pushes, replies, thread resolution, or
  merges. The only local side effect is `git fetch` for a test merge.
- **Conflict labels follow
  [conflict-recipes.md](../../../.agents/skills/eshu-code-review/references/conflict-recipes.md),
  exactly.** `recipeFiles` and
  `agentRuleFile` in `conflict.go` name only paths that file names; add no
  globs it does not name. The generated list is read from the checkout, never
  copied. Changing a label means updating `doc.go`, `README.md`, and
  `babysit-prs.md`.
- No hand-kept allowlists beyond `noiseAuthors`, `recipeFiles`, and `agentRuleFile`; if a
  third list appears, fix the underlying signal instead.

## Tests

```bash
cd go && go test ./cmd/pr-watch/ -race -count=1
```

`fake_world_test.go` seeds GitHub and git in memory. `endtoend_test.go` re-runs
the test binary as `gh` and uses a real repository; it needs `git` on PATH and
no network.
