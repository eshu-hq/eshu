# ask/sandbox — Agent Instructions

Package path: `go/internal/ask/sandbox`
Epic: #3250 (Ask Eshu Tier 2)
Issue: #3261

## What This Package Does

Tier 2 read-only query sandbox for the Ask Eshu feature. It authorizes and
(when enabled) executes Cypher and SQL queries through three ordered security
layers: normalize → validate → guarded read-only-tx exec.

## Critical Constraints

- **DEFAULT-OFF.** The Guard is always constructed with `enabled=false` until
  the security review tracked in issues #1755, #1900, and #1902 is completed.
  Do NOT set `enabled=true` in any call path that is not explicitly reviewed.

- **DEFAULT-DENY.** Any uncertain or unclassified input MUST result in a deny
  Decision. Never add a fallthrough to allow.

- **No query echoing in reasons.** `Decision.Reason` must always be a bounded
  fixed string or a fixed prefix plus a single keyword. It must NEVER include
  the query body, schema names, or user-supplied input beyond a single token.

- **No execution before validation.** The Guard enforces the invariant that
  `Executor.Exec` is never called for a denied or disabled state. Any
  `Executor` implementation must document but NOT re-enforce this — the Guard
  is the single enforcement point.

## Skills Required

- `golang-engineering` — for all Go edits and tests
- `concurrency-deadlock-rigor` — if touching Executor lifecycle, db pool, or ctx cancellation
- `eshu-diagnostic-rigor` — if adding telemetry or measuring performance
- `eshu-mcp-call-rigor` — if wiring this package to the MCP or API layer

## Verification Gates

Run from `/tmp/eshu-wt/sandbox/go` (or the active worktree's `go/`):

```bash
gofmt -l ./internal/ask/sandbox         # must print nothing
go vet ./internal/ask/sandbox           # must print nothing
golangci-lint run ./internal/ask/sandbox/...  # must report 0 issues
go test ./internal/ask/sandbox -count=1 # must pass
```

## File Map

| File              | Purpose                                              |
|-------------------|------------------------------------------------------|
| `sandbox.go`      | Dialect, Decision, Caps, DefaultCaps, ErrSandboxDisabled |
| `normalize.go`    | Adversary-resistant byte scanner; comment/string masker |
| `cypher.go`       | validateCypher; cypherDenylist; tokenizeMasked       |
| `sql.go`          | validateSQL; sqlDenylist                             |
| `guard.go`        | Validate dispatcher; Executor interface; Guard       |
| `pgexec.go`       | NewPostgresReadOnlyExecutor; read-only-tx adapter    |
| `doc.go`          | Package-level godoc                                  |
| `README.md`       | Human architecture + security design doc             |
| `AGENTS.md`       | This file (agent instructions)                       |

## Non-Goals (v1)

**Tenant scope-predicate injection** is NOT solved in this package — it is the
open design question tracked by the Tier-2 enablement gate (issue #3302). Do not
add scope-injection logic here without that design.

**Query cost gating** now lives in this package as a sandbox-execution-layer
defense-in-depth control (the cost gate runs `EXPLAIN (FORMAT JSON)` in the SAME
read-only transaction as execution, so the validated plan matches what runs).
This supersedes the earlier note that cost gating was wired by the API layer
(#3263); see the design decision in `docs/internal/design/3302-ask-eshu-tier2-enablement.md`.
Co-locating it with the read-only-tx executor is required for plan/exec
consistency (RLS `SET LOCAL`, `search_path`, statement timeouts).

## Denylist Modification Policy

Before adding or removing entries from `cypherDenylist` or `sqlDenylist`:

1. Read the existing denylist comments — each entry documents WHY it is there.
2. Write a test that demonstrates the attack vector being closed (or opened).
3. Verify whole-word matching: `tokenizeMasked` splits on non-identifier bytes,
   so `update_time` produces token `UPDATE_TIME`, not `UPDATE`. Confirm the new
   token does or does not collide with legitimate identifiers.
4. Document the change with the same level of detail as existing entries.

## Adding a New Dialect

1. Add a `Dialect` constant to `sandbox.go`.
2. Add a `validateXxx` function in a new `xxx.go` file.
3. Add the dialect to the `switch` in `Validate` (guard.go).
4. Add an `Executor` implementation or extend `postgresReadOnlyExecutor`.
5. Add test coverage for: allowed query, denied query, stacked statements,
   comment bypass attempts, string-literal bypass attempts.
6. Update `doc.go`, `README.md`, and this file.
