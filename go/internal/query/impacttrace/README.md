# Impact trace helpers

## Purpose

`impacttrace` holds the non-method helpers behind the impact handler family
(Issue #6060, lane B): deployment-trace readers and shapers, GitOps/OCI/K8s
source collectors, live-evidence probes and stores, identity anchors,
bounds, controller-entity builders, and response shaping
(`BuildDeploymentTraceResponse` and its fields). Nothing here declares an
`ImpactHandler` method.

## Ownership boundary

This package owns pure family logic over the `querycontract` ports. The
`impact` package imports `impacttrace`, never the reverse; neither imports
the query root. Production drivers, service-story shaping, and entity
handling stay in root and reach this package only through data, never
through imports.

`BuildDeploymentTraceResponse` takes a caller-built, non-nil overview map
(see `impact/README.md` for the derivation contract): counts that are pure
functions of the fields derive inside, evidence lists attach only when
absent, and `deployment_truth_tier` stays caller-owned.

## Exported surface

The exported surface is described in [doc.go](doc.go). Exports exist for
cross-package callers: the `impact` handlers, staying root tests that pin
query text and store SQL, and the `query_test` seam tripwires. Each export
carries a comment naming who pins it.

## Dependencies

The package imports the Go standard library, `querycontract`, and
`querytestutil` in tests only. It must not import the query root, `impact`,
or graph drivers.

## Telemetry

This package emits no metrics, spans, or logs. Handlers retain their
existing telemetry.

No-Observability-Change: moving helpers here changes no call path that
emits telemetry.

## Performance

Helpers here run per-request response shaping, not hot row loops; shared
`querycontract` decoders inline away. No benchmark delta is claimed because
there is no runtime delta to measure.

No-Regression Evidence: same gates as `impact/README.md` — query, mcp,
queryplan, golden-corpus, replay-coverage, and ci gates plus build and vet,
all exit 0, with byte-identical B-7 cassettes and B-12 snapshot.

## Gotchas / invariants

- SQL query-text consts pinned by staying tests are exported with the
  lowercase name kept as an alias, so same-package callers are untouched.
- The live-evidence store dispatches per anchor kind; keep predicate
  ownership with the filter type.
- `RepositoryAccessFilter` values must be derived from the request's
  `AuthContext`, never hand-built to widen access.

## Verification

From `go/`, run `go test ./internal/query/... ./internal/mcp/... -count=1`,
`go test ./internal/queryplan/ -count=1`, `go build ./...`, and
`go vet ./...`. From the repository root, run
`scripts/verify-package-docs.sh` and the B-7 golden-corpus proof selected by
the parent package instructions.

## Related docs

- [Source layout](../../../../docs/public/reference/source-layout.md)
- [HTTP API](../../../../docs/public/reference/http-api.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
