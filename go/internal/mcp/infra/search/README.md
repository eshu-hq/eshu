# MCP infrastructure-search route selection

## Purpose

This package owns family membership and pure internal-request selection for the
MCP infrastructure-search tool: the bounded search over cloud, Kubernetes,
Terraform, ArgoCD, Crossplane, and Helm resource nodes by free text or by
structured filters such as kind, provider, environment, and resource category.

## Ownership boundary

This package owns infrastructure-search family membership and the mapping from
decoded arguments to a dependency-neutral internal request. `internal/mcp` keeps
tool registration and its client-visible order, global route fanout, the private
adapter, HTTP dispatch, authorization, timeouts, response budgets, envelopes,
summaries, and telemetry. `internal/mcp/ecosystem` keeps the advertised
definition. `internal/query` owns the bounded read this path reaches, including
the scope rule, the category vocabulary, the asymmetric `limit` bound, the
capability check that makes the search a 501 below the production profile, and
the access-scope short-circuit for a caller with no grant.

## Exported surface

- `Route` selects the internal request for an infrastructure-search tool
  without executing it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument and
  internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP package
keeps transport and dispatch signals, while the HTTP handler retains the shared
API request duration and error metrics, its own
`SpanQueryInfraResourceSearch` span, and the scope-grant inline-cap counter it
records once per read.

## Gotchas / invariants

- The import path ends in `infrasearch`, while the declared package is
  `infrasearchtools`. The root uses an explicit import alias.
- The request is a `POST` with a JSON body and no query string. The body
  carries exactly eight keys: `query`, `category`, `kind`, `provider`,
  `environment`, `resource_service`, `resource_category`, and `limit`. The
  handler decodes each into a named struct field with no catch-all, and a
  dropped key fails in one of two ways. The seven scope keys are required as a
  group, so losing one returns 400 only for the caller whose sole scope it
  was, with `query or structured filter is required`, and silently widens
  every other caller's page past the filter they named. `limit` fails
  nothing: losing it hands every caller the handler's 50-row substitute.
- Every key is sent even when the caller omitted it, so the handler sees an
  explicitly empty filter rather than no field at all. The handler trims each
  value, so an all-whitespace filter counts as absent there.
- `limit` defaults to 50 and travels as a Go `int`, not a string. The
  handler's bound is asymmetric and never rejects: any value at or below zero
  becomes 50 and any value above 200 becomes 200, so a caller passing 0 or -1
  gets a 50-row page and a caller passing 500 gets 200 rows. The advertised
  schema names a 1..200 range with a default of 50, which is the range a
  caller should stay inside, not the range the handler enforces. Do not
  describe this as a 1..200 bound with a lower clamp of 1.
- `category`, when non-blank, must be one of `k8s`, `terraform`, `argocd`,
  `crossplane`, `helm`, or `cloud` after lowercasing, or the handler returns
  400 with `unsupported category`. The advertised schema enumerates the same
  six values.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`, and
  `float64` are accepted, a `float64` truncates toward zero, and every other
  type falls back to the default. A stringified `"25"` therefore becomes a
  50-row page rather than an error.
- The search has no cursor and no offset. The handler fetches `limit + 1` rows
  and reports `truncated` when more exist; a caller narrows the filters rather
  than paging.
- `Route` returns a fresh body map per call, so a caller may mutate one result
  without changing a later one or the arguments it was given.

No-Observability-Change: this extraction moves only pure infrastructure-search
route selection. The root adapter still feeds the same global fanout, dispatch,
authorization, budgets, envelopes, summaries, and transport telemetry, and the
same query handler executes the request.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [MCP ecosystem registration](../ecosystem/README.md)
- [Infra resource search anchoring evidence](../../../../docs/internal/evidence/5271-infra-resource-search-anchoring.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
