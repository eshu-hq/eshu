# Dead-Code Analysis (`go/internal/query/codequery/deadcode`)

Dead-code analysis for the code-family queries: candidate scanning,
cross-repo consumer evidence, investigation packets, downgraded-root
verdicts, and the default reachability policy.

It split out of package `codequery` (#6060 lane A) because `codequery`
hit the 40-file dirgate cap: the analysis is the most separable
sub-domain (no queryplan-pinned builders, coherent future repo
boundary), and the split keeps every digest value frozen. Two row
readers stay behind in `codequery` -- `deadCodeCandidateRows` and
`deadCodeResultsWithGraphIncomingEdges` keep their
`(*CodeHandler)` receivers and byte-identical bodies with their
`source_sha256` pins (see
`go/internal/queryplan/testdata/query-source-coverage.yaml`).

## How it connects

- `codequery` delegates to a per-call `Analyzer` (`NewAnalyzer` +
  `Dependencies` in `analyzer.go`; the thin `*CodeHandler` delegates
  live there too). The `Mount` route table
  and staying tests still name the handler methods, so behavior is
  unchanged.
- `Dependencies` carries value fields (`Content`, `Graph`, `Profile`)
  plus func fields for staying `codequery` helpers that cannot move
  here (pinned readers, HTTP writers, grant filters, selectors). When
  one of those moves to a leaf, repoint the field and drop the
  `codequery` reference.
- Leaf aliases (`ContentStore`, `GraphQuery`, `TruthEnvelope`, ...)
  point at `querycontract`/`contentread` directly, the same convention
  `codequery`'s hub file uses.
- Root (`code_seam.go`, content readers) and grant tests name the
  exported surface (`CrossRepoDeadCodeEvidence`,
  `MergeStrongestDeadCodeIncomingEdge`, ...); exported names stay
  identical across the split so the seam does not move.

## Contracts (must hold)

- **Never import `codequery` or root package `query`.** Both import
  this package (delegates, seam); the reverse is an import cycle.
  Qualify to the leaves instead.
- **Relocated, not rewritten.** Moved logic keeps its behavior;
  handler methods became `Analyzer` methods with identical bodies
  apart from the receiver and dependency spellings. Never re-freeze a
  `cypher_sha256`; never invent a `source_sha256`.
- **Exports are caller-driven.** Every export exists because a staying
  caller names it (delegates, seam, grant proofs, staying tests); each
  carries a comment saying which. Do not export anything else.
