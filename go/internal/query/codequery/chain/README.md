# chain

Call-chain traversal for the code family (`codequery`).

## What lives here

- `request.go` — the traversal request (decodes the route JSON
  unchanged), validation, and repository resolution: explicit
  start/end selectors in cross-repo mode, else the route repo.
- `cypher.go` — the Neo4j-compat `shortestPath` builder and the
  NornicDB dialect builder, plus the hop predicates. Both bind the
  request's own repository scope and the caller's grant before the
  `LIMIT`.
- `nodes.go` — node projection and endpoint candidacy shaping.

## What stays in `codequery`

`codequery/callers.go` holds the `*CodeHandler` methods (methods must
live in their type's package), the `callChainRequest` alias, response
shaping, and one forwarder (`callChainAllowedTraversalRepoIDs`) that
exists only because two grandfathered digests name the bare
identifier. The anchor label set lives here as
`AnchorLabelDisjunction`, single-sourced for the routes family.

## Invariants

- The two one-hop row readers are grandfathered: their bodies never
  change without a re-validation the ledger forbids — re-freezing is
  not a fix. New logic goes in this leaf behind the existing pins.
- The NornicDB builder does not parse on the pinned backend (measured,
  batch 2b); the live NornicDB path is the Go-side BFS. Do not make
  the builder reachable without re-proving the parse.
- This package never imports `codequery` or root `query`.
