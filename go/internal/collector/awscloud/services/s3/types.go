// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package s3

import (
	"context"
	"time"
)

// Client lists metadata-only S3 bucket observations for one AWS claim.
type Client interface {
	ListBuckets(ctx context.Context) ([]Bucket, error)
}

// Bucket is the scanner-owned S3 bucket model. It contains bucket control-plane
// metadata only and intentionally excludes object inventory, policies, ACL
// grants, lifecycle rules, replication rules, and notification payloads.
//
// The policy-derived fields (PolicyPresent, PolicyGrantsPublic,
// PolicyGrantsCrossAccount, and ExternalPrincipalGrants) and Replication
// presence carry only derived metadata. The raw bucket policy document, its
// statements, and replication rule detail are never stored on this model.
type Bucket struct {
	ARN               string
	Name              string
	Region            string
	CreationTime      time.Time
	Tags              map[string]string
	Versioning        Versioning
	Encryption        Encryption
	PublicAccessBlock PublicAccessBlock
	PolicyIsPublic    *bool
	OwnershipControls []string
	Website           Website
	Logging           Logging
	Replication       Replication

	// PolicyPresent reports whether the bucket has an attached resource policy.
	PolicyPresent bool
	// PolicyGrantsPublic is the derived boolean for a policy statement granting
	// access to a public principal ("*" / anonymous). Nil when no policy is
	// present or the grant could not be derived from the policy document.
	PolicyGrantsPublic *bool
	// PolicyGrantsCrossAccount is the derived boolean for a policy statement
	// granting access to a principal outside the bucket-owner account. Nil when
	// no policy is present or the grant could not be derived.
	PolicyGrantsCrossAccount *bool
	// ExternalPrincipalGrants is a metadata-only projection of bucket-policy
	// principals that are public, cross-account, AWS service principals, or
	// unsupported principal types. It never carries raw policy JSON, statement
	// bodies, actions, resources, conditions, ACL grants, object keys, or object
	// data.
	ExternalPrincipalGrants []ExternalPrincipalGrant
	// ResourcePolicyStatements is a normalized, derived projection of the bucket
	// policy's statements: one entry per statement carrying effect, normalized
	// action/resource patterns, condition key/operator NAMES, and derived grantee
	// principal facts. It is the resource-side analog of the IAM permission
	// statement projection. It never carries raw policy JSON, statement
	// Sid/bodies, or condition VALUES; the SDK adapter discards the raw document
	// after deriving these fields.
	ResourcePolicyStatements []ResourcePolicyStatement
}

// ResourcePolicyStatement is one normalized, metadata-only statement derived by
// the SDK adapter from a transient bucket-policy parse. It mirrors the IAM
// permission statement shape: effect, normalized actions/resources, condition
// key/operator NAMES, and derived grantee-principal facts. The StatementSID is
// retained only so the emitted fact's source-record id stays stable; it is never
// written into the persisted payload. Condition VALUES, the raw statement body,
// and the raw policy document are never represented here.
type ResourcePolicyStatement struct {
	StatementSID        string
	Effect              string
	Actions             []string
	NotActions          []string
	Resources           []string
	NotResources        []string
	ConditionKeys       []string
	ConditionOperators  []string
	PrincipalAccountIDs []string
	PrincipalARNs       []string
	PrincipalTypes      []string
	IsPublic            bool
	IsCrossAccount      bool
}

// ExternalPrincipalGrant describes one metadata-only bucket-policy principal
// grant derived by the SDK adapter from a transient policy parse. Exact AWS
// identities are retained only for public, cross-account, and service
// principals; unsupported principal types retain the type key rather than the
// raw identifier.
type ExternalPrincipalGrant struct {
	PrincipalKind      string
	PrincipalValue     string
	PrincipalAccountID string
	PrincipalPartition string
	PrincipalService   string
	GrantOutcome       string
	Public             bool
	CrossAccount       bool
	ServicePrincipal   bool
	Unsupported        bool
	UnsupportedKey     string
	SourceStatementID  string
}

// Replication carries only the presence of a bucket replication configuration.
// Replication rule detail (destination buckets, filters, KMS keys) is not
// stored.
type Replication struct {
	Enabled bool
}

// Versioning carries the safe bucket versioning state returned by S3.
type Versioning struct {
	Status    string
	MFADelete string
}

// Encryption carries default bucket encryption metadata without object data.
type Encryption struct {
	Rules []EncryptionRule
}

// EncryptionRule is one default encryption rule for a bucket.
type EncryptionRule struct {
	Algorithm      string
	KMSMasterKeyID string
	BucketKey      bool
}

// PublicAccessBlock carries bucket-level public access block booleans. Nil
// values mean the setting was absent from the AWS response.
type PublicAccessBlock struct {
	BlockPublicACLs       *bool
	IgnorePublicACLs      *bool
	BlockPublicPolicy     *bool
	RestrictPublicBuckets *bool
}

// Website carries only website-status shape, not object key names.
type Website struct {
	Enabled               bool
	HasIndexDocument      bool
	HasErrorDocument      bool
	RedirectAllRequestsTo string
	RoutingRuleCount      int
}

// Logging carries the bucket server-access-log target metadata. Target grants
// and object-key formats are intentionally excluded.
type Logging struct {
	Enabled      bool
	TargetBucket string
	TargetPrefix string
}
