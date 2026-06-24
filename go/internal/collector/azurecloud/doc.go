// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package azurecloud implements the fixture-testable Azure cloud collector fact
// engine. It parses Azure Resource Graph Resources API response pages, parses
// fixture Resource Graph resourcechanges pages, normalizes Azure Resource
// Manager (ARM) resource identity, redacts provider-specific extension payloads,
// and emits provider-specific azure_cloud_resource, azure_tag_observation,
// azure_cloud_relationship, azure_identity_observation, azure_resource_change,
// azure_dns_record, azure_image_reference, and azure_collection_warning source
// facts for one bounded subscription, management-group, or tenant shard.
//
// The package owns the durable claim boundary, ARM resource ID normalization,
// skip-token pagination, deterministic and idempotent fact emission, stale and
// invalid generation rejection, partial-scope and truncation warning evidence,
// resource-change actor fingerprinting, tag and identity fingerprinting,
// DNS and image-reference source-lane extraction, extension redaction, and
// bounded-label telemetry. It does not call live Azure Resource Graph or ARM
// APIs (the PageProvider seam is fed by fixtures in this slice), schedule
// workflow claims, choose credentials, commit facts, write graph truth, or
// answer queries.
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
// bodies. Tag, identity, DNS, and container-name values are stored as keyed
// fingerprints where the contract requires redaction; relationship and DNS
// facts stay provenance-only; resource-change facts carry changed property
// paths only, never before/after values or raw response bodies, and delete
// changes are tombstone candidates until reducer-owned evidence confirms final
// state. Telemetry labels are bounded enums only (collector kind, scope kind,
// source lane, operation, status class, fact kind, warning reason) and never
// carry ARM IDs, subscription or tenant IDs, resource group or resource names,
// locations, tags, KQL query text, URLs, or credential names.
package azurecloud
