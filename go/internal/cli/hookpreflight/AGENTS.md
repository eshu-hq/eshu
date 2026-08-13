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
  multi-field request advises on. `TestDocLockstepScopeResolutionIsFirstMatch`
  (doc_lockstep_behavior_test.go) is what holds it: turning the refusal into a
  `continue` advises on `service=checkout` for a request whose `repo_path` is
  `/etc/passwd`, which that test pairs against the same request without the
  unsafe field so it fails on the ordering rather than on the rejection.
- **`--json` output only ever comes from a `DecisionAdvise` `Output`.** The
  wrapper checks this before calling `ClaudePreToolUseOutputForPreflight`;
  do not make that function tolerate a skip `Output` by returning non-empty
  `AdditionalContext` for one.

## Common changes and how to scope them

- **Add a new trigger class** → add it to the `case` list in
  `triggerAllowed` (preflight.go), to `documentedTriggers()`
  (doc_lockstep_behavior_test.go), and to `triggerFromClaudeTool`'s switch
  (claude.go) if a Claude tool name should map to it. Update
  docs/public/reference/assistant-fast-path-hooks.md's "Trigger Classes"
  section in the same PR — that doc is the contract, not just a reference.
  `TestDocLockstepAllowedTriggersMatchDoc` reads the accepted classes out of
  `triggerAllowed` with a `go/ast` walk rather than probing a candidate list,
  so a class added to that `case` list is compared whether or not anyone
  thought to probe for it. What it compares against is `documentedTriggers()`,
  a hand-maintained transcription, plus the contract doc's "Trigger Classes"
  bullets — it does **not** require the doc to name your new class by its
  code-side spelling (`grep` appears nowhere in the contract doc), so editing
  the code and `documentedTriggers()` together with the doc untouched passes.
  Updating the doc is a rule here, not something the test proves.
  Keep `triggerAllowed` in the closed shape that walk requires: its body must
  be exactly one `switch trigger { ... }` whose clauses are string literals
  returning a bare `true`, plus one `default` returning a bare `false`. The
  switch tag must be the bare parameter, optionally wrapped in
  `strings.TrimSpace`/`strings.ToLower` — those fold spellings of one class
  together and cannot map one class onto another, which a helper like
  `canonicalTrigger(trigger)` can. An early `if`, a conditional inside a
  clause, a returned variable or comparison, a tagless switch, a rewritten tag,
  or a clause returning anything else all accept a class the literal list never
  names, so `TestDocLockstepSwitchScannerRejectsEvasions`
  (doc_lockstep_switch_fixtures_test.go) treats each of them as a structural
  violation. If a class genuinely needs a condition, that condition belongs in
  `Evaluate`'s switch as its own skip reason, not hidden inside this one.
- **Do not rewrite a trigger on its way to the gate.** `normalizeInput` may
  lowercase and trim `Input.Trigger` and nothing else; `baseOutput` copies it
  onto the wire unchanged; `MergeClaudePreToolUseInput` is the one deliberate
  translation, and only through `triggerFromClaudeTool(payload.ToolName)`.
  `Evaluate`'s switch must consult `triggerAllowed` itself, on a bare
  `.Trigger` field. Both ends are pinned by
  `doc_lockstep_trigger_path_test.go`, because a remap in `normalizeInput` or a
  call to a widened twin changes which classes get an advisory while
  `triggerAllowed` stays byte-identical for a reader to check.
