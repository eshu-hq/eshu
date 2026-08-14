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
  `strings.TrimSpace`/`strings.ToLower` — literally those two calls, so
  factoring the pair into a named helper and writing
  `switch normalizedTrigger(trigger)` is **refused even though the helper would
  be pure**. That is deliberate, not an oversight: the scanner reads syntax, and
  a call it cannot see through is exactly how `canonicalTrigger(trigger)` mapped
  one class onto another while `triggerAllowed` stayed byte-identical. The cost
  of the rule is one factoring nobody needs; the cost of relaxing it is the
  evasion coming back.
  `TestDocLockstepEvaluateClausesTestWhatTheyDocument` carries the same kind of
  constraint for a different reason: `Evaluate`'s five clause conditions are
  matched **as written**, so `supportedHostClaude != normalized.Host` is
  rejected for `normalized.Host != supportedHostClaude` even though the two are
  identical. Spell them the documented way. Accepting arbitrary equivalent
  spellings needs an expression normalizer, and one loose enough to swap
  operands is loose enough to start accepting the extra conjunct that gate was
  added to catch. The failure message prints both sides, so a rejection here is
  a one-line edit rather than a puzzle — those fold spellings of one class
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
  `Evaluate`'s switch must consult `triggerAllowed` itself, on the `.Trigger`
  field of the variable it bound `normalizeInput`'s result to. A remap anywhere
  along that path changes which classes get an advisory while `triggerAllowed`
  stays byte-identical for a reader to check.
  "Lowercase and trim" is held to the *object* as well as the field: a permitted
  writer may normalize a `Trigger` read off its own receiver or parameters, and
  nothing else. `input.Trigger = readAlias.Trigger`, with a package-level
  `Input{Trigger: "read"}` behind it, looks like a normalization at the
  assignment and is a remap decided somewhere no reader of the function can see
  — and because it sits in `normalizeInput`, the rewritten class goes onto the
  wire too, so the advisory names a class the caller never sent.
  What holds it is `TestDocLockstepEvaluateAdvisesExactlyTheAllowedTriggers`
  (doc_lockstep_trigger_equivalence_test.go): on a request eligible in every
  other respect, `Evaluate` advises for exactly the triggers `triggerAllowed`
  accepts, compared over every string of up to four lowercase characters, every
  string literal the production files declare, every documented class, every
  string one character edit away from one of those, and — for the named classes
  and their neighbourhood — each of the 27 other request shapes in
  `triggerAxisVariants`. It asserts the behaviour, so it does not care how the
  rewrite is written.
  It is still a property over a bounded sample, though, and the bound is where
  it fails. When a new evasion slips through, work the bound first: is the class
  it widens outside the swept set, or is the request it needs outside
  `triggerAxisVariants`? Widen the sweep or the axis set before concluding the
  property itself is wrong — while remembering that the axis list is an
  enumeration and widening it only ever catches the value you just added.
  A gate keyed on a field rather than on the trigger is closed structurally
  instead, by `TestDocLockstepEvaluateHasOneAdvisePath`: `Evaluate` reaches
  `DecisionAdvise` in exactly one place, at the top level of its body, so a
  second advise path fails whatever it keys on. Answering each new evasion with one more
  source-scanner spelling is what the four earlier generations did and it did
  not converge — but the cheap structural belts in
  `doc_lockstep_trigger_alias_test.go` stay regardless. Every remap found so far
  that escaped the sample reached `Input.Trigger` in a way the writer scan
  cannot see: through a pointer to the field, or through a positional composite
  literal. When a new one turns up, check whether it reached the field a third
  way and add the rule, rather than reading a passing sweep as proof that the
  belts are redundant.
  `doc_lockstep_trigger_path_test.go` still holds the source side, and is not
  redundant: it catches the one case the equivalence cannot see, a widening
  applied to `triggerAllowed` and to `Evaluate` at the same time.
  `doc_lockstep_trigger_axes_test.go` holds the sample that equivalence runs
  over — the neighbourhood alphabet, the request shapes, and a literal pin on
  the two sweep constants — and `doc_lockstep_trigger_alias_test.go` refuses the
  pointer writes and positional literals the writer scan cannot see.
