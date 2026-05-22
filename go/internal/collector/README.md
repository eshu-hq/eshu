# internal/collector

## Purpose

`internal/collector` owns source observation for repository indexing: source
selection, git/native snapshots, discovery filtering, parser input shaping,
content entity snapshots, fact streaming, and claim-aware collector service
execution.

## Ownership boundary

Collectors produce durable fact input. They do not decide canonical graph
truth, serve query routes, or write graph rows. Projector and reducer packages
consume the committed facts and decide graph/read truth.

Raw Terraform-state bytes are not part of normal repository snapshots. This
package emits metadata-only state candidates; `internal/collector/terraformstate`
owns approved state reads and redaction.

## Exported surface

See `doc.go` for the package contract. Main surfaces include `GitSource`,
`RepositorySelector`, `RepositorySnapshotter`, `Service`, `ClaimedService`,
`Committer`, `ClaimedCommitter`, repository selection config, discovery
advisory rows, priority selection, webhook trigger selection, and snapshot
parser/SCIP config.

## Dependencies

The package depends on parser, content shaping, facts, discovery, workflow
claim types, and telemetry. Storage, graph projection, reducer, and query
packages remain downstream boundaries.

## Telemetry

Collector paths use `SpanCollectorObserve`, `SpanCollectorStream`,
`SpanScopeAssign`, `SpanFactEmit`, `SpanTerraformStateFactEmitBatch`, parse and
stream metrics, skip counters, claim wait duration, and structured failure
classes for discovery, streaming, parsing, emission, and commit failures.

High-cardinality repository and path details belong in logs or spans, not
metric labels.

## Gotchas / invariants

- Repository selection must stay bounded by source mode, rules, limits, and
  auth configuration.
- Snapshot, parse, and stream worker counts are performance knobs, not
  correctness fixes.
- Discovery skips and partial snapshots are expected inputs; callers must
  handle them explicitly.
- Claim-aware commits must preserve fencing tokens so stale collectors cannot
  overwrite newer generations.
- Parser output shape changes must move content shaping, fact contracts,
  reducer/projector expectations, fixtures, and docs together.

## Focused tests

```bash
cd go
go test ./internal/collector -run 'Test.*Selection|Test.*Snapshot|Test.*Claim|Test.*Service|Test.*Discovery|Test.*TerraformState' -count=1
go test ./internal/collector -count=1
```

Docs-only edits should also pass the package-doc verifier and `git diff --check`.

## Related docs

- `docs/public/architecture.md`
- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
- `go/internal/parser/README.md`
- `go/internal/collector/terraformstate/README.md`
