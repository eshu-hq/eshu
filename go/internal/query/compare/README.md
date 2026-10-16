# compare

## Purpose

Serves `POST /api/v0/compare/environments`: one workload's materialized state
in two environments, the cloud resources each uses, and what differs. See
`doc.go` for the contract.

## Ownership boundary

Owns the route, its request validation, the two graph reads it issues
(`fetchWorkload` and `environmentSnapshot`), the diff and confidence scoring,
and the response story. List bounds and row trimming come from
`internal/query/impact`; service-evidence fallback comes from
`internal/query/service`.

## Layout

- `failure.go` — the fixed per-read failure messages and
  `writeCompareReadFailure` (#7674).
- `handler.go` — `Handler`, request validation, access scoping, the two graph
  reads, the resource diff and the confidence score.
- `story.go` — the response shape: summary, coverage, evidence rows and
  answer metadata.
- `evidence.go` — provenance for an environment inferred from service
  evidence.
- `capability.go` — `Capability` and `Support`, the single declaration of the
  `platform_impact.environment_compare` row.

## Dependencies

`internal/query/querycontract`, `internal/query/impact` (list bounds, row
trimming, repo grant check), `internal/query/service` (evidence reader and
evidence shape). Neither imports this package, so there is no cycle.

## Telemetry

The handler opens no span of its own; its graph reads go through whatever
`GraphQuery` the caller wires in. Unchanged by the #6642 move.
A failed read records its error on the request span (exception event, Error
status with the fixed message), or only the `eshu.request.client_canceled`
event for a client cancel (#7674).

## Gotchas / invariants

- `fetchWorkload` and `environmentSnapshot` are registered in
  `internal/queryplan/testdata/query-source-coverage.yaml` as typed
  `keyed_support` non-hot reads with a source digest. Any edit to either
  function, including its signature, changes the digest: re-audit the read
  and update the entry in the same change.
- An empty grant, or a grant that does not cover the workload's `repo_id`,
  must produce the same response as a workload that does not exist.

## Failure answers (#7674)

`failure.go` holds one fixed message per read and `writeCompareReadFailure`.
`querycontract.WriteGraphReadError` runs first (graph outage or reader fence:
503 with `Retry-After`; graph deadline: 504), then
`tracing.WriteServerFailure` (500 with the error on the request span, or 499
for a client cancel). Every error body, the 400s included, is written by
`querycontract.WriteError`, so the route keeps its plain
`{"error", "detail"}` shape and has no private writer. `server_failure_test.go`
proves it with the shared `testutil/failure` harness. See
[Compare And Semantic Search Failures](../../../../docs/public/reference/http-api/compare-and-semantic-search-failures.md).

## Move evidence (#6642)

`compare.go`, `compare_evidence.go`, `compare_story.go` and their tests moved
here as `handler.go`, `evidence.go`, `story.go`, `handler_test.go`,
`golden_fixture_test.go` and `story_test.go` (`git mv`). `CompareHandler`
became `Handler`. `context_story_limits.go` stayed in root: it only forwards to
`querycontract`, and nothing here calls it. The environment-compare case of
root's answer-metadata sweep moved here as
`TestEnvironmentCompareResponseCarriesAnswerMetadata`.

## No-Regression Evidence

No-Regression Evidence: both Cypher statements, their parameters, the list
bounds, the grant checks and the response shape are unchanged; only package
qualifiers and names differ.

## No-Observability-Change

No-Observability-Change: no span, metric or log is added or removed.

## Related docs

- [Query plan inventory](../../queryplan/README.md)
