// Command collector-gcp-cloud runs the fixture-driven GCP Cloud Asset Inventory
// collector runtime scaffolding.
//
// The binary loads a declarative JSON config (collector instance id, poll
// interval, and bounded scopes referencing read-only credentials by name) and a
// read-only redaction key file, constructs a gcpruntime.Source backed by an
// offline FixturePageProvider, and commits each collected generation through the
// shared Postgres ingestion store wrapped by a status committer that records the
// bounded GCP claim metric.
//
// This slice is fixture-driven scaffolding. It performs no live Google Cloud
// call: pages are served from local fixture files only. The live Cloud Asset
// Inventory transport, Helm values, environment-variable contracts, reducer
// admission, and API/MCP readback are deferred slices per
// docs/public/reference/gcp-cloud-collector-contract.md.
package main