- **Change which Claude tools fire the hook** → edit `triggerFromClaudeTool`
  (claude.go). `TestDocLockstepClaudeToolTriggerClasses`
  (doc_lockstep_publish_safety_test.go) drives every tool the contract doc's
  exclusion sentence names through `MergeClaudePreToolUseInput` and `Evaluate`
  and requires each to come back as a skip with an empty `additionalContext`,
  so remapping `Bash` into a read-family class fails there.
  That test enumerates tool *names*, and the contract doc's exclusion sentence
  is about command *families* — `NotebookEdit` is in no list, and a case mapping
  it to `read` published a full advisory for a notebook write with the suite
  green. Two properties in the same file close that gap:
  `TestDocLockstepExcludedToolFamiliesNeverReachAReadClass` builds names out of
  each excluded verb (`NotebookEdit`, `MultiWrite`, `pre-commit`, `push_file`,
  …) and requires every one to skip, and
  `TestDocLockstepClaudeToolTranslationsAreEnumerated` compares the mapping's
  *translations* — a case whose class differs from the tool's own lowercased
  name — against `claudeToolTriggerClasses`, taking the candidate names from the
  production files' string literals. Adding a case means adding its row and
  deleting a case means deleting the row; the comparison fails both ways. It
  compares behavior rather than syntax, so a case returning a computed class
  fails it exactly as a literal one does.
- **Claim support for another host** → don't, unless the contract doc's
  Implementation Gate is satisfied for that host. `supportedHostClaude`
  (preflight.go) is the whole accepted set, and
  `TestDocLockstepEvaluateAdvisesForExactlyOneHost`
  (doc_lockstep_gate_inputs_test.go) holds it as an equality over a swept host
  rather than as a list of hosts refused today — accepting `cursor` alongside
  `claude` passed everything before that test existed, and put `"host":
  "cursor"` on the wire under a doc that lists Cursor as guidance-only.
  `TestDocLockstepSupportedHostBoundaryDoc` pins the doc's own table, row counts
  included, so a family moved between tiers fails too.
- **Add a new scope kind** → add a candidate to `scopeFromInput`
  (preflight.go) and a case to `plannedCallForScope` choosing which MCP tool
  answers it. Both must change together: a scope kind with no
  `plannedCallForScope` case falls through to the default
  `get_code_relationship_story` tool, which is silently wrong for a kind
  that needs its own tool.
  That rule is enforced now, not only written here.
  `TestDocLockstepEveryScopeKindNamesItsPlannedCall`
  (doc_lockstep_scope_kind_test.go) reads the candidate kinds out of
  `scopeFromInput`'s `[]Scope` literal and the cased kinds out of
  `plannedCallForScope`'s switch, and requires the two to be the same set — so
  a seventh candidate with no case is red, and so is deleting a case for a kind
  that still has a candidate. Reordering either list is not, which is why the
  comparison is over sets: the candidate order is a different claim, held by
  `TestDocLockstepScopeResolutionIsFirstMatch`.
  This is why every kind names its tool in its own case, including the two
  answered by `get_code_relationship_story` — a case clause with no `tool =`
  in it is a finding, because a kind left to the default reads in a diff
  exactly like one somebody chose a tool for. Keep the `default` clause too;
  it is the fallback for a `Scope` no candidate produced, and without it
  `tool` would be the empty string for one. Both shapes are load-bearing for
  the scan: replacing the candidate slice or the switch with a table lookup
  means teaching the scanner that shape in the same PR, not deleting the test.
  What that test does **not** check is *which* tool a case names: point
  `resource` at `get_service_story` and it stays green.
  `TestDocLockstepPlannedCallToolPerScopeKind` (doc_lockstep_advisory_test.go)
  is what holds the mapping, and it drives a hand-written list of the six kinds
  — so a seventh kind's tool is pinned only once you add its row there as well.
  Add that row in the same PR.
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
  The class itself is pinned by
  `TestDocLockstepScopeIDCharacterClassIsExact` (doc_lockstep_gate_inputs_test.go),
  which sweeps every ASCII code point plus a few beyond it through `Evaluate`
  and compares the result against README.md's `[A-Za-z0-9._/:-]` sentence. One
  input per rejection kind could not do that: the character-class input in
  `documentedScopeRejections` carries a space as well as a `$`, so adding `$` to
  the allowed switch left that table green while `services/api/$SECRET.go` was
  published into `additionalContext`.
  `repoRelativePath`'s `strings.HasPrefix(rel, "..")` guard (claude.go) is a
  layer earlier, and this file used to call it the same shape. It is not.
  Dropping it lets an absolute `file_path` outside the payload's `cwd` become
  `../etc/passwd` in `Input.RepoPath`; `scopeSafe` refuses that, so nothing
  unsafe is published either way — but a non-empty unsafe `repo_path` stops
  scope resolution at the first candidate, so a request that also set `service`
  moves from advise on `service=checkout` to a `broad_scope` skip. Measured both
  ways, guard present and dropped. Two tests hold it:
  `TestDocLockstepRepoRelativePathRefusesEscapes` (doc_lockstep_advisory_test.go)
  at the function, where every branch is visible, and
  `TestDocLockstepEscapingToolPathDoesNotDisplaceALaterScope` in the same file
  through `Evaluate`. Keep both. The reusable lesson is in how the wrong claim
  was reached: the original probe set `repo_path` as the only scope field, which
  is the one shape where the guard is inert, and a probe that cannot tell inert
  from load-bearing is not a measurement.

