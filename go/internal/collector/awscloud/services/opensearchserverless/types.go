// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package opensearchserverless

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon OpenSearch Serverless observations for
// one AWS claim. Implementations read control-plane describe/list APIs only and
// never read the OpenSearch HTTP data plane or persist policy document bodies.
type Client interface {
	// Snapshot returns every OpenSearch Serverless collection, security policy,
	// and managed VPC endpoint visible to the configured AWS credentials, plus the
	// encryption-policy-to-KMS bindings resolved for collection encryption edges.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures OpenSearch Serverless metadata plus non-fatal scan warnings.
type Snapshot struct {
	// Collections is the metadata-only set of OpenSearch Serverless collections.
	Collections []Collection
	// SecurityPolicies is the metadata-only set of OpenSearch Serverless security
	// policies (encryption, network). Policy document bodies are never carried.
	SecurityPolicies []SecurityPolicy
	// VPCEndpoints is the metadata-only set of OpenSearch Serverless managed
	// interface VPC endpoints.
	VPCEndpoints []VPCEndpoint
	// EncryptionKeyBindings carries the collection-resource-pattern-to-KMS-key
	// bindings parsed from encryption security policies. The scanner matches
	// collection names against these patterns to emit collection-to-KMS edges
	// without persisting any policy document body.
	EncryptionKeyBindings []EncryptionKeyBinding
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Collection is the scanner-owned OpenSearch Serverless collection model. It
// carries control-plane metadata only and intentionally excludes the collection
// endpoint, dashboard endpoint, saved-object bodies, and indexed data.
type Collection struct {
	// ARN is the Amazon Resource Name that uniquely identifies the collection.
	ARN string
	// ID is the unique collection identifier.
	ID string
	// Name is the collection name.
	Name string
	// Type is the collection type (for example SEARCH, TIMESERIES, VECTORSEARCH).
	Type string
	// Status is the current collection lifecycle status (for example ACTIVE).
	Status string
	// StandbyReplicas reports whether standby replicas are ENABLED or DISABLED.
	StandbyReplicas string
	// KMSKeyARN is the customer-managed KMS key ARN AWS resolved for the
	// collection, when reported directly on the collection record. The
	// authoritative encryption edge is derived from the matching encryption
	// policy; this field is recorded as collection metadata only.
	KMSKeyARN string
	// CreatedDate is when the collection was created.
	CreatedDate time.Time
	// LastModifiedDate is when the collection was last modified.
	LastModifiedDate time.Time
	// Tags carries the collection resource tags.
	Tags map[string]string
}

// SecurityPolicy is the scanner-owned OpenSearch Serverless security policy
// summary. It carries policy identity and lifecycle metadata only. The policy
// document body (which can encode resource patterns, SAML metadata, and access
// rules) is never carried on this model.
type SecurityPolicy struct {
	// Name is the security policy name.
	Name string
	// Type is the security policy type (encryption or network).
	Type string
	// PolicyVersion is the opaque policy version token.
	PolicyVersion string
	// Description is the optional human description of the policy.
	Description string
	// CreatedDate is when the policy was created.
	CreatedDate time.Time
	// LastModifiedDate is when the policy was last modified.
	LastModifiedDate time.Time
}

// VPCEndpoint is the scanner-owned OpenSearch Serverless managed interface VPC
// endpoint model. It carries control-plane metadata and the VPC, subnet, and
// security-group references AWS reports as bare EC2 ids.
type VPCEndpoint struct {
	// ID is the unique endpoint identifier (a bare vpce-… id).
	ID string
	// Name is the endpoint name.
	Name string
	// Status is the current endpoint lifecycle status.
	Status string
	// VPCID is the bare vpc-… id the endpoint is attached to.
	VPCID string
	// SubnetIDs are the bare subnet-… ids the endpoint spans.
	SubnetIDs []string
	// SecurityGroupIDs are the bare sg-… ids attached to the endpoint.
	SecurityGroupIDs []string
	// CreatedDate is when the endpoint was created.
	CreatedDate time.Time
}

// EncryptionKeyBinding is the scanner-owned projection of an encryption security
// policy: the customer-managed KMS key ARN and the collection name patterns the
// policy assigns it to. The raw policy document body is never carried. Only the
// resource patterns (needed to key the collection-to-KMS edge) and the KMS ARN
// survive parsing.
type EncryptionKeyBinding struct {
	// PolicyName is the encryption policy that produced this binding, recorded on
	// the emitted edge for provenance.
	PolicyName string
	// KMSKeyARN is the customer-managed KMS key ARN the policy assigns. It is
	// empty for AWS-owned-key policies, which produce no collection-to-KMS edge.
	KMSKeyARN string
	// CollectionPatterns are the trimmed collection name patterns the policy
	// matches, each derived from a "collection/<name|prefix*>" resource entry with
	// the "collection/" prefix stripped. A trailing "*" denotes a prefix match.
	CollectionPatterns []string
}
