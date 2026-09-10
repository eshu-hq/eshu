# imports

Import-dependency investigation for the code family (`codequery`).

## What lives here

- `params.go` — the parameter map the Cypher builders send
  (`Params`), plus module-scope shaping (`UniqueScopes`,
  `ScopePaths`).
- `rows.go` — the row readers behind the investigation dispatch
  (`Rows`, `ImportRows`, `CycleRows`, `CrossModuleCalls`,
  `ModuleScopes`). Every read binds the request's scope and grant
  through `Params` before the `LIMIT`.

## What stays in `codequery`

`codequery/import_dependencies.go` holds the HTTP handler, the data
dispatcher, and the exported `ImportDependencyParams` forwarder (the
parent package's live grant proof calls it so the statement under
proof is the one the handler sends).
`codequery/import_dependencies_execution.go` holds the
`importDependencyRequest` alias and thin forwarders onto this leaf.
The request type, validation, Cypher builders, and response shaping
already live in `codemodel`.

## Invariants

- Four row readers carry queryplan source pins with QP-CODE-IMPORT
  entry ids (see `query-source-coverage.yaml`): keep the entry ids,
  the key bounds, and the result limits on any rewrite.
- Every Cypher literal here is built by `codemodel`, not by this
  leaf; Cypher shape changes need backend-differential proof
  (NornicDB vs Neo4j row-set equivalence), not just unit tests.
- This package never imports `codequery` or root `query`.
