// Package gcpruntime wires the fixture-driven gcpcloud parser, normalizer,
// redactor, and generation accumulator into a collector.Source for the GCP
// Cloud Asset Inventory collector runtime.
//
// Source.Next yields one collector.CollectedGeneration per configured bounded
// scope. It drains Cloud Asset Inventory pages through the PageProvider seam,
// accumulates gcp_cloud_resource, gcp_tag_observation, and
// gcp_iam_policy_observation, gcp_dns_record, and gcp_collection_warning facts
// in a gcpcloud.Generation, fences the generation with
// gcpcloud.GenerationTracker so a stale scan cannot replace current facts, and
// emits bounded-label telemetry by fact kind.
//
// The PageProvider interface isolates all Cloud Asset Inventory transport.
// FixturePageProvider serves parsed pages from memory or files for tests and the
// binary's offline smoke path; LiveClient is the documented live gRPC/REST seam
// and is intentionally unimplemented and unwired in this slice. No code in this
// package performs a live Google Cloud call, and no test exercises a live call.
//
// This package is runtime scaffolding. Reducer admission, API/MCP readback,
// Helm values, and environment-variable contracts are deferred to later slices
// per docs/public/reference/gcp-cloud-collector-contract.md.
package gcpruntime
