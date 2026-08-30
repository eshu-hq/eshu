# AGENTS.md — MCP secrets/IAM route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch_secrets_iam.go` and `../dispatch_secrets_iam_contract_test.go`
   for the root adapter and the production-boundary proof.
5. `../dispatch_repositories.go` for the repository switch this family is
   answered ahead of.
6. `../routecontract/README.md` for the dependency-neutral request contract.

## Invariants

- Keep only secrets/IAM family membership and pure argument-to-request
  selection here. Global route fanout, the private adapter, and execution stay
  in the parent MCP package and `internal/query`.
- Keep the package clause as `package secretsiamtools`; the root imports it with
  an explicit alias.
- Preserve the five tool names and their exact method, path, and query keys:
  `/api/v0/secrets-iam/identity-trust-chains`,
  `.../privilege-posture-observations`, `.../secret-access-paths`,
  `.../posture-gaps`, and `.../posture-summary`, all `GET` with no body.
- Preserve the `limit` default of 50 on each of the four listings.
- Preserve the summary's austerity. `count_secrets_iam_posture` carries
  `scope_id` alone. Do not give it a limit, a cursor, or a filter for symmetry
  with its four siblings. `h.summary` reads only `scope_id` and calls
  `SummarizeSecretsIAMPosture(ctx, scopeID)`; it never reads a limit, so one
  sent there would be inert. It would advertise a bound the endpoint does not
  honor, which is worse than omitting it.
- Keep `scope_id` present on every route even when the caller omitted it, so the
  handler sees an empty anchor rather than no anchor key at all.
- Return the zero request and `handled=false` for unrelated tools, including
  near-miss names that share the `list_secrets_iam` prefix.
- Selection stays pure: no HTTP call, no query, no clock, no environment read.

## Common changes

- Change a route only with the exact child request tests, the root adapter
  parity test, the shared HTTP contract, and applicable golden-corpus proof.
- Change a `limit` default only with the handler's own bound check, because a
  mismatch silently changes a client-visible page size.
- Change a cursor key only with the query layer's keyset predicate. The four
  listings each seek by their own key; they are not interchangeable, and paging
  is keyset-only with no `offset`.
- Add a tool here only after confirming the root repository switch no longer
  answers it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract` only.
- Giving `count_secrets_iam_posture` a `limit` compiles and reads like a
  consistency fix. It truncates nothing: `SecretsIAMHandler.summary` reads only
  `scope_id` and never a limit, so one sent there is inert and only advertises a
  bound the endpoint does not honor. The child and root summary tests exist to
  catch exactly that edit.
- Dropping `scope_id` from any route turns a scoped read into an unscoped
  aggregate at the handler.
- Executing a request here would split authorization, timeout, response-budget,
  envelope, summary, and telemetry ownership.
- Claiming a name the repository switch still answers makes resolution depend on
  which check runs first.
- Returning a shared map would let one caller's mutation leak into a later
  request.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, storage, authorization,
  summary, or telemetry helpers.
- Do not reintroduce the root `str` and `intOr` helpers; use
  `routecontract.Arguments`, whose coercions they match exactly.
- Do not widen `Route` to a prefix or regular-expression match. Family
  membership is an explicit list of names.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/secretsiam ./internal/mcp -count=1
go vet ./internal/mcp/secretsiam ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
