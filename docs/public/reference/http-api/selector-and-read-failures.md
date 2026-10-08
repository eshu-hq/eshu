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

## Client cancels

A client that cancels its request while one of the post-selector reads above
runs, or while a selector lookup runs through the shared selector helper, gets
`499` with the
same fixed message, and the request span is not marked as an error; the span
carries an `eshu.request.client_canceled` event instead. No route documents
`499` in the OpenAPI spec, because the client has already gone. Other routes
still answer a client cancel with `500` until #7674 lands. The telemetry
effects are in [Failed and canceled query reads](../telemetry/traces.md#failed-and-canceled-query-reads-7626).
