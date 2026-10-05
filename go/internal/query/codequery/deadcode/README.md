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
- **The suppressed bucket is bounded by the request limit.** Both the
  investigation and cross-repo scans cap it at `min(limit, 50)`
  (`suppressedBucketLimit`) and report `suppressed_limit` and
  `suppressed_truncated`, so `limit=1` cannot return dozens of modeled-root
  rows and a short bucket says whether it was cut (#7168). Policy stats still
  count every suppressed row; only the returned bucket is bounded.
- **Boundary consumer evidence is returned once.** A cross-repo candidate with
  no entity-level evidence classifies against the repository's incoming
  relationships (`consumers.Boundary`), but those items are the same for every
  such row, so `bucketCrossRepoDeadCodeResults` filters them once per request
  and the handler returns them as `boundary_consumer_evidence` (+ `_count`),
  empty and 0 when no row used the fallback. A
  fallback row keeps `consumer_evidence: []` and
  `consumer_evidence_source: "repository_boundary"`; other rows get `entity`;
  every row gets `consumer_evidence_count` (#7129). Classification still reads
  the in-memory evidence slice, never the row map.
- **`evidence_detail` is output projection after bucketing.**
  `cross_repo_evidence_detail.go` shapes the finished buckets: `full` (the HTTP
  default) leaves them alone, `handles` replaces each row's `consumer_evidence`
  with at most 5 group objects and caps the boundary list at 25, with counts,
  `*_truncated` markers and `truth.omissions` (#7129). `bucket_counts` and
  `analysis` are computed before shaping, so both modes agree.
- **`test_only_consumers` is a fact beside liveness, never an input to it
  (#7603).** A `live_by_consumer` row gets `test_only_consumers: true` when it has
  consumers and every consumer's root entity file is a test file by
  `codemodel.DeadCodeIsTestFile` (reused, never copied). The key is absent
  otherwise, including on dead, unknown, hidden-consumer, boundary-fallback,
  missing-root, incomplete-coverage and `consumer_repo_ids`-selector rows (the
  last two can hide a non-test consumer). `crossRepoDeadCodeConsumerRootPaths` reads the roots once per
  request through the optional `crossRepoDeadCodeRootPathStore`
  (`CrossRepoDeadCodeConsumerRootPathsQuery`: one keyed `content_entities` read,
  no `source_cache`); `bucketCrossRepoDeadCodeResults` only consumes the result.
  A test method is a reachability root only for C#, Java, Kotlin, Scala, Rust and
  Swift, so there a test-only consumer is flagged; in Go, Python, JavaScript and
  TypeScript a test function is never a root, so the flag appears only when
  another root, such as a main or script entry point, sits in a test path. The
  same-repository routes are deferred: a candidate with
  a strong caller never becomes a result row there.
- **Investigation coverage never reads `content_entities`.** The `coverage`
  block takes `file_count` and `languages` from the narrow
  `RepositoryContextCoverage` read and `content_last_indexed_at` from the files
  `max(indexed_at)` read (`investigation_coverage.go`); it carries no
  `entity_count` (#7525). Do not reintroduce full `RepositoryCoverage` there.
- **A reachability watermark never skips the legacy incoming read.**
  `DeadCodeIncomingEntityIDs` merges the materialized snapshot rows first, then
  always runs the producer-anchored one-hop read for the entities the snapshot
  did not answer (a hidden-consumer-only entry counts as unanswered). It does
  not consult `CodeReachabilityCoverage` (kept for the seam alias and the
  reachability-loader follow-up; no production caller reads it): a watermark with zero or few roots
  still reads "available, not truncated", so treating it as complete classified
  callees of non-root functions as unused (#7547). Do not reintroduce a skip
  without an `exact` language maturity plus a completeness proof.
- **Exports are caller-driven.** Every export exists because a staying
  caller names it (delegates, seam, grant proofs, staying tests); each
  carries a comment saying which. Do not export anything else.
- **Consumer coverage gates "dead" (#7547).** The cross-repo route classifies a
  symbol `dead` only when every consumer repository its answer is judged against
  has a complete reachability watermark (present, `truncated = false`, current
  verdict schema epoch) for its active generation, checked once per request, counting only repositories whose active
  generation has a `code_calls` or `inheritance_edges` edge intent (the ones that
  can be consumers; on a full generation a refresh intent is not an edge, #7591). An incomplete consumer makes a symbol
  with no strong live evidence `unknown_needs_evidence` with
  `consumer_coverage_incomplete`; the response's `consumer_coverage` object
  names the incomplete repositories, each with a `state` (`no_snapshot_yet`,
  `older_epoch`, `truncated`, `no_active_scope`), the `generation_id` and a
  `retryable` hint (a snapshot is expected without action; not a promise). Each
  entry also carries a plain-language `reason` and `next_step`, and
  `consumer_coverage` a `coverage_summary` sentence (#7594); all three are text
  derived in memory from `state` and whether the request named its own
  `consumer_repo_ids` (`cross_repo_consumer_coverage_text.go`), with no
  query. See `docs/public/reference/dead-code-reachability-spec.md`.
