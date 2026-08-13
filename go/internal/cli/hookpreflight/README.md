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
safe scope, picking the recommended tool call, merging Claude's PreToolUse
JSON, and rendering both the text and Claude-hook-JSON output shapes. It
does not own process wiring: reading cobra flags, reading stdin, or mapping
the result to an exit code. Those stay in
`go/cmd/eshu/assistant_hook_preflight.go`, the cobra `RunE` wrapper, because
`go/cmd/eshu` is `package main` and nothing can import it. The wrapper
resolves process state and passes it into this package as plain values;
this package returns data, never printing anything itself except through
`RenderPreflightText`, which writes to a caller-supplied `io.Writer` rather
than to a process stream directly.

## Exported surface

- `Input`, `Output`, `Scope`, `PlannedCall`, `Truth` -- the request and the
  full decision, including the JSON tags the CLI's non-`--json` mode
  serializes
- `Evaluate` -- classifies an `Input` into an `Output`; fails open (skip,
  never an error) rather than blocking the original host action
- `ClaudePreToolUseInput`, `ClaudePreToolUseOutput`,
  `ClaudePreToolUseSpecificOutput` -- the Claude Code PreToolUse hook JSON
  shapes read from stdin and emitted on `--json`
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

None internal. The package imports only the standard library
(`encoding/json` is not imported here -- JSON encoding of `Output` happens
in the wrapper via `encoding/json.Encoder`; this package only carries the
`json` struct tags).

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
  `resource`. If that candidate fails `scopeSafe` (an absolute path, a
  URL, `..`, a backslash, or a character outside
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

## Related docs

- `docs/public/reference/assistant-fast-path-hooks.md` -- the
  `assistant_fast_path_hook.v1` contract, trigger classes, bounded query
  shape, latency budget, and safe failure modes this package implements
- `docs/public/reference/assistant-guidance.md` -- where `hook preflight`
  sits relative to `assistant install`/`status`/`uninstall`
