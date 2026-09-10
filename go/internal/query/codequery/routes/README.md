# routes

Route-to-caller tracing for the code family (`codequery`).

## What lives here

- `entry.go` — the trace request (decodes the route JSON unchanged),
  validation, scope check, and route selection: the endpoint/handler
  row join picks one route, preferring the row that carries a handler.
- `graph.go` — the endpoint/handler row join, the directional CALLS
  traversal around the resolved handler label, and the code-entity
  label gate. Every read binds the request's own selectors and the
  caller's grant before the `LIMIT`.
- `impact.go` — caller/callee splitting, impact-set merging, and the
  scoped-access predicates and params shared by the reads.

## What stays in `codequery`

`codequery/route_handlers.go` holds the HTTP handler, the impact assembler,
the `*CodeHandler` graph-read methods (methods must live in their
type's package), the request/route aliases, and thin forwarders onto
this leaf. Four of those methods are grandfathered (see Invariants);
the label gate lives here as `LabelAllowed`, single-sourced from
`chain.AnchorLabelDisjunction`.

## Invariants

- Four graph-read methods are grandfathered: their bodies never
  change without a re-validation the ledger forbids — re-freezing is
  not a fix. New logic goes in this leaf behind the existing pins.
- Every Cypher literal here moved verbatim from `codequery`; only the
  surrounding Go changed (explicit graph, context, and grant
  parameters instead of the handler receiver). Cypher shape changes
  need backend-differential proof (NornicDB vs Neo4j row-set
  equivalence), not just unit tests.
- This package never imports `codequery` or root `query`.
