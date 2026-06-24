// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import "time"

// CollectorKind is the durable collector_kind value for Azure cloud facts.
const CollectorKind = "azure"

// Scope kinds bound the Azure shard hierarchy. They are durable enum values
// used in scope identity and bounded telemetry labels, never raw IDs.
const (
	// ScopeKindSubscription bounds a claim to one Azure subscription.
	ScopeKindSubscription = "subscription"
	// ScopeKindManagementGroup bounds a claim to one Azure management group.
	ScopeKindManagementGroup = "management_group"
	// ScopeKindTenant bounds a claim to a tenant-level scope.
	ScopeKindTenant = "tenant"
)

// Source lanes name the bounded provider read path for one observation. They
// are enum values safe for telemetry labels.
const (
	// SourceLaneResourceGraph marks evidence from the Resource Graph Resources
	// API.
	SourceLaneResourceGraph = "resource_graph"
	// SourceLaneResourceChanges marks evidence from Resource Graph
	// resourcechanges.
	SourceLaneResourceChanges = "resource_changes"
	// SourceLaneARMFallback marks evidence from an allowlisted, read-only ARM
	// fallback GET.
	SourceLaneARMFallback = "arm_fallback"
)

// Azure DNS record types supported by source-lane extraction.
const (
	DNSRecordTypeA     = "A"
	DNSRecordTypeAAAA  = "AAAA"
	DNSRecordTypeCNAME = "CNAME"
	DNSRecordTypeMX    = "MX"
	DNSRecordTypeNS    = "NS"
	DNSRecordTypePTR   = "PTR"
	DNSRecordTypeTXT   = "TXT"
	DNSRecordTypeSRV   = "SRV"
	DNSRecordTypeCAA   = "CAA"
)

// Boundary carries the durable scope-generation and claim identity shared by
// every fact emitted for one Azure collection claim. It encodes the scope and
// generation contract fields the Azure collector contract requires.
type Boundary struct {
	// CollectorInstanceID is the configured runtime instance that owns target
	// policy and the credential environment for the claim.
	CollectorInstanceID string
	// TenantID is the Azure tenant ID (or tenant fingerprint) for the shard.
	TenantID string
	// ScopeKind is one of ScopeKindSubscription, ScopeKindManagementGroup, or
	// ScopeKindTenant.
	ScopeKind string
	// ProviderScopeID is the subscription ID, management group ID, or tenant
	// fingerprint kept as source evidence.
	ProviderScopeID string
	// ResourceTypeFamily buckets the resource provider namespace for sharding,
	// for example "microsoft.compute".
	ResourceTypeFamily string
	// LocationBucket buckets the Azure location for sharding, for example
	// "eastus".
	LocationBucket string
	// SourceLane is one of the SourceLane* enum values.
	SourceLane string
	// ScopeID is the durable Eshu scope for the Azure shard.
	ScopeID string
	// GenerationID is the collector- or coordinator-assigned generation for one
	// bounded scan.
	GenerationID string
	// FencingToken fences the durable claim. It must be positive.
	FencingToken int64
	// ObservedAt is the Eshu observation time for the provider response.
	ObservedAt time.Time
}

// ResourceObservation describes one Azure Resource Graph resource row after
// ARM identity normalization. RawExtension carries safe control-plane metadata
// that is redacted before emission; it must never carry object contents,
// secrets, private payloads, or data-plane records.
type ResourceObservation struct {
	Boundary Boundary
	// ARMResourceID is the raw provider identity preserved for exact reducer
	// joins.
	ARMResourceID string
	// Identity is the normalized identity parsed from the ARM resource ID.
	Identity ARMIdentity
	// Kind is the optional Azure resource kind discriminator.
	Kind string
	// SKUClass is the bounded SKU class (tier or name) when present.
	SKUClass string
	// ManagedBy is the ARM ID of an owning resource when present.
	ManagedBy string
	// APIVersion is the provider API version when known.
	APIVersion string
	// ProviderTime is the Resource Graph read or change time. Nil means the
	// provider did not report a time and the absence is explicit.
	ProviderTime *time.Time
	// Tags carries Resource Graph tags within the configured evidence retention
	// boundary. Sensitive tag values are redacted by the envelope builder.
	Tags map[string]string
	// HasIdentity reports whether the resource exposes a managed identity.
	HasIdentity bool
	// RawExtension is the provider-specific control-plane metadata object. It is
	// redacted before persistence.
	RawExtension map[string]any
	// SourceRecordID overrides the default Resource Graph record id.
	SourceRecordID string
	// SourceURI is the bounded Resource Graph source URI.
	SourceURI string
}

// WarningObservation describes one explicit Azure collection coverage outcome:
// partial scope, permission-hidden subscription, truncation, throttling,
// fallback skip, stale, unsupported, or redaction warning.
type WarningObservation struct {
	Boundary Boundary
	// WarningKind is a bounded enum from the Warning* constants.
	WarningKind string
	// Outcome is a bounded enum from the Outcome* constants.
	Outcome string
	// Retryable reports whether the condition may resolve on retry.
	Retryable bool
	// HiddenResourceCount counts resources the principal could not read in the
	// scope, when known.
	HiddenResourceCount int
	// Message is an operator-facing detail. It is sanitized before persistence.
	Message string
	// SourceRecordID overrides the default warning record id.
	SourceRecordID string
	// SourceURI is the bounded source URI.
	SourceURI string
}

// Warning kinds bound the azure_collection_warning evidence taxonomy.
const (
	// WarningPartialScope marks a scope where only part of the configured
	// subscriptions or management groups were readable.
	WarningPartialScope = "partial_scope"
	// WarningPermissionHidden marks subscriptions or resources hidden from the
	// principal by Azure RBAC.
	WarningPermissionHidden = "permission_hidden"
	// WarningResultTruncated marks a Resource Graph response that set
	// resultTruncated.
	WarningResultTruncated = "result_truncated"
	// WarningThrottled marks a scope where Resource Graph quota throttling
	// forced backoff.
	WarningThrottled = "throttled"
	// WarningFallbackSkipped marks an ARM fallback that was skipped because the
	// resource family was not allowlisted.
	WarningFallbackSkipped = "fallback_skipped"
	// WarningStale marks evidence older than the configured freshness window.
	WarningStale = "stale"
	// WarningUnsupported marks a resource family or evidence type the source
	// does not expose.
	WarningUnsupported = "unsupported"
	// WarningRedaction marks a payload where extension fields were redacted.
	WarningRedaction = "redaction"
)

// Outcomes bound the warning resolution state.
const (
	// OutcomePartial means the scope produced partial evidence.
	OutcomePartial = "partial"
	// OutcomeStale means the scope produced stale evidence.
	OutcomeStale = "stale"
	// OutcomeUnavailable means the scope produced no current evidence.
	OutcomeUnavailable = "unavailable"
	// OutcomeUnsupported means the evidence type is not exposed.
	OutcomeUnsupported = "unsupported"
)
