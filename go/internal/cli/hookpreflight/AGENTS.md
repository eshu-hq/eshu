# AGENTS.md — go/internal/cli/hookpreflight guidance for LLM assistants

## Read first

1. `go/internal/cli/hookpreflight/README.md` — purpose, ownership boundary,
   exported surface
2. `go/internal/cli/hookpreflight/doc.go` — the godoc contract
3. `go/cmd/eshu/assistant_hook_preflight.go` — the cobra `RunE` wrapper that
   resolves process state (flags, stdin) and calls into this package. This
   is the file that shows how the two halves fit together.
4. `docs/public/reference/assistant-fast-path-hooks.md` — the
   `assistant_fast_path_hook.v1` contract this package implements: trigger
   classes, bounded query shape, 200ms latency budget, and the "Safe Failure
   Modes" table. That table and `Evaluate`'s skip reasons overlap but are not
   one-for-one: the table has a "Missing endpoint or token reference" row this
   package has no reason code for (nothing here resolves an endpoint or
   token), `disallowed_trigger` has no table row of its own (the "Trigger
   Classes" section carries that rule), and `stale_index` is an *advise*
   reason, not a skip.

## Invariants this package enforces

- **No process wiring in this package.** No cobra flags, no stdin reads, no
  `fmt.Print*` to a process stream. `go/cmd/eshu` is `package main`, so
  nothing can import it — any symbol that reads a flag or `cmd.InOrStdin()`
  has to live in `assistant_hook_preflight.go` instead. `RenderPreflightText`
  takes an `io.Writer` parameter rather than writing to stdout directly, so
  it stays testable without a cobra command.
- **`Evaluate` never returns an error.** Every ineligible condition (expired
  budget, unsupported host, disabled hook, disallowed trigger, denied
  permission, missing/unsafe scope, unavailable freshness) is a skip
  `Output`, not an `error`. The contract is fail-open: the original host
  tool call must proceed when the hook cannot safely advise.
- **Scope resolution is first-match, not best-match.** `scopeFromInput`
  checks `repo_path`, `entity_id`, `service`, `workload`, `environment`,
  `resource` in that order and stops at the first non-empty field. If that
  field fails `scopeSafe`, the whole call skips with `reasonBroadScope`
  rather than trying the next field — do not change this to "try all
  fields, use the first safe one" without an ADR; it changes which scope a
  multi-field request advises on.
- **`--json` output only ever comes from a `DecisionAdvise` `Output`.** The
  wrapper checks this before calling `ClaudePreToolUseOutputForPreflight`;
  do not make that function tolerate a skip `Output` by returning non-empty
  `AdditionalContext` for one.

## Common changes and how to scope them

- **Add a new trigger class** → add it to the `case` list in
  `triggerAllowed` (preflight.go) and to `triggerFromClaudeTool`'s switch
  (claude.go) if a Claude tool name should map to it. Update
  docs/public/reference/assistant-fast-path-hooks.md's "Trigger Classes"
  section in the same PR — that doc is the contract, not just a reference.
- **Add a new scope kind** → add a candidate to `scopeFromInput`
  (preflight.go) and a case to `plannedCallForScope` choosing which MCP tool
  answers it. Both must change together: a scope kind with no
  `plannedCallForScope` case falls through to the default
  `get_code_relationship_story` tool, which is silently wrong for a kind
  that needs its own tool.
- **Change the recommended MCP tool for a scope kind** → edit
  `plannedCallForScope` only. Do not touch `scopeFromInput`'s field order —
  that is the priority a multi-field request resolves by, not related to
  which tool answers a resolved scope.
- **Change what counts as a "safe" scope ID** → edit `scopeSafe`
  (preflight.go). This is the last line of defense against a private
  absolute path or URL leaking into `Output.Scope.ID`, which then
  serializes into both the JSON output and the Claude hook
  `additionalContext` string — treat any relaxation here as a publish-safety
  change requiring the same scrutiny as a new output field.

## Failure modes and how to debug

- Symptom: `eshu assistant hook preflight --json` prints nothing when a
  narrow scope is passed → check `Input.Elapsed` first: `runAssistantHookPreflight`
  in the wrapper fills it from its own `time.Since(start)`, so a slow flag
  read or a slow `readClaudePreToolUseInput` stdin read can push `Elapsed`
  past `Budget` before `Evaluate` ever reaches scope resolution. This is a
  wrapper-side timing question, not a bug in this package's logic.
- Symptom: a scope that looks safe (e.g. `service=checkout`) skips with
  `reasonBroadScope` → check `scopeFromInput`'s field order. If an earlier
  field (`repo_path`, `entity_id`) is also set but empty-after-trim or
  fails `scopeSafe`, the function returns `(Scope{}, false)` immediately —
  it does not fall through to the `service` field. This is deliberate (see
  Invariants above), not a bug.
- Symptom: `TestAssistantHookPreflightBenchmarkCasesCoverContract`
  (preflight_bench_test.go) fails after adding a case → the test asserts
  `len(cases) >= 6` and that every one of six named cases is present; a
  renamed `name` field without updating the `seen` check list at the bottom
  of the test fails it.

## Anti-patterns specific to this package

- **Printing from this package.** `RenderPreflightText` writes to an
  `io.Writer` parameter; nothing in this package calls `fmt.Print*` to a
  process stream. `fmt.Print*` belongs only in
  `go/cmd/eshu/assistant_hook_preflight.go`.
- **Reaching into `go/cmd/eshu`.** It cannot be imported (`package main`).
  If new logic needs something only the wrapper has (a cobra flag, stdin),
  add a parameter instead.
- **Turning a skip into an error.** `Evaluate`'s contract is fail-open; a
  skip `Output` is the correct, successful result for every ineligible
  input, not an error case the caller needs to branch on separately.

## What NOT to change without an ADR

- `Evaluate`'s check order (budget, then host, then enabled, then trigger,
  then permission, then scope, then freshness) — later checks assume earlier
  ones already passed, and this order alone decides which single reason code
  comes back when several conditions are ineligible at once. The contract doc
  (docs/public/reference/assistant-fast-path-hooks.md) does NOT pin this: its
  "Safe Failure Modes" table gives the required behavior per failure, and its
  row order is not a precedence. So this source file is the only place the
  precedence is written down — reordering the switch silently changes which
  reason a caller sees, with no doc or gate to catch it.
- `DefaultBudget` (200ms) — this is the contract's documented latency
  budget, not a locally-tunable default; changing it needs the benchmark
  evidence the contract doc requires for any budget change.
