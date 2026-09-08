# Agent instructions: entitysemantics

Read `doc.go` and `README.md` first, then the parent `../AGENTS.md` for
query-wide invariants.

## Invariants

- This package renders prose and shapes semantic metadata. It must not gain a
  handler, a graph read, a SQL read, or a Cypher literal. If a change needs
  one, it belongs in a query-owning package, not here.
- It may import `querycontract` and the standard library, nothing else inside
  `internal/query`. It must never import the root `query` package: root
  imports this package, so a back-import is an import cycle (#6060).
- Root keeps lowercase forwarders for these symbols. Keep them delegating —
  a second implementation at root would drift silently, because both would
  compile and only one would be exercised by this package's tests.

## Why this is not querycontract

`querycontract/AGENTS.md` bars family-specific response models. Prose that
decides how one family's answers read is one. That rule is why this package
exists; do not move code back across that line without re-reading it.

## Verification

From `go/`: `go test ./internal/query/... -count=1`. The summary tests are
table-driven over entity kinds and languages — confirm a real case count ran
rather than a suite that matched zero.
