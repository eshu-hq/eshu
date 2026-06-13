# Collector SDK

## Purpose

`internal/collector/sdk` holds shared first-party collector helpers for bounded
HTTP execution, safe provider failures, retry-after parsing, and common
failure-class decisions. It exists to keep provider collectors focused on source
truth, pagination, normalization, and redaction instead of duplicating the same
kernel code.

## Ownership boundary

This package owns reusable collector-kernel mechanics only. It does not own
provider endpoint traversal, fact envelope construction, source-specific
redaction, workflow claims, Postgres commits, graph writes, reducers, query
surfaces, or extension-host process protocols.

## Exported surface

See `doc.go` for the godoc package contract. The public surface includes:

- `Version` for the first internal SDK contract line.
- `FailureClass`, shared failure constants, `StatusPolicy`, and
  `ProviderFailure`.
- `HTTPError`, `HTTPDoer`, `JSONRequest`, including custom body decoders,
  `ParseBaseURL`, `DefaultHTTPClient`, `ParseRetryAfter`,
  `ParseRetryAfterHeader`, `ShouldRetryStatus`, and `DoJSON`.

## Dependencies

The package depends only on the Go standard library. It intentionally imports no
Eshu storage, facts, workflow, telemetry, reducer, query, or graph packages.

## Telemetry

This package emits no telemetry directly. Callers keep provider-specific spans,
metrics, and low-cardinality labels in their owning collector packages.

## Gotchas / invariants

- `HTTPError.Error` never includes provider response bodies.
- `DoJSON` retries only statuses selected by `ShouldRetryStatus` or the caller's
  override, and it closes every response body before retrying or returning.
- `JSONRequest.Decode` lets a provider keep YAML or non-standard payload
  parsing local while still using the shared request, retry, status, and
  body-close path.
- `ParseBaseURL` rejects credential-bearing URLs before any request can be
  built.
- Provider collectors may wrap these helpers, but source-specific redaction and
  warning semantics stay local to the provider package.

## Related docs

- `docs/public/guides/collector-authoring.md`
- `docs/public/deployment/service-runtimes-collectors.md`
- `docs/internal/design/1821-collector-extension-sdk-contracts.md`

## Evidence

Line-count marker: before extraction, the four initial adopters (`jira`,
`pagerduty`, `grafana`, and `tempo`) carried 1,727 lines across their local
`failure.go` and `http_client.go` files. After extraction, provider-specific
REST traversal is 1,262 lines across adopter `client.go` files plus Jira's
partial collection wrapper, and the shared SDK production code is 400 lines.
The existing adopter surface is 65 lines smaller while future collectors reuse
the shared request and failure kernel instead of copying it.

No-Regression Evidence: `go test ./internal/collector/sdk
./internal/collector/jira ./internal/collector/pagerduty
./internal/collector/grafana ./internal/collector/tempo -count=1` covers SDK
base URL validation, retry-after parsing, bounded HTTP retries, safe HTTP
errors, and the four adopters' existing pagination, redaction, warning,
rate-limit, and provider failure behavior.

No-Observability-Change: the SDK emits no telemetry directly. Jira, PagerDuty,
Grafana, and Tempo keep their existing provider request counters, fetch
duration histograms, rate-limit counters, fact counters, and provider spans;
metric labels remain bounded to provider, status class, fact kind, and existing
low-cardinality dimensions.

