// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codecommit

import (
	"context"
	"time"
)

// Client is the metadata-only CodeCommit read surface consumed by Scanner.
// Runtime adapters translate AWS SDK responses into these scanner-owned types.
// The interface intentionally exposes no commit, ref, blob, file-content,
// pull-request, comment, or mutation reads so the scanner cannot reach
// repository contents.
type Client interface {
	// ListRepositories returns every CodeCommit repository the configured
	// credentials can see, with full metadata and trigger evidence resolved.
	ListRepositories(context.Context) ([]Repository, error)
}

// Repository is the scanner-owned representation of a CodeCommit repository.
// Only repository metadata is carried; commit history, branch refs, and file
// contents are never read or represented here.
type Repository struct {
	// ARN is the repository Amazon Resource Name reported by AWS. It is the
	// partition-correct identity the scanner uses directly without synthesis.
	ARN string
	// Name is the repository name.
	Name string
	// ID is the system-generated repository id.
	ID string
	// AccountID is the AWS account the repository belongs to, as AWS reports it.
	AccountID string
	// DefaultBranch is the repository default branch name. It is a ref label,
	// not ref content.
	DefaultBranch string
	// CloneURLHTTP is the HTTPS clone URL AWS reports for the repository.
	CloneURLHTTP string
	// CloneURLSSH is the SSH clone URL AWS reports for the repository.
	CloneURLSSH string
	// KMSKeyID is the KMS key id or ARN the repository is encrypted with, as
	// AWS reports it. It may be a bare key id or a full key ARN.
	KMSKeyID string
	// CreatedAt is the repository creation timestamp.
	CreatedAt time.Time
	// LastModifiedAt is the repository last-modified timestamp.
	LastModifiedAt time.Time
	// Triggers are the repository trigger configurations. Trigger destinations
	// drive the repository-to-SNS-topic relationship edges.
	Triggers []Trigger
	// Tags are the repository tags reported by AWS, treated as raw tag evidence.
	Tags map[string]string
}

// Trigger is the scanner-owned representation of a CodeCommit repository
// trigger. Only the destination identity, trigger name, and branch scope are
// carried; trigger custom-data payloads are never persisted.
type Trigger struct {
	// Name is the trigger name.
	Name string
	// DestinationARN is the ARN of the target the trigger notifies (for
	// example an SNS topic ARN). It is the join key for the
	// repository-to-SNS-topic edge.
	DestinationARN string
	// Events are the repository events that fire the trigger.
	Events []string
	// Branches is the branch scope of the trigger. An empty list means the
	// trigger applies to all branches.
	Branches []string
}
