# Selector And Read Failures

This page covers how query routes answer a failed repository-selector lookup
and a failed read after the selector resolves (#7626). The reader-fence `503`
and graph-read verdicts these answers build on are in
[PostgreSQL reader fence failures](../http-api.md#postgresql-reader-fence-failures).

## Selector lookup failures

Repository-selector resolution through the shared request helper (the
package-registry, service-catalog, CI/CD, advisory-evidence, container-image,
SBOM-attachment, and impact routes) answers a catalog or graph failure that is
not a fence or graph-availability verdict with `500` and the fixed message
`repository selector lookup failed`, recorded on the request span, rather than
`400`. An unmatched selector stays `404` and an ambiguous one `400`.

The graph-backed `repo_id` selectors map the same lookup failure to the same
`500`: every `/api/v0/code/*` route that takes `repo_id` (including
`POST /api/v0/code/language-query`), where an unmatched or ambiguous selector
stays `400`, and `POST /api/v0/entities/resolve`, where an unmatched selector
stays `404` and an ambiguous one `400`. The optional `repo` selector on
`GET /api/v0/investigations/services/{service_name}`,
`GET /api/v0/services/{service_name}/story`, and
`GET /api/v0/services/{service_name}/intelligence-report` answers the same fixed `500`
instead of a `500` that carried the backend error text; an unmatched selector
there stays `404` and an ambiguous one `409`.

The content-backed selector routes map the same way: the
`GET /api/v0/repositories/{repo_id}/...` routes (`context`, `story`, `stats`,
`coverage`, `tree`, `content`, `branches`, `freshness`), `POST
/api/v0/iac/dead`, and the content read and search routes (`POST
/api/v0/content/files/read`, `.../files/lines`, `.../files/search`,
`.../entities/search`). A reader fence answers the retryable `503` (the
content routes previously answered `400`), a graph-availability or graph
deadline verdict on the repository routes answers `503`/`504`, and any other
failed catalog or graph read answers `500` with the fixed message `repository
selector lookup failed`, recorded on the request span (on `POST
/api/v0/iac/dead`, its handler span), instead of `400` with the backend error
text. The stats route keeps `504`, with the same fixed message, when the
selector read runs out its 2-second route budget. An unmatched selector stays
`404`, except on `POST /api/v0/iac/dead`, which keeps its `400`; an ambiguous
selector stays `400`.

## Read failures after the selector resolves

The content routes (including `POST /api/v0/content/entities/read`, which takes
no selector), repository stats and coverage, and the service context,
investigation, and story routes also answer a failed read after selector
resolution with a fixed message instead of the backend error text. The content
routes answer `content file read failed` (`files/read`, `files/lines`),
`content entity read failed` (`POST /api/v0/content/entities/read`), `content
file search failed`, or `content entity search failed`.
`GET /api/v0/repositories/{repo_id}/stats` answers `repository stats query
failed`, keeping `504` when its own route budget ran out, and `.../coverage`
answers `repository coverage query failed`. The service routes answer a fixed
message per step, for example `service context query failed`, `service
investigation enrichment failed`, or `service story ci/cd evidence load
failed`. Each is recorded on the request span. A reader fence on these reads
answers the retryable `503` with `Retry-After`; the content routes and the
service story's ci/cd and supply-chain reads previously answered `500`.

## Platform impact read failures

The `POST /api/v0/impact/*` routes answer a failed graph or content read with
`500` and a fixed message for the step that failed, recorded on the request
span, instead of `500` with the backend error text (#7674). The routes are
`blast-radius`, `change-surface`, `change-surface/investigate`, `pre-change`,
`developer-change-plan`, `contracts`, `entity-map`, `resource-investigation`,
`trace-resource-to-code`, `explain-dependency-path`, `trace-exposure-path`,
`trace-deployment-chain`, and `deployment-config-influence`. A route with
several reads names the step, for example `entity map start resolution
failed`, `dependency path ownership check failed`, or `deployment trace k8s
resource query failed`.

A reader fence on any read that fails one of these routes answers the
retryable `503` with `Retry-After`. The `trace-deployment-chain` k8s resource and GitOps evidence
steps, and the code-evidence reads on `change-surface/investigate`,
`pre-change`, and `developer-change-plan`, previously answered `500` or `503`
with the error text. Those code-evidence reads keep their `503` for any other
failure, now with a fixed message such as `change surface code evidence read
failed`. The `contracts` route's unavailable-graph `503` answers `graph
backend is unavailable`, never a wrapped error.

`POST /api/v0/impact/trace-exposure-path` answered a failed source lookup with
`400` and the content store's error text. That lookup now answers `500` with
`exposure path source read failed`, or the reader-fence `503`. A source name
that matches several entities still answers `400` naming them. An ambiguous
workload selector on `trace-deployment-chain` and
`deployment-config-influence` still answers `409` with the selector.

## IaC read failures

The IaC, AWS runtime drift, and replatforming routes answer a failed store or
graph read with `500` and a fixed message for the step that failed, recorded
on the handler span, instead of `500` with the backend error text (#7674). The
routes are `POST /api/v0/iac/dead`, `/iac/unmanaged-resources`,
`/iac/management-status`, `/iac/management-status/explain`, and
`/iac/terraform-import-plan/candidates`; `GET /api/v0/iac/resources`;
`POST /api/v0/aws/runtime-drift/findings`; and the
`/api/v0/replatforming/*` routes (`plans`, `rollups`, `ownership-packets`,
and `GET .../selectors`). Example messages are `count unmanaged cloud
resources failed`, `list AWS runtime drift findings failed`, `IaC resource
graph read failed`, and `read dead-IaC repository files failed`. The selector
inventory keeps its `internal_error` envelope with the capability and
profiles, and its fixed `replatforming selector inventory failed` message.

A reader fence on any of these reads answers the retryable `503`
`backend_unavailable` with `Retry-After`; the routes other than
`POST /api/v0/iac/dead` and the graph read of `GET /api/v0/iac/resources`
previously answered `500`. `POST /api/v0/replatforming/plans` answers a
composed plan that fails its own contract check with `composed replatforming
plan failed contract validation`; the old body quoted finding ids from the
validation error.

## Package registry and secrets/IAM read failures

The `GET /api/v0/package-registry/*` routes (`packages`, `versions`,
`dependencies`, `correlations`, `dependency-chains`, `packages/count`,
`packages/inventory`) and the `GET /api/v0/secrets-iam/*` routes
(`identity-trust-chains`, `privilege-posture-observations`,
`secret-access-paths`, `posture-gaps`, `posture-summary`) answer a failed
store or graph read with `500` and a fixed message for the step that failed,
recorded on the handler span, instead of `500` with the backend error text
(#7674). Example messages are `package registry package query failed`,
`package dependency chain query failed`, `secrets/IAM posture gap query
failed`, and `secrets/IAM posture summary query failed`.

The package-registry scoped-access probes run for a scoped caller before a
row is shown. A probe failure answers `500` with one fixed access-check
message per route, for example `package registry package access check
failed`, and never the empty page, `403`, or `404` a denial answers, so a
failure neither fails open nor reveals whether a package or version exists.

A reader fence answers the retryable `503` with `Retry-After` on every one of
these reads. The `correlations` and `dependency-chains` reads and all five
secrets/IAM reads previously answered `500`.

## Other read routes

These routes answer a failed store or graph read with `500` and a fixed
message, recorded on the handler span, instead of `500` with the backend
error text (#7674):

- `GET /api/v0/ci-cd/run-correlations`, `.../count`, and `.../inventory`, for
  example `count CI/CD run correlations failed`.
- `GET /api/v0/codeowners/ownership`: `codeowners ownership graph read failed`
  or `resolve effective repository owner failed`.
- `GET /api/v0/dependencies`: `dependency graph read failed`.
- `GET /api/v0/freshness/changed-since`, `.../generations`, and
  `.../services/changed-since`, for example `list generation lifecycle
  failed`.
- `GET /api/v0/incidents/{incident_id}/context`: `read incident context
  failed`. A failed scoped authorization check answers `incident context
  authorization failed` and stays fail-closed.
- `GET /api/v0/kubernetes/correlations`, `GET
  /api/v0/observability/coverage/correlations`, `GET
  /api/v0/service-catalog/correlations`, and `GET
  /api/v0/work-items/evidence`, for example `list kubernetes correlations
  failed`.
- `GET /api/v0/metrics/timeseries`: `metrics query failed`.
- `POST /api/v0/terraform/config-state-drift/findings`: `count Terraform
  config-vs-state drift findings failed` or `list Terraform config-vs-state
  drift findings failed`.

A reader fence on these reads answers the retryable `503` with `Retry-After`.
The metrics route reads Prometheus, which cannot produce a fence today. The OpenAPI spec now declares that `503` on the CI/CD, service catalog,
Terraform drift, and metrics routes. An incident that is not found answers
`404` with the fixed not-found text, not the wrapped lookup error.

A shared selector helper error that is neither an unmatched nor an ambiguous
selector answers the fixed `500` `repository selector lookup failed`, never a
`400` with the error text.

## Client cancels

A client that cancels its request while one of the post-selector reads above
runs, while a selector lookup runs through the shared selector helper, or
while a read on any route in the sections above runs, gets `499` with the
same fixed message, and
the request span is not marked as an error; the span carries an
`eshu.request.client_canceled` event instead. No route documents `499` in the
OpenAPI spec, because the client has already gone. Other routes still answer
a client cancel with `500` until #7674 lands. The telemetry effects are in
[Failed and canceled query reads](../telemetry/traces.md#failed-and-canceled-query-reads-7626).

## Supply-chain read failures

The `/api/v0/supply-chain/*` query routes, the impact investigation packet,
and the suppression mutation answer a failed store read or runtime probe with
`500` and a fixed message for the step that failed, instead of `500` with the
backend error text (#7674). Examples are `supply-chain impact findings read
failed`, `supply-chain impact runtime context probe failed`, `advisory catalog
read failed`, and `security alert reconciliation count read failed`. The
security-alert repository selector's catalog match and provider scope lookup
answer `repository selector lookup failed`, as its exact resolution already
did. The handler span records the error once and carries the step's message as
its Error description. A reader fence still answers the retryable `503` with
`Retry-After`.

A client cancel on these routes answers `499` with the same fixed message. A
query route logs one INFO `supply_chain_query.stage_canceled` line for the
stage and no `supply_chain_query.stage_failed` line, so a client walking away
does not page at ERROR. The suppression mutation has no stage timer and writes
neither line. A `context.Canceled` from an inner context while the request is
still live is a server fault and answers `500` with `stage_failed`. See
[Logging](../logging.md) for both events.
