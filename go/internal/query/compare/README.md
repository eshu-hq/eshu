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

## Gotchas / invariants

- `fetchWorkload` and `environmentSnapshot` are registered in
  `internal/queryplan/testdata/query-source-coverage.yaml` as typed
  `keyed_support` non-hot reads with a source digest. Any edit to either
  function, including its signature, changes the digest: re-audit the read
  and update the entry in the same change.
- An empty grant, or a grant that does not cover the workload's `repo_id`,
  must produce the same response as a workload that does not exist.

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
