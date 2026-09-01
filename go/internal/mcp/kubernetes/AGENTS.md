# AGENTS.md — MCP Kubernetes-correlation route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch_kubernetes.go` and `../dispatch_kubernetes_contract_test.go`
   for the root adapter and the production-boundary proof.
5. `../dispatch_repositories.go` for the repository switch this family is
   answered ahead of.
6. `../tools_kubernetes.go` for the advertised schema, which stays at root and
   must keep naming the same ten fields and the same 1..200 `limit` range.
7. `../routecontract/README.md` for the dependency-neutral request contract.
8. `go/internal/query/kubernetes.go` for the handler that reads the keys this
   package selects: the required `limit` and its bound, the anchor rule, and
   the access-scope short-circuit all live there. The bound is a rejection,
   not a clamp: `limit` outside 1..200 is a 400.
9. `go/internal/query/kubernetes_correlations.go` for the store, where
   `outcome` and `drift_kind` are equality filters and `after_correlation_id`
   is a `fact_id` keyset cursor.

## Invariants

- Keep only Kubernetes-correlation family membership and pure
  argument-to-request selection here. Global route fanout, the private adapter,
  and execution stay in the parent MCP package and `internal/query`.
- Keep the package clause as `package kubernetestools`; the root imports it
  with an explicit alias.
- Preserve the tool name and its exact method, path, and query keys: `GET`
  `/api/v0/kubernetes/correlations` with no body, carrying
  `after_correlation_id`, `cluster_id`, `drift_kind`, `image_ref`, `limit`,
  `namespace`, `outcome`, `scope_id`, `source_digest`, and
  `workload_object_id`, and nothing else.
- Keep every key present even when the caller omitted it, so the handler sees an
  empty filter rather than no filter key at all.
- Preserve the `limit` default of 50. The handler rejects an absent `limit`,
  so this default is the only reason an MCP caller who omits it gets a page.
- Return the zero request and `handled=false` for unrelated tools, including
  the sibling `*_correlations` listings and near-miss names that share the
  `list_kubernetes` prefix.
- Selection stays pure: no HTTP call, no query, no clock, no environment read.

## Common changes

- Change the route only with the exact child request tests, the root adapter
  parity test, the shared HTTP contract, and applicable golden-corpus proof.
- Change the `limit` default only with the handler's own bound check and the
  advertised schema, because a mismatch either changes a client-visible page
  size or turns an omitted `limit` into a 400.
- Add a filter only after the handler reads it by name. The handler has no
  catch-all, so a new key sent from here is inert until `listCorrelations`
  copies it into `KubernetesCorrelationFilter`.
- Add a tool here only after confirming the root repository switch no longer
  answers it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract` only.
- Dropping one of the ten keys fails four different ways. `limit` is required,
  so dropping it 400s every request with `limit is required`. The six anchors
  are required as a group, so dropping one 400s only the caller whose sole
  anchor it was, with `scope_id, cluster_id, workload_object_id, namespace,
  image_ref, or source_digest is required`, and silently widens every other
  caller's page. `outcome` and `drift_kind` fail nothing: a lost filter widens
  the page to every outcome or drift kind. `after_correlation_id` fails
  nothing either: a lost cursor restarts paging from the top, so a caller
  continuing a truncated page sees rows it already has. Dropping `drift_kind`
  from the builder fails three child tests and one root test; the per-key
  child and dispatch assertions exist because the loud and silent shapes are
  invisible to a single request-level comparison.
- Sending `limit` from a parsed string would make `"25"` diverge from today's
  behaviour, where every non-number collapses to the 50-row default.
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
go test ./internal/mcp/kubernetes ./internal/mcp -count=1
go vet ./internal/mcp/kubernetes ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
