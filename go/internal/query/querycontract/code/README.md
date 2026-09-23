# querycontract/code

Dead-code contract types, declared once for every package that helps answer
"is this symbol dead".

## Why the types live here and not with a caller

No single package owns a dead-code answer. The content readers in
`internal/query` fetch candidates, `internal/query/codequery/deadcode` scans
and filters them, `internal/query/impact` walks exposure paths, and
`internal/query/querytestutil` fakes the store for tests. None of them can
import the others without a cycle, so the shapes they pass between each other
have to sit below all of them.

This is a contract package: no behaviour beyond a trim, a lookup and a
presence check. Anything that decides policy belongs in the caller.

## What is here

| file | holds |
| --- | --- |
| `dead_candidates.go` | `DeadCodeCandidateLabels`, `DeadCodeCandidateEntityType`, `DeadCodeRootKindsFromMetadata`, `DeadCodeIncomingEdge` |
| `cross_repo_reads.go` | `CrossRepoDeadCodeConsumerReads`, `CrossRepoDeadCodeHiddenConsumers` |

`DeadCodeCandidateLabels` is the only declaration of the candidate set. The
OpenAPI contract test names this package directly, so the advertised
`candidate_kind` enum stays pinned to the same set the scan reads.

## Dependencies

Inbound: 22 files across `internal/query`, `codequery`, `codequery/deadcode`,
`codemodel`, `impact` and `querytestutil`.

Outbound: `strings`, and nothing else. Not the parent `querycontract`, not any
other Eshu package. That is why the move out of `querycontract` needed no
qualification of any symbol.

## Naming

The `DeadCode` prefix stays on the identifiers. See `doc.go` for why it is not
treated as package stutter.
