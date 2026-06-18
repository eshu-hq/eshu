// Command collector-azure-cloud runs the Azure cloud collector runtime in either
// fixture or claimed-live mode.
//
// In fixture mode (the default), the command reads declarative Azure scope
// targets (tenant, subscription, or management group) from environment
// configuration, builds a non-claimed collector.Source over the
// azurecloud.PageProvider seam, and commits reported azure_cloud_resource and
// azure_collection_warning facts through the shared ingestion store. With no
// ESHU_AZURE_FIXTURE_PAGES_JSON set the command selects the zero-value
// LiveProviderFactory, which returns ErrLiveProviderGated and never issues a
// live Azure call; a file-backed offline provider (ESHU_AZURE_FIXTURE_PAGES_JSON)
// drives local proof and smoke tests.
//
// In claimed-live mode (-mode claimed-live), the command requires an explicit,
// enabled, claim-enabled Azure collector instance with live_collection_enabled=true,
// resolves the ambient read-only Azure credential, wires the official Resource
// Graph SDK client behind the LiveProviderFactory, and runs through
// collector.ClaimedService so claim acquire, heartbeat, fenced commit, retry, and
// terminal failure behavior follow the shared workflow lifecycle. The
// coordinator-assigned generation and fencing token come from each claimed work
// item, and the read-only redaction key is required from -redaction-key-file so
// live tag observation never runs unkeyed.
//
// Credentials are referenced by name only in each target's credential_ref; no
// secret value is read from the configuration. Live smoke proof, Helm chart
// activation, reducer admission, and API/MCP readback remain gated per
// docs/public/reference/azure-cloud-collector-contract.md.
package main
