# #6693 checklist step 6: `incident/` (1 file)

Baseline: `origin/main` `8fd319051` (rebased after #7026). Change: move
`incident_repository_correlation_loader.go` (and
its test) to the new non-root package
`go/internal/storage/postgres/incident` (package clause `incidentstore`), no
existing package of that name (`rg -n '^package incidentstore$' go/ --glob
'*.go' -l` found none).

## What moved

- `incident_repository_correlation_loader.go` ->
  `incident/repository_correlation_loader.go`.
- `incident_repository_correlation_loader_test.go` ->
  `incident/repository_correlation_loader_test.go` (in-package test, per the
  mapping's unannotated line; no root-private symbols read).

## Export

`listAppliedPagerDutyServiceRoutingQuery` was renamed to the exported
`ListAppliedPagerDutyServiceRoutingQuery`: the root test
`incident_routing_sql_schema_lockstep_test.go` (kept at root — annotated
`SPLIT: reads private symbols of incident, service` in `root.md` because it
also reads root-private `serviceIncidentEvidenceQuery`) reads this constant's
SQL text and must import the new package to keep doing so. `derefTrim` stayed
unexported; nothing outside this file used it.
`PostgresAppliedPagerDutyServiceRoutingLoader` and
`BackendRepositoryResolverAdapter` were already exported and needed no
further change.

## Callers repointed

- `go/cmd/reducer/main_helpers.go`: added the
  `github.com/eshu-hq/eshu/go/internal/storage/postgres/incident` import
  (distinct from its existing `.../reducer/incident` import — no alias
  needed, the declared package name is `incidentstore`) and requalified
  `postgres.PostgresAppliedPagerDutyServiceRoutingLoader` /
  `postgres.BackendRepositoryResolverAdapter` to `incidentstore.*`.
- `go/internal/storage/postgres/incident_routing_sql_schema_lockstep_test.go`:
  added the import and requalified
  `listAppliedPagerDutyServiceRoutingQuery` to
  `incidentstore.ListAppliedPagerDutyServiceRoutingQuery`; updated its
  comments' stale filename/identifier references.
- Comment-only path/identifier fixes (no behavior change):
  `go/internal/relationships/tfstatebackend/resolver.go` (old file path in a
  doc comment) and `go/internal/storage/postgres/service_incident_evidence_loader.go`
  (old unexported constant name in a doc comment, now
  `incidentstore.ListAppliedPagerDutyServiceRoutingQuery`).

## dirgate

Creating the `incident/` subpackage made dirgate flag four *already-mapped,
not-yet-moved* root sibling files by name collision
(`dirgate_naming_violation_subpkg`): `incident_freshness_schema_sql.go`,
`incident_freshness_sql.go`, `incident_freshness_store.go` (checklist step 20,
`freshness/incident/`) and `incident_routing_evidence_loader.go` (checklist
step 39, `facts/`). Per the executor brief, each got
`//nolint:dirgate // #6693 checklist step N moves this file to <dest>; not yet
executed` on its `package postgres` line instead of being moved early.
`internal/storage/postgres`'s row in `scripts/lib/dirgate-grandfather.tsv` was
re-pinned (one file leaves root; each rebase onto a sibling move re-derives
the row) to what
`bash scripts/verify-dirgate.sh --digest internal/storage/postgres` prints,
and `tools/golangci-lint-dirgate/grandfather.go` was regenerated.

## Verification (from `go/` unless noted)

- `gofumpt -l -w` on every changed file: exit 0, no files listed (already
  formatted).
- `go build ./...`: exit 0.
- `go vet ./...`: exit 0.
- `go vet -tags "integration perf5854_ack perf5740_completion perf6785_wait" ./internal/storage/postgres/...`: exit 0.
- `go test ./internal/storage/postgres/... -race -count=1`: exit 0, all
  packages pass including the new `.../postgres/incident`.
- `go test ./cmd/reducer/... ./internal/relationships/tfstatebackend/... -count=1`: exit 0.
- `go test -list '.*' ./internal/storage/postgres/incident/`: lists all 6
  moved test names
  (`TestLoadAppliedPagerDutyServiceRoutingDecodesRows`,
  `TestLoadAppliedPagerDutyServiceRoutingRejectsBlankScope`,
  `TestLoadAppliedPagerDutyServiceRoutingPropagatesQueryError`,
  `TestBackendRepositoryResolverAdapterSingleOwner`,
  `TestBackendRepositoryResolverAdapterAmbiguousOwner`,
  `TestBackendRepositoryResolverAdapterNoOwner`).
- Exact-name repoint proof for each of the 6 names: `for n in TestLoadAppliedPagerDutyServiceRoutingDecodesRows TestLoadAppliedPagerDutyServiceRoutingRejectsBlankScope TestLoadAppliedPagerDutyServiceRoutingPropagatesQueryError TestBackendRepositoryResolverAdapterSingleOwner TestBackendRepositoryResolverAdapterAmbiguousOwner TestBackendRepositoryResolverAdapterNoOwner; do go test ./internal/storage/postgres/incident/... -list "^$n\$" -count=1 | rg -q "^$n\$" || echo "missing $n"; done` exits 0 for every name; the same command against
  `./internal/storage/postgres` exits 1 for every name (test no longer
  registered at root).
- From the repo root: `bash scripts/verify-dirgate.sh --all`: exit 0, no
  output. `bash scripts/verify-package-docs.sh`, run on the committed branch:
  exit 0, "changed Go package docs present". `bash scripts/verify-performance-evidence.sh
  origin/main`: exit 0, "benchmark and observability markers found for
  hot-path changes". `git diff --check`: exit 0, no output.
- `rg` for `incident_repository_correlation_loader` (old filename) and
  `listAppliedPagerDutyServiceRoutingQuery` (old unexported name) across
  `go/`: no hits outside this evidence note and the design docs under
  `docs/internal/design/6693-postgres-target-tree*`.

No-Regression Evidence: the query text, predicates, and ordering in
`ListAppliedPagerDutyServiceRoutingQuery` are byte-identical to the
pre-move `listAppliedPagerDutyServiceRoutingQuery`; the resolver's error
mapping (`ErrNoConfigRepoOwnsBackend` -> blank, `ErrAmbiguousBackendOwner` ->
`Ambiguous: true`) is unchanged. No SQL, lock, lease, batch size, worker
count, or retry behavior changed — only paths, the package clause, and one
symbol's export status. `go test ./internal/storage/postgres/... -race
-count=1` and the cmd/reducer and tfstatebackend package tests above are
green on the moved tree.

No-Observability-Change: no metric, span, log key, or status field was added,
removed, or renamed; the loader and adapter still execute the same bounded
SQL query and resolver call through injected dependencies with no
instrumentation of their own.