Line-count marker (#2361): Prometheus/Mimir and Loki now reuse the SDK request
and failure kernel. This slice deletes 210 lines of duplicated local
`failure.go` wrappers, removes the last local retry-status helper in these two
packages, and leaves the affected production Go diff at 254 insertions and 356
deletions, a net reduction of 102 lines while preserving provider-owned API
status handling and Loki YAML decoding.

No-Regression Evidence (#2361): `go test ./internal/collector/sdk
./internal/collector/prometheusmimir ./internal/collector/loki -count=1` covers
the SDK custom decoder hook, bounded SDK `HTTPError` return path, retry counts
on hard provider failures, Prometheus/Mimir API-status failures, Loki YAML rule
decoding, partial warnings, and terminal versus retryable workflow failure
classes.

Collector Performance Evidence: Prometheus/Mimir and Loki keep the same
provider endpoint sequence, the same `maxHTTPRetries = 2`, the same per-target
resource limits, and the same no-backoff retry behavior as before this
migration. The production diff removes 102 net lines across the affected
collector and SDK packages while adding no provider calls, goroutines,
channels, queues, graph writes, database writes, runtime knobs, or deployment
processes.

Collector Observability Evidence: Prometheus/Mimir and Loki still report
provider fetch duration, provider request counts, rate-limit counts, retry
counts, emitted fact counts, stale counts, and redaction or high-cardinality
counts through their existing package telemetry. Hard provider failures now
return the partially populated `CollectionResult`, so retry counts from failed
HTTP attempts can reach the claimed-source telemetry path.

Collector Deployment Evidence: this migration changes only Go package code and
package docs. It does not add or remove collector binaries, Helm values,
ServiceMonitors, command flags, environment variables, health routes, status
routes, scrape labels, or runtime profiles for `collector-prometheus-mimir` or
`collector-loki`.

No-Observability-Change (#2361): Prometheus/Mimir and Loki still emit
provider-local spans, request counters, retry counters, rate-limit counters,
fact counters, stale counters, and redaction or high-cardinality counters. The
SDK remains telemetry-free, and metric labels stay bounded to provider, status
class, fact kind, and existing low-cardinality reason values.

Line-count marker (#2366): the GitHub REST collectors (`securityalerts` and
`cicdrun/ghactionsruntime`) now reuse SDK failure constants, bounded default
HTTP clients, base URL validation, `HTTPError` wrappers, and `Retry-After`
parsing. GitHub `rel=next` cursor validation, `X-RateLimit-Reset` handling, and
GitHub Actions `UseNumber` decoding remain provider-owned. This intentionally
keeps the local GitHub traversal files and leaves the production Go diff at 20
net added lines while deleting the local GitHub Actions `Retry-After` parser.

No-Regression Evidence (#2366): `go test ./internal/collector/sdk
./internal/collector/securityalerts
./internal/collector/securityalerts/alertruntime
./internal/collector/cicdrun/ghactionsruntime
./cmd/collector-security-alerts ./cmd/collector-cicd-run -count=1` covers SDK
HTTP error wrapping, HTTP-date `Retry-After` parsing, credential-bearing base
URL rejection, Dependabot cursor pagination and same-host guards, GitHub
Actions rate-limit retry guidance, artifact URL redaction, and command config
construction.

No-Observability-Change (#2366): security-alert and CI/CD run collectors keep
their existing provider request counters, fetch-duration histograms, rate-limit
counters, fact counters, partial-generation counters, and observe/fetch spans.
The SDK remains telemetry-free, and metric labels remain bounded to provider,
status class, fact kind, and existing low-cardinality reason values.

Line-count marker (#2371): Confluence now reuses SDK base URL validation,
bounded default HTTP client construction, bounded `HTTPError` values, and
`Retry-After` parsing. Confluence-owned read-only GET traversal, `_links.next`
pagination, 403/404 permission-gap handling, retry backoff and jitter, and
source-stage telemetry stay local. The production Go diff is four net added
lines while deleting the local Confluence `Retry-After` parser.

No-Regression Evidence (#2371): `go test ./internal/collector/sdk
./internal/collector/confluence ./cmd/collector-confluence -count=1` covers SDK
base URL credential rejection, default client construction, bounded SDK HTTP
errors for retryable and non-retryable provider statuses, HTTP-date
`Retry-After` parsing, read-only Confluence GET requests, context-rooted
pagination, source retry backoff, and command service wiring.

No-Observability-Change (#2371): Confluence keeps its existing HTTP request
counters, fetch-duration histograms, sync failure counters,
permission-denied counters, fact counters, shared `collector.observe` span, and
hosted status surface. The SDK remains telemetry-free, and metric labels remain
bounded to operation, result, status class, failure class, and the existing
collector dimensions.

Line-count marker (#2374): package-registry metadata fetches now reuse the SDK
bounded HTTP error and default-client primitives while keeping provider-specific
request shape and registry workflow failure classification in
`packageruntime`. The production diff changes one provider file and adds the
SDK dependency without adding retries, sleeps, goroutines, queues, graph writes,
database writes, collector binaries, Helm values, or runtime knobs.

No-Regression Evidence (#2374): `go test ./internal/collector/sdk
./internal/collector/packageregistry
./internal/collector/packageregistry/packageruntime
./cmd/collector-package-registry -count=1` covers SDK default-client reuse,
bounded SDK `HTTPError` causes for status and transport failures, the existing
`registry_*` failure-class mapping, rate-limit sentinel matching, metadata URL
redaction, request auth, ecosystem-specific accept headers, and metadata body
bounds.

No-Observability-Change (#2374): package-registry retains its provider-local
request, observe, rate-limit, fact, parse-failure, warning-fact, readiness,
metrics, and admin-status signals. The SDK still emits no telemetry directly,
and no metric label now contains package names, metadata URLs, versions, feed
paths, source locators, or credential material.

Line-count marker (#2377): Vault live's `vaultapi` adapter now reuses SDK base
URL validation, bounded default HTTP client construction, and bounded
`HTTPError` values. Vault-owned metadata endpoint traversal, KV `/data/` guards,
namespace/token headers, 404-as-empty handling, body-size limits, and API-call
observation hooks stay local, and the package still has no HashiCorp Vault SDK
dependency.

No-Regression Evidence (#2377): `go test ./internal/collector/sdk
./internal/collector/vaultlive ./internal/collector/vaultlive/vaultapi
./cmd/collector-vault-live -count=1` covers SDK base URL credential rejection,
bounded default client construction, SDK HTTP error wrapping for status and
transport failures, Vault metadata-only path guards, 404 empty-state behavior,
all seven metadata families, API-call observations, redaction canaries, claim
config, and command service wiring.

No-Observability-Change (#2377): Vault live keeps its existing secrets/IAM
source fact counters, API-call counters, partial-scope counters, redaction
counters, freshness gauge, shared collector metrics, and `vault_live.snapshot`
span. The SDK emits no telemetry directly, and metric labels remain bounded to
source, fact kind, operation, result, reason, field class, and scope kind.

Line-count marker (#2381): OCI Distribution now reuses SDK base URL validation,
bounded default HTTP client construction, and bounded `HTTPError` values for
registry and token service status or transport failures. OCI-owned endpoint
resolution, `/v2/` auth-challenge handling, token query shaping, repository path
escaping, referrers capability errors, blob body caps, and registry workflow
failure classes stay local.

No-Regression Evidence (#2381): `go test ./internal/collector/sdk
./internal/collector/ociregistry/distribution
./internal/collector/ociregistry/ociruntime ./internal/collector/sbomruntime
./cmd/collector-oci-registry ./cmd/collector-sbom-attestation -count=1` covers
SDK base URL credential rejection, default client construction, SDK HTTP error
causes for status and transport failures, Distribution ping, tag, manifest,
blob, referrer, and token behavior, OCI runtime warning handling, and the SBOM
OCI-referrer fetch path that depends on this client.

No-Observability-Change (#2381): Distribution and the SDK remain telemetry-free.
The OCI and SBOM runtimes keep their existing provider-local spans, request
counters, warning facts, status surfaces, and shared collector metrics. Metric
labels remain bounded and do not include registry hosts, repository paths, tags,
digests, URLs, token values, or credential material.

Line-count marker (#2384): SBOM configured-document fetches now reuse the SDK
default HTTP client constructor, bounded `HTTPError` values, and `Retry-After`
parsing. SBOM-owned arbitrary document body reads, auth header construction,
OCI referrer delegation, document identity, and source URI redaction stay local
to `internal/collector/sbomruntime`.

No-Regression Evidence (#2384): `go test ./internal/collector/sbomruntime -run
'TestHTTPProviderConfiguredSource(StatusFailure|TransportFailure)' -count=1`
covers bounded SDK HTTP status and transport causes, retry-after preservation,
registry failure class/details compatibility, configured document response-body
redaction, and transport cause unwrapping.

No-Observability-Change (#2384): SBOM configured-document fetches keep the same
claim workflow, fact emission, warning fact, and workflow failure class/details
surface. The SDK remains telemetry-free and adds no metric, span, log, status,
queue, graph write, deployment, or runtime profile change.

Line-count marker (#2387): the vulnerability-intelligence source clients now
reuse SDK default HTTP client construction, bounded `HTTPError` values,
`Retry-After` parsing, and transport-error wrapping for OSV, CISA KEV, FIRST
EPSS, and NVD. Source-owned request shaping, OSV ecosystem mapping, CISA KEV
catalog decoding, FIRST EPSS query limits, NVD window validation, and NVD
header-only API key handling stay local.

No-Regression Evidence (#2387): `go test ./internal/collector/sdk
./internal/collector/vulnerabilityintelligence
./internal/collector/vulnerabilityintelligence/vulnruntime
./cmd/collector-vulnerability-intelligence -count=1` covers SDK-backed HTTP
status, transport, decode, and retry-after behavior for vulnerability source
clients plus runtime request-result and source-state classification from
structured SDK errors.

No-Observability-Change (#2387): the SDK remains telemetry-free. The
vulnerability-intelligence runtime keeps ownership of observe/fetch spans,
observation counters, fetch-duration histograms, fact counters, rate-limit
counters, durable source-state rows, health, readiness, metrics, and
admin-status surfaces.
