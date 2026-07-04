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
7. `tagprovider.go` - the direct/effective Resource Manager tag API seam.
8. `config.go` - declarative config and credential-by-name policy.
9. `telemetry.go` - bounded-label metric and structured-log helpers.

## Invariants

- This package contains the explicitly injected `LiveClient` REST seam for
  Cloud Asset Inventory `assets.list`, but it is never wired as a default.
  Fixture mode still uses `FixturePageProvider`; claimed-live command mode may
  inject `LiveClient` only after explicit workflow config. Live-client tests
  must use local HTTP servers, not live Google Cloud calls, with exactly one
  documented exception below.
- **Scoped exception:** `liveclient_smoke_test.go`
  (`TestLiveSmokeCloudAssetInventory`) is a deliberate, env-gated live-smoke
  test that is the proof artifact for the #1997/#2644 live-collector
  enablement gate. It skips unless `ESHU_GCP_LIVE_SMOKE=1` is set, it never
  runs in CI (the environment gate is absent there), and it bounds its scan at
  the `PageProvider` seam so an operator run against a real org stays
  cost-bounded. This is the only live-client test allowed in this package;
  every other live-client test must stay `httptest`-based per the rule above.
- CAI transport goes through `PageProvider.FetchPage`; direct/effective
  Resource Manager tag API transport goes through `TagProvider.FetchTagPage`.
  Do not import a Google Cloud SDK into `source.go`; transport belongs behind
  these provider seams.
- Parsed resource labels emit label-backed `gcp_tag_observation` facts through
  `gcpcloud.Generation`. Direct/effective tag APIs are scope opt-ins only, and
  tag values must be fingerprinted before fact emission.
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
