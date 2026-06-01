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
- `DerivedTargetConfig` enables bounded target resolution from coordinator
  scope IDs derived from active owned package evidence.
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
  target. When derived targets are enabled, implicit targets are normalized
  package identities for npm, PyPI, Go modules, Maven, NuGet, Composer,
  RubyGems, and Cargo that started from active owned dependency evidence.
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
- Derived npm targets use the abbreviated packument path, and derived PyPI
  targets use the PyPI JSON project API. Both keep the configured
  `package_limit` / `version_limit` contract. When the derived `version_limit`
  is omitted, it defaults to one registry version so full-corpus vulnerability
  enrichment records package identity without projecting every registry version
  and dependency edge for heavily reused packages.
- Derived Go module, Maven, NuGet, Composer, RubyGems, and Cargo targets are
  identity evidence until native metadata adapter URLs land. The runtime
  completes those claims with `warning_code=unsupported_metadata_source` rather
  than crawling public registries or reporting clean scanner output.
- Private registry and Artifactory targets must use explicit `metadata_url`
  configuration. Missing credentials for private explicit targets complete as
  `warning_code=credentials_missing`; registry URLs and credential material stay
  out of facts, metrics, and warning messages.
- A 404 for a derived target is missing evidence, not a failed collection
  run. The runtime completes the claim with a `package_registry.warning` fact
  using `warning_code=registry_not_found`. Explicitly configured targets keep
  their existing `registry_not_found` failure behavior.
- Parser failures for fetched metadata complete as
  `warning_code=malformed_metadata` unless the parser registry has no adapter
  for the ecosystem, which is `warning_code=unsupported_metadata_source`.
- A metadata document larger than the configured 20 MiB response cap is a
  deterministic coverage gap, not a retryable provider outage. The runtime
  completes the claim with a `package_registry.warning` fact using
  `warning_code=metadata_too_large`, preserving package identity as bounded
  source-state evidence for readiness without raising the cap.

## Evidence

Collector Performance Evidence: `go test ./internal/collector/packageregistry/packageruntime -run 'TestBoundedParsedMetadataTruncatesVersionsAndEmitsWarning|TestClaimedSourceTruncatesMetadataOverVersionLimit|TestClaimedSourceParsesMetadataIntoPackageRegistryFacts|TestClaimedSourceSanitizesSourceURIBeforeFactEmission' -count=1 -v` proves over-limit package metadata stays bounded without retrying the whole collector claim. The bounding pass remains linear over parsed observations, emits no more than `version_limit` version facts per package, and drops dependent observations for truncated versions before envelope construction.

No-Regression Evidence: `go test ./internal/collector/packageregistry/packageruntime -run TestHTTPMetadataProviderRequestsAbbreviatedNPMPackument -count=1 -v` proves npm targets request abbreviated packuments before the body-size guard runs. On 2026-05-23, `curl -fsSL -H 'Accept: application/vnd.npm.install-v1+json; q=1.0, application/json; q=0.8, */*' https://registry.npmjs.org/vite -o /tmp/eshu-vite-abbreviated-packument.json && wc -c /tmp/eshu-vite-abbreviated-packument.json` returned 2,249,718 bytes, while the previous generic JSON accept shape returned 38,843,326 bytes for the same public package.

Collector Observability Evidence: truncated package metadata emits a durable `package_registry.warning` fact with `warning_code=version_limit_truncated`, and the existing `eshu_dp_package_registry_facts_emitted_total` counter reports it through the bounded `fact_kind=package_registry.warning` label. `/admin/status` continues to expose package-registry collector completion, retryable failure, and terminal failure counts without adding package names, feed URLs, versions, or artifact paths to metric labels.

No-Observability-Change: no new metrics or labels were added. Existing package-registry duration, request status-class, facts-emitted, rate-limit, generation-lag, parse-failure, health, readiness, metrics, and admin-status signals already cover npm abbreviated-packument success and the body-size failure path without package names, feed URLs, versions, or artifact paths in metric labels.

Collector Deployment Evidence: `helm lint deploy/helm/eshu` and `go test ./internal/runtime -run 'TestHelmClaimDrivenCollectorsRequireWorkflowCoordinator|TestHelmWorkflowCoordinatorActiveModeForClaimDrivenCollectors|TestHelmPodSecurityContextUsesOnRootMismatch' -count=1 -v` prove the chart renders active workflow-coordinator claim scheduling for hosted collectors and keeps the package-registry collector Deployment on the existing metrics Service and ServiceMonitor path.

No-Regression Evidence: `go test ./internal/collector/packageregistry/packageruntime -run 'TestClaimedSource(ResolvesDerivedNPMTarget|RejectsDerivedTargetWhenDisabled)' -count=1` proves the runtime resolves only enabled, normalized npm derived scopes and rejects unknown scopes when derivation is disabled. The broader touched-package proof ran `go test ./internal/coordinator ./internal/workflow ./internal/storage/postgres ./internal/collector/packageregistry/packageruntime ./internal/collector/vulnerabilityintelligence/vulnruntime ./cmd/workflow-coordinator ./cmd/collector-package-registry ./cmd/collector-vulnerability-intelligence -count=1`.

No-Regression Evidence: `go test ./internal/collector/packageregistry/packageruntime -run 'TestClaimedSourceCompletesDerivedNotFoundAsWarning|TestClaimedSourceKeepsConfiguredNotFoundAsError' -count=1` proves derived npm registry 404s complete as warning evidence while explicitly configured targets still surface `registry_not_found` as a collector failure.

No-Observability-Change: derived npm targets use the existing package-registry observe duration, request status-class, facts-emitted, rate-limit, generation-lag, parse-failure, health, readiness, metrics, and admin-status signals. No new metric labels were added, and package names, versions, metadata URLs, and credential material stay out of labels.

No-Regression Evidence: `go test ./internal/collector/packageregistry/packageruntime -run 'TestHTTPMetadataProviderClassifiesOversizedMetadataAsTerminalSourceState|TestClaimedSourceCompletesMetadataTooLargeAsCoverageGapWarning|TestClaimedServiceCompletesMetadataTooLargeWithoutRetry' -count=1` proves a response over the configured metadata byte limit is classified as deterministic, converted into `package_registry.warning` evidence, and completed by `collector.ClaimedService` without `FailClaimRetryable` or retry-budget terminal failure.

Observability Evidence: oversized package metadata records `status_class=metadata_too_large` on the existing package-registry request/observe metrics and emits a durable `package_registry.warning` fact with `warning_code=metadata_too_large`. No new metric labels were added, and package names, metadata URLs, versions, and credential material stay out of metric labels and warning messages.

No-Regression Evidence: `go test ./internal/collector/packageregistry/packageruntime -run 'TestClaimedSource(ResolvesDerivedPyPITarget|CompletesUnsupportedDerivedMetadataSourceAsWarning|CompletesMalformedMetadataAsWarning|CompletesMissingCredentialsAsWarning)' -count=1` proves derived PyPI target resolution and fail-closed warning behavior for unsupported adapters, malformed metadata, and missing private-registry credentials.

Observability Evidence: derived missing-evidence claims emit stable `package_registry.warning` facts with `warning_code` values `unsupported_metadata_source`, `registry_not_found`, `metadata_too_large`, `malformed_metadata`, and `credentials_missing`. The new per-ecosystem `/admin/status` metadata-target counts are read from workflow rows and warning facts; no new metric labels, raw package names, registry URLs, or credential values were added.

## Related Docs

- [Package registry ADR](../../../../../docs/public/reference/component-package-manager.md)
- [Service runtimes](../../../../../docs/public/deployment/service-runtimes.md)
