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
- The claims on this page are tested, not just written down. Four lockstep
  files pin them, and a code change that makes one of the sentences above
  false fails a test rather than quietly aging into fiction:
  `doc_lockstep_test.go` pins the struct tags and their wire names, the
  import set, the `assistant_fast_path_hook.v1` literal, and the three reason
  codes the contract doc names; `doc_lockstep_behavior_test.go` pins the
  reason-code precedence, the first-match scope ordering above, that no skip
  publishes a scope, the 200 ms `DefaultBudget`, and the trigger classes;
  `doc_lockstep_source_test.go` reads the source declarations, so the
  json-tagged struct set is a complete inventory rather than a hand-written
  sample, and the only calls the production files make are
  `fmt.Fprintf`/`fmt.Sprintf` and the four pure `path/filepath` functions
  named above; `doc_lockstep_switch_test.go` requires `triggerAllowed` to
  stay a closed string switch, so the accepted classes can be read out of it.
  The call-level pin is what backs this section's "touches no file" claim:
  `path/filepath` is on the import allow-list, so `Glob`, `WalkDir`, `Abs`,
  and `EvalSymlinks` would all clear the import check on their own.

## Related docs

- `docs/public/reference/assistant-fast-path-hooks.md` -- the
  `assistant_fast_path_hook.v1` contract, trigger classes, bounded query
  shape, latency budget, and safe failure modes this package implements
- `docs/public/reference/assistant-guidance.md` -- where `hook preflight`
  sits relative to `assistant install`/`status`/`uninstall`
