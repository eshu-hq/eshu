// Package gcpcloud builds durable source facts for the GCP cloud collector from
// Cloud Asset Inventory (CAI) responses.
//
// The package is fixture-driven and provider-agnostic at the transport layer:
// callers parse CAI assets.list or searchAllResources JSON into observations and
// the package normalizes, redacts, and emits gcp_cloud_resource and
// gcp_collection_warning facts. It never calls Google Cloud APIs, never writes
// graph truth, and never persists raw IAM policy bodies, secret values, object
// contents, public or private IP addresses, startup scripts, or other data-plane
// records. Reducers own canonical CloudResource identity, drift, relationship
// edges, and API/MCP truth.
//
// The durable claim boundary is explicit: collector instance, parent scope kind
// and id, asset and content family, location bucket, scope id, generation id,
// and a positive fencing token. Raw provider identity (the CAI full resource
// name) is preserved verbatim for exact reducer joins alongside normalized asset
// type, project id/number, folder and organization ancestors, and location.
// Stable fact keys derive from fact kind, full resource name, asset type,
// content family, and provider update time so duplicate delivery converges and a
// stale generation is rejected rather than replacing current facts.
//
// This is the first GCP collector slice. It covers resource inventory and
// collection-warning evidence and the scoped telemetry contract. The
// claim-driven runtime binary, reducer admission, API/MCP readback, Helm values,
// and the tag, IAM, relationship, DNS, and image-reference fact families are
// documented follow-ups and are intentionally not implemented here. See
// docs/public/reference/gcp-cloud-collector-contract.md for the full contract.
package gcpcloud
