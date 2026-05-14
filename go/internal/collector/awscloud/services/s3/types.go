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
