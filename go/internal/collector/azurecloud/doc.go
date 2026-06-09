// Package azurecloud implements the first fixture-testable slice of the Azure
// cloud collector. It parses Azure Resource Graph Resources API response pages,
// normalizes Azure Resource Manager (ARM) resource identity, redacts
// provider-specific extension payloads, and emits provider-specific
// azure_cloud_resource and azure_collection_warning source facts for one
// bounded subscription, management-group, or tenant shard.
//
// The package owns the durable claim boundary, ARM resource ID normalization,
// skip-token pagination, deterministic and idempotent fact emission, stale and
// invalid generation rejection, partial-scope and truncation warning evidence,
// extension redaction, and bounded-label telemetry. It does not call live Azure
// Resource Graph or ARM APIs (the PageProvider seam is fed by fixtures in this
// slice), schedule workflow claims, choose credentials, commit facts, write
// graph truth, or answer queries.
//
// Reducers own canonical CloudResource identity, drift, unmanaged-resource
// detection, relationship graph writes, and API or MCP truth, per the Azure
// cloud collector contract. Every emitted fact uses source_confidence=reported
// because it models provider control-plane evidence, and carries the redaction
// policy version so downstream consumers can prove which redaction policy
// produced a payload.
//
// Payload boundaries follow the contract: the raw ARM resource ID is preserved
// for exact reducer joins, normalized identity fields are added, and the
// provider extension object is redacted so it never carries deployment
// templates, secret or Key Vault values, connection strings, access keys,
// tokens, IP addresses, private endpoint hostnames, or provider response
// bodies. Telemetry labels are bounded enums only (collector kind, scope kind,
// source lane, operation, status class, fact kind, warning reason) and never
// carry ARM IDs, subscription or tenant IDs, resource group or resource names,
// locations, tags, KQL query text, URLs, or credential names.
package azurecloud
