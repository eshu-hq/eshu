# AGENTS.md — MCP admission-decisions route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch_admission_decisions.go` and
   `../dispatch_admission_decisions_contract_test.go` for the root adapter and
   the production-boundary proof.
5. `../dispatch_repositories.go` for the repository switch this family is
   answered ahead of.
6. `../tools_admission_decisions.go` for the advertised schema, which stays at
   root and must keep naming the same eight fields.
7. `../routecontract/README.md` for the dependency-neutral request contract.
8. `go/internal/query/admission_decisions.go` for the handler that reads the
   keys this package selects: the required-key check, the state vocabulary,
   the anchor-pair rule, and the 1-200 limit bound all live there.

## Invariants

- Keep only admission-decisions family membership and pure
  argument-to-request selection here. Global route fanout, the private adapter,
  and execution stay in the parent MCP package and `internal/query`.
- Keep the package clause as `package admissiondecisionstools`; the root
  imports it with an explicit alias.
- Preserve the tool name and its exact method, path, and query keys: `GET`
  `/api/v0/evidence/admission-decisions` with no body, carrying `anchor_id`,
  `anchor_kind`, `domain`, `generation_id`, `include_evidence`, `limit`,
  `scope_id`, and `state`, and nothing else.
- Keep every key present even when the caller omitted it, so the handler sees an
  empty filter rather than no filter key at all.
- Keep `include_evidence` as an explicit `"true"` or `"false"` built from a Go
  bool only. Parsing strings here would change what a stringified `"false"`
  means to the handler.
- Preserve the `limit` default of 50.
- Return the zero request and `handled=false` for unrelated tools, including
  near-miss names that share the `list_admission` prefix.
- Selection stays pure: no HTTP call, no query, no clock, no environment read.

## Common changes

- Change the route only with the exact child request tests, the root adapter
  parity test, the shared HTTP contract, and applicable golden-corpus proof.
- Change the `limit` default only with the handler's own bound check, because a
  mismatch silently changes a client-visible page size.
- Add a filter only after the handler reads it by name. The handler has no
  catch-all, so a new key sent from here is inert until
  `admissionDecisionFilterFromRequest` consumes it.
- Add a tool here only after confirming the root repository switch no longer
  answers it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract` only.
- Dropping one of the eight keys fails three different ways. `domain`,
  `scope_id`, and `generation_id` are required, so dropping any of them 400s
  every request with `domain, scope_id, and generation_id are required`.
  `anchor_kind` and `anchor_id` must arrive together, so dropping one half 400s
  with `anchor_kind and anchor_id must be provided together`, while dropping
  both returns 200 over every anchor in scope. `state`, `include_evidence`, and
  `limit` fail nothing: a lost `state` widens the page to every admission
  state, a lost `include_evidence` returns decisions with no evidence rows, and
  a lost `limit` serves the handler's own 50-row default in place of the
  caller's. The per-key child and dispatch assertions exist because the loud
  and silent shapes are invisible to a single request-level comparison.
- Sending `include_evidence` from a parsed string would make `"false"`,
  `"0"`, and an unparseable value diverge from today's behaviour, where every
  non-bool collapses to `false`.
- Executing a request here would split authorization, timeout, response-budget,
  envelope, summary, and telemetry ownership.
- Claiming a name the repository switch still answers makes resolution depend on
  which check runs first.
- Returning a shared map would let one caller's mutation leak into a later
  request.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, storage, authorization,
  summary, or telemetry helpers.
- Do not reintroduce the root `str`, `intOr`, and `boolOr` helpers; use
  `routecontract.Arguments`, whose coercions they match exactly.
- Do not widen `Route` to a prefix or regular-expression match. Family
  membership is an explicit list of names.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/admissiondecisions ./internal/mcp -count=1
go vet ./internal/mcp/admissiondecisions ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
