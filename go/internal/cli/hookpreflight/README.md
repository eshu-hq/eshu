# Hook Preflight

## Purpose

`hookpreflight` owns the business logic behind `eshu assistant hook
preflight`: classifying an opt-in, local, Claude Code-style PreToolUse
request into an advise-or-skip decision under the
`assistant_fast_path_hook.v1` contract
(docs/public/reference/assistant-fast-path-hooks.md). It decides whether a
narrow, share-safe Eshu scope exists for the trigger, and if so what bounded
MCP tool call to recommend next -- never running that call itself.

## Ownership boundary

This package owns preflight *logic*: normalizing the request, resolving a
safe scope, picking the recommended tool call, folding in an
already-decoded Claude PreToolUse payload, rendering the text output, and
building the Claude hook response value. It does not own process wiring:
reading cobra flags, reading stdin, decoding or encoding JSON, or mapping
the result to an exit code. Those stay in
`go/cmd/eshu/assistant_hook_preflight.go`, the cobra `RunE` wrapper, because
`go/cmd/eshu` is `package main` and nothing can import it. The wrapper
resolves process state and passes it into this package as plain values;
this package returns data, never printing anything itself except through
`RenderPreflightText`, which writes to a caller-supplied `io.Writer` rather
than to a process stream directly.

## Exported surface

- `Input` -- the request `Evaluate` classifies. It is a plain flag carrier
  with no `json` struct tags; nothing decodes into it
- `Output`, `Scope`, `PlannedCall`, `Truth` -- the full decision. These
  carry `json` struct tags naming the `assistant_fast_path_hook.v1` field
  names, but no production code marshals them: without `--json` the CLI
  renders `Output` as text, and with `--json` it encodes the narrower
  `ClaudePreToolUseOutput` instead. The one `json.Marshal(Output)` in the
  package is in `preflight_test.go`, checking that an unsafe scope ID never
  reaches the JSON form
- `Evaluate` -- classifies an `Input` into an `Output`; fails open (skip,
  never an error) rather than blocking the original host action
- `ClaudePreToolUseInput`, `ClaudePreToolUseOutput`,
  `ClaudePreToolUseSpecificOutput` -- the Claude Code PreToolUse hook JSON
  shapes, and the only ones anything serializes: the wrapper decodes stdin
  into the first and encodes the second on `--json`
- `MergeClaudePreToolUseInput` -- folds a decoded Claude payload into an
  `Input`: it always overwrites `Tool` with the payload's tool name, and
  fills `Trigger` and `RepoPath` only when the caller left them empty
- `ClaudePreToolUseOutputForPreflight` -- converts an advise `Output` into
  the Claude hook JSON response
- `RenderPreflightText` -- writes the plain-text decision report the CLI
  prints when `--json` is not set
- `DefaultBudget`, `FreshnessFresh`, `PermissionAllowed`, `DecisionAdvise`
  -- the constants the wrapper needs as flag defaults and to gate `--json`
  output to advise decisions only

See `doc.go` for the full godoc contract.

## Dependencies

None internal. The non-test files import only `fmt`, `io`, `path/filepath`,
`strings`, and `time`, so every package `go list -deps` resolves is standard
library. `os/exec`, `net/http`, and `encoding/json` are not among them: this
package runs no binary, opens no connection, and does no JSON encoding or
decoding outside its own tests (the wrapper decodes the stdin payload and
encodes the `--json` response; `preflight_test.go` marshals an `Output` to
check for a leaked path). The exact dependency count is deliberately not
quoted here -- it moves with the Go release, while the import set does not,
and `TestDocLockstepNonTestImports` pins the set.

`os` is pulled in transitively by `path/filepath`, but no production file
here calls it, and the four `path/filepath` functions those files do call --
`IsAbs`, `Rel`, `Clean`, `ToSlash` -- are pure string operations. So the
production files read no environment variable and touch no file. The lockstep
tests do both: they read the contract doc off disk and write fixture packages
under `t.TempDir()`. Like the `json.Marshal` above, that is test-only, and it
is what lets those tests fail on a real file the way a compiler-level overlay
could not.

Consumed by `go/cmd/eshu`: `assistant_hook_preflight.go` (the `hook
preflight` command) is the only production caller.
`assistant_hook_preflight_bench_test.go` also imports the package, to measure
`Evaluate` alongside the command wrapper.

## Telemetry

None. Preflight classification runs inline with the CLI invocation and
returns data; there is no background pipeline stage to instrument. See
docs/public/reference/assistant-fast-path-hooks.md's "No-Observability-Change"
note for the full rationale (no runtime start, no MCP/API/provider calls, no
graph/Postgres drivers).

## Gotchas / invariants

- `Evaluate` never returns an error. A fast-path hook that cannot safely
  advise must fail open (skip) so the original host tool call proceeds;
  turning an ineligible condition into an `error` would be a behavior
  change, not a classification.
