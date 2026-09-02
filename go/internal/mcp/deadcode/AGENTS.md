# AGENTS.md — MCP dead-code route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch.go` for `resolveRoute` and the private `deadCodeRoute`
   adapter, consulted as a delegation ahead of the switch that held the three
   arms before the extraction, and
   `../dispatch_dead_code_contract_test.go` for the production-boundary
   proof.
5. `../tools_codebase.go`, `../tools_dead_code.go`, and
   `../tools_cross_repo_dead_code.go` for the three advertised schemas. They
   stay at the parent's root and must keep naming the same fields this
   builder selects.
6. `../routecontract/README.md` for the dependency-neutral request contract.
7. `go/internal/query/code_dead_code.go`,
   `go/internal/query/code_dead_code_investigation.go`, and
   `go/internal/query/code_dead_code_cross_repo.go` for the handlers behind
   the three paths: each substitutes 100 for a nonpositive `limit` and clamps
   above 500, the investigation caps `offset` at 2000, and only the
   cross-repo handler rejects a blank `repo_id`.

## Invariants

- Keep only dead-code family membership and pure argument-to-request
  selection here. Global route fanout, the private adapter, and execution
  stay in the parent MCP package and `internal/query`.
- Keep the package clause as `package deadcodetools`; the root imports it
  with an explicit alias.
- Preserve each tool's exact method, path, and body keys. All three requests
  are `POST` under `/api/v0/code/dead-code` with no query string.
- Keep every selected string key present even when the caller omitted it, so
  the handler sees an empty filter rather than no field at all.
- Preserve the dispatcher-side defaults the tests pin: `limit` 100 and
  `offset` 0.
- Preserve the two list arguments' opposite absent shapes:
  `exclude_decorated_with` is nil (JSON `null`) when absent and a non-nil
  empty `[]any` (JSON `[]`) when sent empty; `consumer_repo_ids` is always a
  non-nil `[]string` with empty-string and non-string members dropped. Both
  are inherited wire shape.
- Return the zero request and `handled=false` for unrelated tools, including
  `find_dead_iac`, which shares the `find_dead_` spelling, and
  `analyze_code_relationships`, whose `dead_code` query type selects the same
  scan path from the relationships child.
- Selection stays pure: no HTTP call, no query, no clock, no environment
  read.

## Common changes

- Change a route only with the exact child request tests, the root adapter
  parity test, the shared HTTP contract, and applicable golden-corpus proof.
- Change a default only against the handler's own behavior and the advertised
  schema, because a mismatch changes a client-visible page size.
- Add a body key only after the handler decodes it by name; a key the handler
  does not read is inert.
- Add a tool here only after confirming the root `resolveRoute` does not also
  answer it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract`
  only.
- A dropped `repo_id` fails loudly only on the cross-repo route; on the scan
  and investigate routes it silently widens the result to every repository
  the caller's scope grants. A dropped `language` silently widens the page. A
  dropped `limit` never fails at all: the handler substitutes its own 100.
  The per-key assertions in both test files exist because a request-level
  comparison alone hides which key was lost.
- Turning `consumer_repo_ids` nil, or `exclude_decorated_with` always-empty,
  changes bytes on the wire (`[]` versus `null`) that a length comparison
  cannot see; the nil-ness assertions exist for exactly that regression.
- Claiming a name the root also answers makes resolution depend on which
  check runs first.
- Executing a request here would split authorization, timeout,
  response-budget, envelope, summary, and telemetry ownership.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, storage, authorization,
  summary, or telemetry helpers.
- Do not reintroduce the root `str`, `intOr`, or `stringSlice` helpers; use
  `routecontract.Arguments`, whose coercions they match exactly. The local
  `stringValues` exists only because `routecontract` has no `[]string`
  narrowing, and it must keep the root helper's drop-empty, never-nil
  semantics.
- Do not widen `Route` to a prefix or regular-expression match. Family
  membership is an explicit list of names.

## Changes needing ADR review

- Moving registration, dispatch, authorization, or execution into this
  package.
- Changing a tool name, path, body key, or default in a way clients can
  observe, including the `null`-versus-`[]` absent shapes.
- Replacing the explicit name switch with derived membership.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/deadcode ./internal/mcp -count=1
go vet ./internal/mcp/deadcode ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
