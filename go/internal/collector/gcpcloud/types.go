// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import "time"

// CollectorKind is the durable collector_kind value for GCP cloud facts.
const CollectorKind = "gcp"

// ParentScopeKind enumerates the Cloud Asset Inventory parent scope kinds a GCP
// collector claim can shard by. Values are bounded enums safe for telemetry
// labels and scope identity; they never carry provider numbers or names.
type ParentScopeKind string

const (
	// ParentScopeOrganization is an organization-level CAI parent scope.
	ParentScopeOrganization ParentScopeKind = "organization"
	// ParentScopeFolder is a folder-level CAI parent scope.
	ParentScopeFolder ParentScopeKind = "folder"
	// ParentScopeProject is a project-level CAI parent scope.
	ParentScopeProject ParentScopeKind = "project"
)

// Valid reports whether the parent scope kind is one of the bounded enum values.
func (p ParentScopeKind) Valid() bool {
	switch p {
	case ParentScopeOrganization, ParentScopeFolder, ParentScopeProject:
		return true
	default:
		return false
	}
}

// Boundary carries the durable scope-generation and claim identity shared by all
// facts emitted for one GCP collector claim.
//
// ParentScopeKind, ParentScopeID, and the asset/content family identify the
// bounded shard; ScopeID is the stable Eshu scope key; GenerationID and
// FencingToken fence the bounded scan so duplicate or stale deliveries converge
// or are rejected rather than corrupting current facts.
type Boundary struct {
	// CollectorInstanceID is the configured runtime instance that owns target
	// policy and credential environment.
	CollectorInstanceID string
	// ParentScopeKind is the bounded CAI parent scope kind for the shard.
	ParentScopeKind ParentScopeKind
	// ParentScopeID is the provider parent identifier (organization number,
	// folder number, or project id). It is source evidence, not a telemetry label.
	ParentScopeID string
	// AssetTypeFamily is the bounded asset family for the shard, for example
	// "compute" or "storage". It is derived from the CAI asset type service
	// segment and is safe as a telemetry label.
	AssetTypeFamily string
	// ContentFamily is the bounded CAI content family for the shard, for example
	// "resource" or "iam_policy".
	ContentFamily string
	// LocationBucket is the bounded location bucket for the shard, for example a
	// region or "global". It is safe as a telemetry label.
	LocationBucket string
	// ScopeID is the stable Eshu scope for the parent scope and shard.
	ScopeID string
	// GenerationID is the collector- or coordinator-assigned id for one bounded
	// scan.
	GenerationID string
	// FencingToken fences the claim so stale generations cannot replace current
	// facts. It must be positive.
	FencingToken int64
	// ReadTime is the Cloud Asset Inventory snapshot or response read time.
	ReadTime time.Time
	// ObservedAt is the Eshu observation time. When zero the envelope builder
	// stamps the current UTC time.
	ObservedAt time.Time
}

// ResourceObservation describes one Cloud Asset Inventory resource asset before
// normalization and redaction.
//
// Name is the CAI full resource name (raw provider identity preserved verbatim
// for exact reducer joins). AssetType is the CAI asset type. Ancestors is the
// ordered CAI ancestor chain (most specific first). Labels carry author-set
// resource labels; LabelFingerprintKeys names label keys whose values must be
// fingerprinted rather than preserved. IAMPolicyBindings carries parsed IAM
// role bindings without raw policy JSON. Relationships carries parsed provider
// relationship evidence from CAI relatedAsset fields. DNSRecords carries parsed
// Cloud DNS record sets without raw provider resource data. ImageReferences
// carries parsed runtime container image references from bounded Cloud Run
// service/job metadata. ExtensionVersion and Extension carry a versioned,
// redacted provider-specific extension object. ServiceAccountEmail is retained
// only for iam.googleapis.com/ServiceAccount assets so the GCP trust layer can
// build redaction-safe target digests; builders must not persist it raw. The
// builder never accepts raw IAM policy JSON, secret values, or data-plane
// records here.
type ResourceObservation struct {
	Name                string
	AssetType           string
	DisplayName         string
	State               string
	Location            string
	Ancestors           []string
	Labels              map[string]string
	LabelFingerprint    map[string]string
	IAMPolicyBindings   []IAMPolicyBindingObservation
	Relationships       []RelationshipObservation
	DNSRecords          []DNSRecordObservation
	ImageReferences     []ImageReferenceObservation
	ServiceAccountEmail string
	UpdateTime          time.Time
	ExtensionVersion    string
	Extension           map[string]any
	SourceURI           string
	SourceRecordID      string
}

// IAMPolicyBindingObservation is one parsed Cloud Asset Inventory IAM binding
// attached to a resource observation. Members and the compact condition
// fingerprint input remain in memory until envelope construction fingerprints
// them; raw policy JSON is never kept or emitted.
type IAMPolicyBindingObservation struct {
	Role                      string
	Members                   []string
	ConditionPresent          bool
	ConditionFingerprintInput string
	Etag                      string
}

// WarningObservation describes one explicit GCP collection coverage outcome:
// partial scope, unsupported content, permission-hidden resources, quota
// throttling, stale generation, or redaction. It becomes a gcp_collection_warning
// fact so coverage gaps are durable evidence, not silent success.
type WarningObservation struct {
	Boundary       Boundary
	WarningKind    string
	Outcome        string
	Reason         string
	Retryable      bool
	HiddenCount    int
	SourceURI      string
	SourceRecordID string
}
