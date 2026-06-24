// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package azureruntime wires the Azure cloud collector into the shared collector
// runtime. It implements collector.Source for one or more declarative Azure
// scope targets (tenant, subscription, or management group), reads Resource
// Graph inventory or fixture resourcechanges pages through the
// azurecloud.PageProvider seam with $skipToken resume, and yields one
// collector.CollectedGeneration per target for atomic durable commit by the
// shared collector.Service.
//
// It also implements collector.ClaimedSource: NextClaimed resolves one
// already-claimed workflow work item to its authorized scope target and pins the
// coordinator-assigned generation id and fencing token, so the same scan logic
// runs under fixtures, the non-claimed poll loop, and the claim-driven
// collector.ClaimedService runner. An unauthorized scope, mismatched collector
// instance, non-claimed status, non-positive fencing token, or generation/run
// mismatch is rejected before any provider call.
//
// The runtime is the scaffolding slice of the Azure collector: it owns config
// validation, scope and generation identity, deterministic generation
// fingerprints, per-target tracing and logging, and the PageProviderFactory
// seam. It delegates pagination, normalization, redaction, fact emission, and
// bounded telemetry to the parent azurecloud package, and it commits nothing
// itself; the collector.Service owns the durable write boundary.
//
// The live Azure Resource Graph and ARM fallback clients stay behind
// PageProviderFactory and are never the default. LiveProviderFactory is gated by
// construction: its zero value returns ErrLiveProviderGated, while live calls
// require an explicitly injected read-only LiveResourceGraphClient or
// AzureSDKResourceGraphClient. Optional ARM fallback enrichment additionally
// requires an injected LiveARMFallbackClient, exact resource-type allowlist
// rules, fixed API versions, and bounded extension fields; the SDK wrapper
// exposes only GET-by-ID behavior. Tests and offline tooling use
// FixturePageProvider, which serves pre-parsed or file-backed Resource Graph
// inventory pages and pre-parsed resourcechanges pages with no network calls.
// The command and chart paths do not enable live transport.
//
// Resource-change facts are emitted only when TargetConfig.SourceLane is
// azurecloud.SourceLaneResourceChanges and remain provenance-only. Reducer
// admission, graph promotion, API and MCP readback, workflow scheduling,
// Helm/chart wiring, and live transport activation belong outside this runtime
// package; credential-bearing and chart slices remain gated by the Azure cloud
// collector contract. Credentials are referenced by name only in
// TargetConfig.CredentialRef, never inlined. Provider identifiers and credential
// references remain control input only; they must not be copied into telemetry.
package azureruntime
