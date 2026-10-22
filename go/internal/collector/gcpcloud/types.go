// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import "time"

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
	// Attributes is the bounded, redaction-safe typed-depth field set extracted
	// from the CAI resource data blob by the registered per-asset-type extractor.
	// It mirrors the AWS ResourceObservation attribute contract: only safe
	// control-plane metadata usable for Terraform, edges, correlation, or
	// monitoring, never secrets, data-plane content, or raw response bodies.
	Attributes map[string]any
	// CorrelationAnchors are cross-source join keys (parent resource names, KMS
	// key names, and similar resource identifiers) the extractor surfaces so
	// downstream correlation can match this resource against IaC, Terraform
	// state, container image identity, and secrets/IAM evidence.
	CorrelationAnchors []string
	SourceURI          string
	SourceRecordID     string
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
