// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeguru

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon CodeGuru observations for one AWS claim.
// Implementations read control-plane metadata through CodeGuru Reviewer
// (ListRepositoryAssociations) and CodeGuru Profiler (ListProfilingGroups) and
// never read recommendation content, code-review findings, profiling sample
// data, flame graphs, or agent telemetry.
type Client interface {
	// Snapshot returns every CodeGuru Reviewer repository association and
	// CodeGuru Profiler profiling group visible to the configured AWS
	// credentials, carrying control-plane metadata only.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures CodeGuru Reviewer association and Profiler profiling-group
// metadata plus non-fatal scan warnings.
type Snapshot struct {
	// RepositoryAssociations is the metadata-only set of CodeGuru Reviewer
	// repository associations.
	RepositoryAssociations []RepositoryAssociation
	// ProfilingGroups is the metadata-only set of CodeGuru Profiler profiling
	// groups.
	ProfilingGroups []ProfilingGroup
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// RepositoryAssociation is the scanner-owned CodeGuru Reviewer repository
// association model. It carries control-plane metadata only and intentionally
// excludes code-review findings, recommendation text, and analyzed source
// content.
type RepositoryAssociation struct {
	// ARN is the Amazon Resource Name that uniquely identifies the association.
	ARN string
	// AssociationID is the CodeGuru Reviewer repository association id.
	AssociationID string
	// Name is the associated repository name.
	Name string
	// Owner is the repository owner. For a CodeCommit repository AWS reports the
	// owning account id; for GitHub/Bitbucket/S3 it reports the account username
	// or account id.
	Owner string
	// ProviderType is the source provider (CodeCommit, GitHub, Bitbucket,
	// GitHubEnterpriseServer, or S3Bucket).
	ProviderType string
	// State is the association lifecycle state (for example Associated).
	State string
	// ConnectionARN is the AWS CodeStar Connections connection ARN backing a
	// third-party (GitHub/Bitbucket) association, when reported. It is recorded
	// as an attribute reference, not promoted to an edge.
	ConnectionARN string
	// S3BucketName is the S3 bucket name backing an S3Bucket-provider
	// association, when reported. It is recorded as an attribute reference only.
	S3BucketName string
	// KMSKeyID is the customer-managed KMS key id AWS reports for the
	// association, when the encryption option is customer-managed.
	KMSKeyID string
	// EncryptionOption is the reported encryption option (AWS_OWNED_CMK or
	// CUSTOMER_MANAGED_CMK).
	EncryptionOption string
	// CreatedAt is when the association was created.
	CreatedAt time.Time
	// LastUpdatedAt is when the association was last updated.
	LastUpdatedAt time.Time
	// Tags carries the association resource tags.
	Tags map[string]string
}

// ProfilingGroup is the scanner-owned CodeGuru Profiler profiling group model.
// It carries control-plane metadata only and intentionally excludes profiling
// samples, aggregated profiles, flame graphs, recommendation reports, and agent
// telemetry.
type ProfilingGroup struct {
	// ARN is the Amazon Resource Name that uniquely identifies the profiling
	// group.
	ARN string
	// Name is the profiling group name.
	Name string
	// ComputePlatform is the reported compute platform the profiled application
	// runs on (Default for EC2/on-premises/other, or AWSLambda). It is recorded
	// as a resource attribute; CodeGuru does not report a structured compute
	// resource identifier to key an edge on.
	ComputePlatform string
	// ProfilingEnabled reports whether the profiling agent is orchestrated to
	// profile the application. It is nil when AWS reports no orchestration
	// configuration.
	ProfilingEnabled *bool
	// CreatedAt is when the profiling group was created.
	CreatedAt time.Time
	// LastUpdatedAt is when the profiling group was last updated.
	LastUpdatedAt time.Time
	// Tags carries the profiling group resource tags.
	Tags map[string]string
}
