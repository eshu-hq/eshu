# AGENTS.md - internal/collector/gcpcloud/gcpruntime guidance

## Read First

1. `README.md` - package responsibilities and the PageProvider seam.
2. `../AGENTS.md` and `../README.md` - the gcpcloud source-fact contract and
   invariants this runtime inherits.
3. `docs/public/reference/gcp-cloud-collector-contract.md` - scope/generation,
   fact family, payload boundary, telemetry, and fixture contract.
4. `source.go` - the `collector.Source` implementation and scope/generation
   identity.
5. `claimed_source.go` - workflow claim validation and work-item-to-scope
   mapping.
6. `pageprovider.go` - the transport seam, `FixturePageProvider`, and the
   continuation-token resume contract.
7. `config.go` - declarative config and credential-by-name policy.
8. `telemetry.go` - bounded-label metric and structured-log helpers.

## Invariants

- This package contains the explicitly injected `LiveClient` REST seam for
  Cloud Asset Inventory `assets.list`, but it is never wired as a default.
  Fixture mode still uses `FixturePageProvider`; claimed-live command mode may
  inject `LiveClient` only after explicit workflow config. Live-client tests
  must use local HTTP servers, not live Google Cloud calls.
- All transport goes through `PageProvider.FetchPage`. Do not import a Google
  Cloud SDK into `source.go`; transport belongs behind a `PageProvider`.
- Parsed resource labels emit label-backed `gcp_tag_observation` facts through
  `gcpcloud.Generation`; direct/effective GCP tag API collection belongs in a
  later source slice.
- Parsed Cloud Asset Inventory `relatedAsset` fields emit
  `gcp_cloud_relationship` facts through `gcpcloud.Generation`; reducer
  admission and graph projection belong in later slices.
- Parsed Cloud Asset Inventory IAM policy bindings emit
  `gcp_iam_policy_observation` facts through `gcpcloud.Generation`; reducer
  admission and security graph projection belong in later slices.
- Parsed Cloud Asset Inventory DNS record set assets emit `gcp_dns_record`
  facts through `gcpcloud.Generation`; record names and targets must stay
  fingerprinted.
- Parsed Cloud Run service/job runtime image fields emit `gcp_image_reference`
  facts through `gcpcloud.Generation`; container names must stay fingerprinted.
- Reference credentials by NAME only (`ScopeConfig.CredentialRef`). Never store,
  log, or label credential material or names.
- Fence every generation through `gcpcloud.GenerationTracker` before draining
  pages. A stale generation emits a single stale warning and never emits
  resource facts. Re-running the same generation id and fencing token is
  idempotent.
- For claim-driven collection, generation id and fencing token must come from
  the workflow work item, not static collector configuration.
- A continuation token the provider cannot resume becomes a
  `page_token_expired` warning, not a hard failure or silent truncation. Typed
  live provider warnings become `gcp_collection_warning` facts.
- Require a non-zero `redact.Key`. Facts must never carry unkeyed redaction
  markers.
- Metric labels, log fields, and runtime error text are bounded enums and counts
  only: collector kind, claim status, parent scope kind, asset family, content
  family, fact kind, warning kind, and outcome. Never emit resource names,
  project ids, derived scope ids, labels, IAM members, DNS names, image
  references, URLs, credential names, page tokens, or provider response bodies.
- Keep every source file under 500 lines; split before the cap.

## What Not To Change Without An ADR

- Do not wire `LiveClient` (or any live transport) as a default page provider.
- Do not add reducer admission, graph writes, API/MCP readback, Helm values, or
  ServiceMonitor wiring here; those are separate slices.
- Do not infer environment, workload, ownership, or deployable-unit truth from
  names, labels, folders, or project ids.

## Verification

```bash
cd go && go test ./internal/collector/gcpcloud/... ./cmd/collector-gcp-cloud/... -count=1
cd go && golangci-lint run ./internal/collector/gcpcloud/... ./cmd/collector-gcp-cloud/...
scripts/verify-package-docs.sh
scripts/verify-performance-evidence.sh
```