## Failure modes and how to debug

- Symptom: `eshu assistant hook preflight --json` prints nothing when a
  narrow scope is passed → check `Input.Elapsed` first: `runAssistantHookPreflight`
  in the wrapper fills it from its own `time.Since(start)`, so a slow flag
  read or a slow `readClaudePreToolUseInput` stdin read can push `Elapsed`
  past `Budget` before `Evaluate` ever reaches scope resolution. This is a
  wrapper-side timing question, not a bug in this package's logic. Note that
  no test holds where that assignment sits: moving it above the stdin read
  takes the read out of the budget and stays green, so this paragraph is the
  only thing saying the read counts.
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
  `doc_lockstep_trigger_equivalence_test.go` compares `Evaluate`'s advise set
  against `triggerAllowed`'s accept set (with the sample it sweeps, and the pins
  on that sample, in `doc_lockstep_trigger_axes_test.go`, and the structural
  belts in `doc_lockstep_trigger_alias_test.go`),
  `doc_lockstep_trigger_path_test.go` pins the path the trigger takes to reach
  that switch (fixtures in `doc_lockstep_trigger_path_fixtures_test.go`),
  `doc_lockstep_publish_safety_test.go` pins the `scopeSafe`
  rejections, the Claude tool-to-class mapping, the excluded command families,
  and that the mapping's translations are all enumerated,
  `doc_lockstep_gate_inputs_test.go` pins the acceptance gates that are not the
  trigger — the single supported host, the exact scope-ID character class, and
  that exactly one place writes a `Decision` anything other than `decisionSkip`
  —
  `doc_lockstep_evaluate_switch_test.go` pins what each of `Evaluate`'s five
  clauses actually tests,
  `doc_lockstep_advisory_test.go` pins what an emitted advisory says — the
  truth labels, the disclaimer sentence, the MCP tool per scope kind, and
  `repoRelativePath`'s escape refusal —
  `doc_lockstep_scope_kind_test.go` pins the scope-kind membership under that
  tool-per-kind claim, comparing the kinds `scopeFromInput` offers against the
  kinds `plannedCallForScope` cases for, and
  `doc_lockstep_literal_test.go` counts the contract-name mentions in the
  package docs rather than checking each file has one, and checks README.md's
  per-file list still names every lockstep file on disk. Fix the doc or the code
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
  then permission, then scope, then freshness), **and what each clause tests**.
  A clause may test exactly the one condition it documents:
  `TestDocLockstepEvaluateClausesTestWhatTheyDocument`
  (doc_lockstep_evaluate_switch_test.go) compares all five against that list, so
  an extra conjunct is a finding. It has to be, because that is the one way left
  to widen acceptance without tripping anything else — `case !x.Enabled &&
  x.Tool != "Terminal":` keeps the clause count at five, keeps every clause
  returning a skip, writes no `Trigger` and adds no decision site, and it let
  the hook fire with no explicit enablement. Later checks assume earlier
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
