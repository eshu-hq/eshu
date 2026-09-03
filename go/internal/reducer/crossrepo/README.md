# crossrepo

Resolves cross-repository relationships from persisted evidence facts and emits
the durable repo-dependency projection intents the shared projection later turns
into graph edges.

This package moved out of the flat `internal/reducer` root under issue #6061. It
is a domain family: it owns one handler and the pipeline behind it, and nothing
else in the reducer depends on its internals.

## What it owns

| piece | file | what it does |
|---|---|---|
| `CrossRepoRelationshipHandler` | `cross_repo_resolution.go` | the reducer handler the `deployment_mapping` domain registers; loads evidence and assertions, runs `relationships.Resolve`, persists the audit trail, emits intents, then activates the generation |
| `CrossRepoEvidenceSource` | `cross_repo_resolution.go` | the `evidence_source` this resolver stamps on every edge it writes |
| `EvidenceFactLoader`, `AssertionLoader`, `ResolutionPersister`, `RepoDependencyIntentWriter` | `cross_repo_resolution.go` | the four ports the handler is wired through |
| `ExtractRepoDependencyIntentRows` | `cross_repo_intent_row.go` | the resolved-relationship to intent-row conversion, exported for Ifá's materialized-edge vacuity guards |
| retraction rows | `cross_repo_resolution_retract.go` | the rows emitted for a source repo that resolved to no edges, so a stale edge is withdrawn rather than left standing |
| evidence artifacts | `cross_repo_evidence_artifacts.go` | the bounded `EvidenceArtifact` summaries an edge carries, including citation fields and the `GITHUB_ACTIONS_*` ref/pinned classification |
| evidence type and source tool | `cross_repo_evidence_type.go` | the pure evidence-kind to `evidence_type` / `source_tool` classifier |

`CrossRepoEvidenceSource` is exported on purpose. Workload materialization
writes the identical `(WorkloadInstance)-[:RUNS_ON]->(Platform)` shape under a
different `evidence_source`, so relationship type and endpoint labels cannot
tell the two families' edges apart. The Ifá assert gate reads this constant
rather than a copied literal, so its partition cannot drift from what the writer
stamps.

## Package boundary

Imports point strictly downward. This package reaches `reducer/contract`,
`reducer/gpphase`, `reducer/payloadcore`, `reducer/sharedintent`,
`internal/environment`, `internal/ghactionsref`, `internal/relationships`,
`internal/telemetry` and `pkg/log`, and it never imports the parent
`internal/reducer` package. The dependency runs the other way: the root keeps
compatibility aliases in `cross_repo_compat.go` for every name its own files,
`cmd/reducer`, `internal/ifa/materializededges` and `internal/storage/cypher`
still spell as `reducer.X` — seven of them today, and that file is the list.

Every blocker the move surfaced was of the first kind described in `AGENTS.md`
-- a root one-line forwarder or type alias to a leaf that had already moved out
-- so nothing was hoisted and no body was copied. `SharedProjectionIntentRow`,
`SharedProjectionIntentInput` and `BuildSharedProjectionIntent` resolve to
`sharedintent`; `GraphProjectionPhaseKey`, `GraphProjectionPhase`,
`GraphProjectionReadinessLookup`, `GraphProjectionReadinessPrefetch`,
`GraphProjectionPhaseBackwardEvidenceCommitted` and
`GraphProjectionKeyspaceCrossRepoEvidence` resolve to `gpphase`;
`toStringSlice` resolves to `payloadcore`; `DomainRepoDependency` resolves to
`reducer/contract`.

## Telemetry

`Resolve` emits four instruments, all registered in
`go/internal/telemetry/instruments.go`:

| instrument | when |
|---|---|
| `eshu_dp_cross_repo_resolution_duration_seconds` | once per generation, on each **successful** exit path -- an error return skips it, so the histogram counts completed resolutions only |
| `eshu_dp_cross_repo_evidence_loaded_total` | after the loaded evidence facts are deduped |
| `eshu_dp_cross_repo_edges_resolved_total` | once the resolved edges are counted |
| `eshu_dp_cross_repo_activation_fenced_total` | when durable acceptance intents fail to commit and generation activation is fenced |

`eshu_dp_cross_repo_activation_fenced_total` is the one to look at first when
resolved edges stop appearing in the graph while resolution itself looks
healthy: the edges resolved, the intents did not commit, and activation was
withheld on purpose.

Every `Instruments` access is nil-guarded, so a handler constructed without
telemetry resolves normally and reports nothing -- which is also why an empty
panel can mean "not wired" rather than "no work".

No-Regression Evidence: #6061 relocates this family's production logic without
changing it. Every hunk inside the moved production files is a package clause,
an import line, or a requalification of a name the reducer root supplied only as
a one-line forwarder or type alias -- the leaf that already owned each name is
now imported directly (see the Package boundary section for the full list). No
declaration changed body, order, or signature, and a Go import change adds no
indirection at runtime. Measured against baseline `origin/main` at `cec2781bb` (this branch's merge-base):
`go build ./...` and `go vet ./...` both exit 0 on the branch, and `go test
./internal/reducer/... -count=1` exits 0 with 22 packages ok and 2 carrying no test files, including this
one. Moved test files additionally gained requalified `gpphase` spellings, the
reducer root gained its own copies of two test doubles and one payload accessor
the moved test files defined (Go test files cannot share unexported symbols
across a package boundary), and `cross_repo_source_tool_snapshot_test.go`'s
golden-snapshot path gained one more parent hop after moving a directory deeper
-- a real fix to a path the move broke, not a production change. Binary output
was not compared and no such claim is made here.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span, or
log field. The four instruments above and the spans and log lines around them
are the same before and after the move; only the file paths their
telemetry-coverage rows cite changed.

## Gotchas / invariants

- **Do not import the reducer root from here.** If this package needs a symbol
  the root defines, the symbol is in the wrong place: hoist it to a shared-core
  tier (`payloadcore` for generic helpers, `contract` for vocabulary) and leave
  a root alias, rather than reaching upward.
- **A retraction is a row, not a silence.** A source repo that resolves to no
  edges emits retraction intent rows; dropping that path would leave a stale
  edge standing rather than withdrawing it.
- **`""` and `unknown` are different source-tool answers.**
  `sourceToolForEvidenceKind` returns `""` for a kind it cannot map, and
  `resolvedRelationshipSourceTool` turns that into the explicit
  `sourceToolUnknown` token only when a primary evidence kind was present at
  all; an edge with no evidence kind yields `""` and is not stamped. Collapsing
  the two loses the difference between "unclassified tool" and "no evidence".
  `scripts/verify-edge-source-tool-coverage.sh` reads
  `cross_repo_evidence_type.go` directly to prove every evidence kind is
  classified, so adding an evidence kind fires that gate; its path is pinned in
  `specs/ci-gates.v1.yaml`.
- **`evidence_type` and `source_tool` derive from the FIRST evidence kind in
  the preview**, so preview ordering is part of the contract, not a display
  detail.
- **Activation is fenced, not skipped, on an intent-commit failure.** The
  handler returns without activating the generation so downstream consumers
  never read a half-committed resolution; the fenced counter is the only signal
  that this happened.

## Related docs

- [Reducer package](../README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
- [Edge source-tool provenance](../../../../docs/public/reference/edge-source-tool-provenance.md)
