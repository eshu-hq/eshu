# Package Registry Runtime

## Purpose

`internal/collector/packageregistry/packageruntime` owns the claim-driven
runtime for `package_registry` collector work. It maps one workflow claim to
one configured package-registry target, fetches that target's metadata document,
parses it with `packageregistry.MetadataParserRegistry` or the Artifactory
package wrapper when configured, and returns fact envelopes to
`collector.ClaimedService`.

## Flow

```mermaid
flowchart LR
  A["workflow.WorkItem"] --> B["ClaimedSource.NextClaimed"]
  B --> C["MetadataProvider.FetchMetadata"]
  C --> D["native parser or artifactory_package wrapper"]
  D --> E["New*Envelope\nincluding hosting, advisories + events"]
  E --> F["collector.CollectedGeneration"]
  F --> G["Postgres commit with claim fencing"]
```

## Exported Surface

- `SourceConfig` validates collector instance ID, bounded targets, provider,
  and optional telemetry handles.
- `TargetConfig` stores parsed target identity plus runtime-only endpoint,
  document format, and credential material.
- `MetadataProvider` fetches one bounded metadata document for a target.
- `HTTPMetadataProvider` performs the first production metadata fetch path
  using an explicit `metadata_url`.
  For npm targets, it requests npm's abbreviated packument media type
  (`application/vnd.npm.install-v1+json`) with JSON fallback so popular
  packages do not require downloading full metadata documents that contain
  readmes and other install-irrelevant fields.
- `ClaimedSource` implements `collector.ClaimedSource`.

## Telemetry

The runtime records:

- `eshu_dp_package_registry_observe_duration_seconds`
- `eshu_dp_package_registry_requests_total`
- `eshu_dp_package_registry_facts_emitted_total`
- `eshu_dp_package_registry_rate_limited_total`
- `eshu_dp_package_registry_generation_lag_seconds`
- `eshu_dp_package_registry_parse_failures_total`

Labels stay bounded to provider, ecosystem, result/status class, fact kind, and
document type. Package names, private feed URLs, versions, and artifact paths
must stay out of metrics.

## Invariants

- A claimed source only collects the configured `scope_id` from the workflow
  item. Unknown scope IDs fail the claim instead of falling back to another
  target.
- `document_format` defaults to `native`. `artifactory_package` is allowed only
  as a wrapper around package-native metadata and uses the same parser registry
  as native metadata; Artifactory repository topology remains hosting evidence.
- `collector_instance_id`, `generation_id`, and `fencing_token` come from the
  workflow claim path and are copied into every emitted fact.
- Advisory and registry-event observations are bounded by the same configured
  package and version limits as dependencies, artifacts, and source hints.
- Package scope remains strict: metadata that exceeds `package_limit` fails the
  claim. Version scope is truncating: metadata over `version_limit` keeps the
  first deterministic version set, drops dependent observations for truncated
  versions, and emits a package-scoped `version_limit_truncated` warning fact.
- Credentials stay in `TargetConfig` runtime fields. They must not be copied to
  facts, logs, metric labels, or docs.
- `HTTPMetadataProvider` accepts one explicit metadata URL per target; it does
  not crawl feeds or enumerate registries.
- npm packument targets use the npm registry's abbreviated metadata request
  shape. The body-size cap still applies to the returned document; the request
  shape prevents normal npm package identity and version evidence from needing
  an unbounded full packument.

## Evidence

Collector Performance Evidence: `go test ./internal/collector/packageregistry/packageruntime -run 'TestBoundedParsedMetadataTruncatesVersionsAndEmitsWarning|TestClaimedSourceTruncatesMetadataOverVersionLimit|TestClaimedSourceParsesMetadataIntoPackageRegistryFacts|TestClaimedSourceSanitizesSourceURIBeforeFactEmission' -count=1 -v` proves over-limit package metadata stays bounded without retrying the whole collector claim. The bounding pass remains linear over parsed observations, emits no more than `version_limit` version facts per package, and drops dependent observations for truncated versions before envelope construction.

No-Regression Evidence: `go test ./internal/collector/packageregistry/packageruntime -run TestHTTPMetadataProviderRequestsAbbreviatedNPMPackument -count=1 -v` proves npm targets request abbreviated packuments before the body-size guard runs. On 2026-05-23, `curl -fsSL -H 'Accept: application/vnd.npm.install-v1+json; q=1.0, application/json; q=0.8, */*' https://registry.npmjs.org/vite -o /tmp/eshu-vite-abbreviated-packument.json && wc -c /tmp/eshu-vite-abbreviated-packument.json` returned 2,249,718 bytes, while the previous generic JSON accept shape returned 38,843,326 bytes for the same public package.

Collector Observability Evidence: truncated package metadata emits a durable `package_registry.warning` fact with `warning_code=version_limit_truncated`, and the existing `eshu_dp_package_registry_facts_emitted_total` counter reports it through the bounded `fact_kind=package_registry.warning` label. `/admin/status` continues to expose package-registry collector completion, retryable failure, and terminal failure counts without adding package names, feed URLs, versions, or artifact paths to metric labels.

No-Observability-Change: no new metrics or labels were added. Existing package-registry duration, request status-class, facts-emitted, rate-limit, generation-lag, parse-failure, health, readiness, metrics, and admin-status signals already cover npm abbreviated-packument success and the body-size failure path without package names, feed URLs, versions, or artifact paths in metric labels.

Collector Deployment Evidence: `helm lint deploy/helm/eshu` and `go test ./internal/runtime -run 'TestHelmClaimDrivenCollectorsRequireWorkflowCoordinator|TestHelmWorkflowCoordinatorActiveModeForClaimDrivenCollectors|TestHelmPodSecurityContextUsesOnRootMismatch' -count=1 -v` prove the chart renders active workflow-coordinator claim scheduling for hosted collectors and keeps the package-registry collector Deployment on the existing metrics Service and ServiceMonitor path.

## Related Docs

- [Package registry ADR](../../../../../docs/public/reference/component-package-manager.md)
- [Service runtimes](../../../../../docs/public/deployment/service-runtimes.md)
