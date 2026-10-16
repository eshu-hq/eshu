# 7674 slice 10: infra and inventory route failures

Slice 10 of #7674 moves the failure answers of the root infrastructure,
relationship catalog, cloud resource, graph entity, container image,
admission decision, runtime drift, operator control plane, and surface
inventory routes onto fixed messages. A stale or timed-out PostgreSQL reader
now answers the retryable `503` on the routes that read through the guarded
reader, and a client cancel answers `499`. The operator control plane has no
capability-matrix key, so it passes `querycontract.NoCapability` from an
exactly allowlisted file until #8227 registers its key. The route list and
messages are in
`docs/public/reference/http-api/infra-and-inventory-failures.md`.

## No-Regression Evidence

No-Regression Evidence: baseline is the base of this branch on main (the
merge base with `origin/main`, slice 9 included), measured against this
branch's head. No SQL, Cypher, transaction, lock,
queue, lease, or batching path changed. Input shape, statement text,
statement count, and row counts are the same as the baseline: only the error
branches of the handlers changed. The queryplan source digests of five
symbols moved because their error branches changed:
`(*InfraHandler).searchResources`, `(*InfraHandler).listCloudResources`,
`(*InfraHandler).getRelationships`, `(*GraphEntityInventoryHandler).listEntities`,
and `(*ImageHandler).listImages`. Their statements and call counts did not
move (`TestHotCypherManifestCoversEveryProductionQueryCall` and
`TestLegacyQueryplanManifestBindsProductionQueries` pass). The added work on
a failed read is one `WriteGraphReadError` call, a switch over sentinel checks
that returns false for a non-verdict error, then `tracing.WriteServerFailure`
or `tracing.ServerFailureEnvelope`. The success path is unchanged. Measured
with stub stores, no live backend: the six route tests (20 failure steps
driven through the canary, reader fence, client cancel, and inner cancel
cases with a fence capability check, plus 4 steps that read no store) report
80 failed checks on the baseline handlers and pass on this branch.

## Observability Evidence

Observability Evidence: a failed step now marks the handler span, or the
request span on the routes that open none, through
`tracing.WriteServerFailure` or `tracing.ServerFailureEnvelope`: Error status
with the fixed message and one `exception` event for a server fault, or only
the `eshu.request.client_canceled` event and Unset status for a client
cancel. The shared harness in `go/internal/query/testutil/failure` asserts
both on every converted route. No instrument was added, removed, or renamed.
The image list, tag-history, and cloud resource list counters keep their
outcome labels, so a client cancel there still counts under the step's
existing error label; the `499` status and the cancel span event tell it
apart. A canceled read moves from the `5xx` to the `4xx` status class on
`eshu_dp_api_request_duration_seconds`, as on the earlier #7674 slices.