- Scope resolution stops at the first non-empty candidate, in order:
  `repo_path`, `entity_id`, `service`, `workload`, `environment`,
  `resource`. If that candidate fails `scopeSafe` (an absolute path, a `~`
  prefix, a URL, `..`, a backslash, or a character outside
  `[A-Za-z0-9._/:-]`), `Evaluate` skips with `reasonBroadScope` rather than
  falling through to the next candidate -- an unsafe first match is not
  silently replaced by a safer later one.
- `Evaluate` checks `Input.Elapsed > Input.Budget` before anything else, so
  a caller that fills `Elapsed` from its own wall-clock start (as the
  wrapper does) gets a timeout skip even when every other condition would
  have advised.
- `MergeClaudePreToolUseInput` only fills `Trigger` and `RepoPath` from the
  Claude payload when the caller left them empty; an explicit `--trigger`
  or `--repo-path` flag always wins over the inferred value.
- The claims on this page are tested, not just written down. Sixteen lockstep
  files pin them, so a code change that makes one of the sentences above false
  fails a test rather than quietly aging into fiction. Each names what it
  covers, and the list is the boundary — a claim not in it is not pinned:
  - `doc_lockstep_test.go` — the struct tags and their wire names, the import
    set, the `assistant_fast_path_hook.v1` value `Evaluate` stamps, and the
    three reason codes the contract doc names
  - `doc_lockstep_behavior_test.go` — the reason-code precedence, the
    first-match scope ordering above, that no skip publishes a scope, the
    200 ms `DefaultBudget`, and the accepted trigger classes
  - `doc_lockstep_source_test.go` — the source declarations, so the
    json-tagged struct set is a complete inventory rather than a hand-written
    sample, and the only calls the production files make are
    `fmt.Fprintf`/`fmt.Sprintf` and the four pure `path/filepath` functions
    named above. That call-level pin is what backs this section's "touches no
    file" claim: `path/filepath` is on the import allow-list, so `Glob`,
    `WalkDir`, `Abs`, and `EvalSymlinks` would all clear the import check on
    their own
  - `doc_lockstep_switch_test.go` and `doc_lockstep_switch_fixtures_test.go` —
    `triggerAllowed` stays a closed string switch compared against its own
    parameter, so the accepted classes can be read out of it
  - `doc_lockstep_trigger_equivalence_test.go` — the whole-program claim: on a
    request that is eligible in every other respect, `Evaluate` advises for
    exactly the triggers `triggerAllowed` accepts. This is what "the trigger
    reaches that switch unrewritten" actually means, and it is asserted as a
    property rather than as a shape, so a remap fails it however it is spelled —
    within the sample it is checked over, which is the part to keep in mind: a
    remap can still evade it by reaching a class the sweep does not generate, or
    by keying on a request field the sweep holds fixed
  - `doc_lockstep_trigger_axes_test.go` — what that sample is: the alphabet the
    one-edit neighbourhood is built from, the request shapes the comparison is
    repeated on, and a literal pin on the two constants the exhaustive arm's
    coverage rests on, since the size check alone agrees with any value they take
  - `doc_lockstep_trigger_alias_test.go` — the belt under both: no production
    function takes the address of a `Trigger` field, writes through a
    dereferenced pointer, or builds a `Trigger`-carrying struct without naming
    its fields. Those are the ways of setting that field the writer scan below
    cannot see, since it reads `.Trigger` assignments and `Trigger:` literal
    keys — and every remap found so far that escaped the equivalence's sample
    was written in one of them. Treat that as a pattern worth re-checking rather
    than a closed list: a new way of reaching the field is a new rule here, not
    an argument that the rules are unnecessary
  - `doc_lockstep_trigger_path_test.go` and
    `doc_lockstep_trigger_path_fixtures_test.go` — the source half of the same
    claim: which functions may write a `Trigger` field and from what, and that
    `Evaluate`'s switch consults `triggerAllowed` itself on the value it
    normalized rather than a twin or a rewritten copy. It catches the case the
    equivalence test cannot see, a widening applied to both sides at once
  - `doc_lockstep_publish_safety_test.go` — one input per `scopeSafe`
    rejection kind listed above reaches `Evaluate` and publishes nothing, every
    Claude tool the contract doc excludes comes back as a skip, no tool name
    built from an excluded command family reaches a read class whatever the host
    calls it, and the tool-to-class translations are a complete list rather than
    a sample: any production string literal the mapping translates into a
    different class has to be one of the pinned names
  - `doc_lockstep_gate_inputs_test.go` — the acceptance gates that are not the
    trigger, each pinned as a property rather than as a list of values that fail
    today: `Evaluate` advises for exactly one host, for exactly the scope-ID
    characters the class above names (swept character by character), and it
    reaches `DecisionAdvise` in exactly one place. That last one is what closes
    a gate keyed on some *other* request field — `Tool`, `Workload`, a
    permission value nobody listed — which the trigger sweep cannot see, because
    it varies the trigger and holds the rest still
  - `doc_lockstep_advisory_test.go` — what an advisory says once it is emitted:
    the `advisory`/`local_preflight` truth labels in both the `Output` and the
    published string, the "advisory context only" disclaimer sentence, which
    MCP tool answers each scope kind, and `repoRelativePath` refusing a tool
    path that resolves outside the payload's `cwd` — at the function, and again
    through `Evaluate`, where an escaping path must leave a scope the caller did
    set standing
  - `doc_lockstep_scope_kind_test.go` — the membership under the
    tool-per-scope-kind claim above: the kinds `scopeFromInput` offers and the kinds
    `plannedCallForScope` names a case for are both read out of the source, and
    have to be the same set. A seventh kind added with no case of its own fails
    here instead of being quietly answered by the default tool, and so does
    deleting the case for a kind that still has a candidate. It compares
    membership only — see the bound below on which tool a case names
  - `doc_lockstep_evaluate_switch_test.go` — the condition each of `Evaluate`'s
    five eligibility clauses tests, pinned to the one thing `AGENTS.md` says it
    tests. This is the guard against acceptance widened *inside* an existing
    gate — `case !x.Enabled && x.Tool != "Terminal":` keeps five clauses, keeps
    every clause returning a skip, writes no `Trigger` and adds no decision
    site, so nothing else here can see it
  - `doc_lockstep_literal_test.go` — how many times each package doc quotes
    the contract name, so rewriting one of several mentions is not silent

  What this set does not prove. The general statement first, because it
  predicts the specifics rather than listing them after the fact: **the guards
  watch the trigger-to-decision function over a stated sample, a fixed set of
  write shapes, and a fixed set of decision sites. They do not watch what the
  existing gates test.** Concretely —

  - the sampled function is blind to triggers outside the sweep's alphabet and
    depth, and to request fields whose values are not among the axis variants;
  - the write inventory is blind to a `Trigger` written in a shape it does not
    enumerate;
  - the decision-site inventory is blind to a decision written under a different
    spelling, and to acceptance widened inside an existing clause's condition.

  The last two of those were live escapes found by reading that statement rather
  than by imagining mutants, and both are closed now — a raw `"advise"` string,
  and an extra conjunct on the `Enabled` clause. Read the statement as a map of
  where to look next, not as a list of what is broken.

  Each item below was mutated on a full disk copy of the package and left the
  whole suite green. It is what has been tried, not a proof that nothing else is
  missing:

  - The `~` and `\` rejections in `scopeSafe` overlap the character-class
    check, so deleting either changes no decision — the character class is what
    holds them, and it is now swept character by character rather than sampled.
    `repoRelativePath`'s `..` guard was described here as the same shape, on a
    probe that set `repo_path` as the sole scope field. That is the one request
    shape where it is inert. With a later scope field set it moves a decision,
    because scope resolution stops at the first non-empty candidate — so it is
    pinned through `Evaluate` as well as at the function.
  - The trigger-class comparison is against a transcription of the contract
    doc's "Trigger Classes" bullets, not against the class names themselves, so
    adding a class to both the code and that transcription with the doc
    untouched passes; updating the doc is a rule in `AGENTS.md`, not a gate.
  - Which MCP tool a *new* scope kind's case names. Candidate membership is
    pinned now — `TestDocLockstepEveryScopeKindNamesItsPlannedCall` requires
    every kind `scopeFromInput` offers to name a tool in a case of its own — but
    that the tool named is the right one for the kind is compared against a
    hand-written expectation only for the six kinds
    `TestDocLockstepPlannedCallToolPerScopeKind` lists. A seventh kind whose
    case named `get_service_story` left the whole suite green.
  - The positional-literal rule's package-qualifier arm (`pkg.Type{…}`) is
    implemented but has never been exercised, because it is unreachable in this
    package by construction: such a literal needs an import exposing a
    `Trigger`-carrying struct, and `docAllowedImports` permits only `fmt`, `io`,
    `path/filepath`, `strings`, and `time`, none of which has one — so
    `TestDocLockstepNonTestImports` fires first. Its irrelevance depends on that
    allow-list. Widen the allow-list and this arm becomes load-bearing and still
    unmeasured.
  - A `Trigger`-carrying literal nested two containers deep — `[][]Input{{{…}}}`
    — where the middle literal elides its type too. The positional-literal rule
    resolves an elided element from its immediate container, and there is no
    type on the middle one to resolve from. Unlike the single-level `[]Input{{…}}`
    case, which the production file's own `[]Scope{{Kind: …}}` shows is ordinary
    Go, nobody writes this by accident; it is listed because it is reachable,
    not because it is likely.
  - Where the wrapper sets `Input.Elapsed`. Moving `time.Since(start)` to
    before the stdin read stops that read counting toward the budget, and
    nothing goes red. `AGENTS.md` tells a debugger it counts; no test does.

## Related docs

- `docs/public/reference/assistant-fast-path-hooks.md` -- the
  `assistant_fast_path_hook.v1` contract, trigger classes, bounded query
  shape, latency budget, and safe failure modes this package implements
- `docs/public/reference/assistant-guidance.md` -- where `hook preflight`
  sits relative to `assistant install`/`status`/`uninstall`
