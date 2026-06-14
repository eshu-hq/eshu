// Package azureruntime wires the fixture-driven Azure cloud collector into the
// shared collector runtime. It implements collector.Source for one or more
// declarative Azure scope targets (tenant, subscription, or management group),
// reads Resource Graph inventory or fixture resourcechanges pages through the
// azurecloud.PageProvider seam with $skipToken resume, and yields one
// collector.CollectedGeneration per target for atomic durable commit by the
// shared collector.Service.
//
// The runtime is the scaffolding slice of the Azure collector: it owns config
// validation, scope and generation identity, deterministic generation
// fingerprints, per-target tracing and logging, and the PageProviderFactory
// seam. It delegates pagination, normalization, redaction, fact emission, and
// bounded telemetry to the parent azurecloud package, and it commits nothing
// itself; the collector.Service owns the durable write boundary.
//
// The live Azure Resource Graph and ARM client stays behind PageProviderFactory
// and is never the default. LiveProviderFactory is an inert documented seam that
// returns ErrLiveProviderGated, so no test or default code path issues a live
// Azure request. Tests and offline tooling use FixturePageProvider, which serves
// pre-parsed or file-backed Resource Graph inventory pages and pre-parsed
// resourcechanges pages with no network calls. A future PR replaces
// LiveProviderFactory with a real read-only adapter proven by its own credential,
// quota, throttle, and fixture gates.
//
// Resource-change facts are emitted only when TargetConfig.SourceLane is
// azurecloud.SourceLaneResourceChanges and remain provenance-only. Reducer
// admission, graph promotion, API and MCP readback, and Helm or chart wiring are
// deferred follow-ups gated by the Azure cloud collector contract; this package
// adds none of them. Credentials are referenced by name only in
// TargetConfig.CredentialRef, never inlined, so configuration is safe to log and
// persist, and no secret or credential name reaches telemetry.
package azureruntime
