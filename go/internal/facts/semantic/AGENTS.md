# AGENTS.md - internal/facts/semantic guidance

## Read first

1. README.md - package boundary and registry role
2. doc.go - godoc contract for the semantic package
3. semantic.go - kinds, payload types, validation, stable ids
4. encode.go - SDK factschema encoding

## Invariants this package enforces

- Dependency direction stays one way: the parent `internal/facts` package
  and downstream consumers (semanticdocs, semanticcode, query, reducer,
  registry commands) may import this package, but this package must not
  import `internal/facts`. Its only `internal/facts/*` dependencies are
  `internal/facts/contract`, `internal/facts/docs`, and
  `internal/facts/encode`.
- Kind strings and payload shapes are frozen contracts shared with
  collectors, the schema registry, conformance fixtures, and the MCP
  marker registry (which matches the Go identifier spelling verbatim).
  Do not rename, re-derive, or reformat them in a refactor.
- Stable ids go through `encode.StableID` only, so they stay byte-identical
  with the root `facts.StableID` forwarder and the SDK encoders.
- A new versioned family registers in the parent `schema_version.go`
  table; an unregistered family is invisible to the classifier and drift
  guard.
- `contract.Declare` is the identity function the static generator reads.
  Keep the declaration as a plain package-level var so the generator and
  the fact-kind-registry verifier keep seeing it.
