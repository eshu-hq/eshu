// Command collector-azure-cloud runs the fixture-driven Azure cloud collector
// runtime.
//
// The command reads declarative Azure scope targets (tenant, subscription, or
// management group) from environment configuration, builds a non-claimed
// collector.Source over the azurecloud.PageProvider seam, and commits reported
// azure_cloud_resource and azure_collection_warning facts through the shared
// ingestion store. Credentials are referenced by name only in each target's
// credential_ref; no secret value is read from the target configuration.
//
// This is the runtime scaffolding slice of the Azure collector (issue #1998).
// The live Azure Resource Graph and ARM client is a gated seam: with no
// ESHU_AZURE_FIXTURE_PAGES_JSON set the command selects the inert
// LiveProviderFactory, which never issues a live Azure call. A file-backed
// offline provider (ESHU_AZURE_FIXTURE_PAGES_JSON) drives local proof and smoke
// tests. Reducer admission, the remaining Azure fact families, API and MCP
// readback, and Helm or chart wiring are deferred follow-ups gated by the Azure
// cloud collector contract.
package main