- **Change which Claude tools fire the hook** → edit `triggerFromClaudeTool`
  (claude.go). `TestDocLockstepClaudeToolTriggerClasses`
  (doc_lockstep_publish_safety_test.go) drives every tool the contract doc's
  exclusion sentence names through `MergeClaudePreToolUseInput` and `Evaluate`
  and requires each to come back as a skip with an empty `additionalContext`,
  so remapping `Bash` into a read-family class fails there.
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
  absolute path or URL leaking into `Output.Scope.ID`, which the CLI then
  echoes both in its `scope:` text line and inside the Claude hook
  `additionalContext` string — treat any relaxation here as a publish-safety
  change requiring the same scrutiny as a new output field.
  `TestDocLockstepScopeSafeRejectionsStayUnpublished`
  (doc_lockstep_publish_safety_test.go) drives one input per rejection kind
  README.md lists through `Evaluate` and asserts the decision, the absent
  `Output.Scope`, and an empty `additionalContext`. Two of the six clauses
  overlap the character-class check — `~` and `\` are outside
  `[A-Za-z0-9._/:-]` too — so deleting either explicit check changes no
  decision and no test can go red on it. The character-class loop is what
  actually holds those two; treat it accordingly.

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
- Symptom: a `TestDocLockstep*` test fails after a code change → the change
  made a sentence in `README.md`, `doc.go`, this file, the contract doc, or a
  `preflight.go` constant comment false. `doc_lockstep_test.go` pins the
  structural claims (which structs carry `json` tags and under what wire
  names, which packages the non-test files import, the
  `assistant_fast_path_hook.v1` literal, the three reason codes the contract
  doc's "Safe Failure Modes" table names, both places that table's timeout
  code appears), `doc_lockstep_behavior_test.go` pins the behavioral ones
  (reason-code precedence, first-match scope resolution, that no skip
  publishes a scope or planned call, `DefaultBudget`, the trigger classes),
  `doc_lockstep_source_test.go` walks the source with `go/ast` for the claims
  a hand-written list cannot carry (the full set of json-tagged structs, and
  that the only `fmt` and `path/filepath` calls in production files are
  `Fprintf`/`Sprintf` and `IsAbs`/`Rel`/`Clean`/`ToSlash`),
  `doc_lockstep_switch_test.go` holds `triggerAllowed` to the closed-switch
  shape that walk depends on (with its fixture drive in
  `doc_lockstep_switch_fixtures_test.go`),
  `doc_lockstep_trigger_path_test.go` pins the path the trigger takes to reach
  that switch, `doc_lockstep_publish_safety_test.go` pins the `scopeSafe`
  rejections and the Claude tool-to-class mapping, and
  `doc_lockstep_literal_test.go` counts the contract-name mentions in the
  package docs rather than checking each file has one. Fix the doc or the code
  — do not relax the assertion. These exist because four rounds of hand-edited
  doc corrections on this package each fixed the reported sentence and left the
  next one standing.
- Symptom: a scanner reports nothing for a file you can see in the directory →
  check for a `//go:build` line. `parseNonTestGoFiles` reports
  build-constrained files separately instead of parsing them, because a file
  the compiler skips is not the package's behavior; a real one carrying a
  constraint fails `TestDocLockstepNoBuildConstrainedFiles` rather than going
  quietly unscanned.
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
  `TestDocLockstepProductionCallsStayPure` allows only `fmt.Fprintf` and
  `fmt.Sprintf` in the production files, so a stray `Println` fails there.
- **Reaching the filesystem through `path/filepath`.** The four functions used
  (`IsAbs`, `Rel`, `Clean`, `ToSlash`) are pure string operations, and the same
  test allows only those four. `Glob`, `Abs`, `WalkDir`, and `EvalSymlinks`
  live in the same already-imported package and would clear the import check on
  their own — that is exactly why the call-level pin exists.
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
  row order is not a precedence. So `preflight.go` is the only place the
  precedence is written down, and `TestDocLockstepReasonPrecedence`
  (doc_lockstep_behavior_test.go) is the only thing that holds it there —
  every one of its cases is ineligible for several reasons at once, so
  reordering the switch fails it. That test is the gate; it is not a substitute
  for the ADR, because it will happily pin whatever order you put in front of
  it once you edit the expectations too.
- `DefaultBudget` (200ms) — this is the contract's documented latency
  budget, not a locally-tunable default; changing it needs the benchmark
  evidence the contract doc requires for any budget change.
  `TestDocLockstepDefaultBudgetMatchesContract` (doc_lockstep_behavior_test.go)
  is the gate, and like the precedence test it is not a substitute for the ADR:
  it asserts the constant, the milliseconds it puts on the wire, and the
  contract-doc and AGENTS.md sentences quoting 200 ms, so raising the budget
  means editing all of them and noticing you did.
