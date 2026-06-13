# AGENTS.md - internal/collector/gcpcloud/gcpruntime guidance

## Read First

1. `README.md` - package responsibilities and the PageProvider seam.
2. `../AGENTS.md` and `../README.md` - the gcpcloud source-fact contract and
   invariants this runtime inherits.
3. `docs/public/reference/gcp-cloud-collector-contract.md` - scope/generation,
   fact family, payload boundary, telemetry, and fixture contract.
4. `source.go` - the `collector.Source` implementation and scope/generation
   identity.
5. `pageprovider.go` - the transport seam, `FixturePageProvider`, and the
   continuation-token resume contract.
6. `config.go` - declarative config and credential-by-name policy.
7. `telemetry.go` - bounded-label metric and structured-log helpers.

## Invariants

- This package performs no live Google Cloud call. The live transport is the
  `LiveClient` seam; it is unimplemented and never wired as a default. Tests
  always use `FixturePageProvider`.
- All transport goes through `PageProvider.FetchPage`. Do not import a Google
  Cloud SDK into `source.go`; new transport is a new `PageProvider`.
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
- Reference credentials by NAME only (`ScopeConfig.CredentialRef`). Never store,
  log, or label credential material or names.
- Fence every generation through `gcpcloud.GenerationTracker` before draining
  pages. A stale generation emits a single stale warning and never emits
  resource facts. Re-running the same generation id and fencing token is
  idempotent.
- A continuation token the provider cannot resume becomes a
  `page_token_expired` warning, not a hard failure or silent truncation.
- Require a non-zero `redact.Key`. Facts must never carry unkeyed redaction
  markers.
- Metric labels and log fields are bounded enums and counts only: collector
  kind, claim status, parent scope kind, asset family, content family, fact
  kind, warning kind, and outcome. Never emit resource names, project ids,
  labels, IAM members, DNS names, URLs, or credential names.
- Keep every source file under 500 lines; split before the cap.

## What Not To Change Without An ADR

- Do not wire `LiveClient` (or any live transport) as a default page provider in
  this slice.
- Do not add reducer admission, graph writes, API/MCP readback, Helm values, or
  environment-variable contracts here; those are deferred slices.
- Do not infer environment, workload, ownership, or deployable-unit truth from
  names, labels, folders, or project ids.

## Verification

```bash
cd go && go test ./internal/collector/gcpcloud/... ./cmd/collector-gcp-cloud/... -count=1
cd go && golangci-lint run ./internal/collector/gcpcloud/... ./cmd/collector-gcp-cloud/...
scripts/verify-package-docs.sh
scripts/verify-performance-evidence.sh
```
