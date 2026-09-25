// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package macie

import "context"

// Client is the Amazon Macie metadata read surface consumed by Scanner. Runtime
// adapters translate AWS SDK responses into these scanner-owned metadata
// records.
//
// The interface is deliberately the highest-redaction read surface in the AWS
// collector. It exposes no sensitive-data finding read, no custom data
// identifier regular-expression read, no allow-list content read, no findings
// filter criteria read, no classification-job bucket-criteria read, and no
// mutation call. Amazon Macie's product is detecting personally identifiable
// information; its finding payloads and custom-identifier regular expressions
// are themselves descriptions of that sensitive data, so they never enter Eshu.
type Client interface {
	// Session returns the Macie account session status for the claimed account
	// and region, or a Session with Enabled false when Macie is not enabled.
	Session(context.Context) (Session, error)
	// AdministratorAccountID returns the delegated administrator account id when
	// the claimed account is a Macie member, or an empty string for a standalone
	// or administrator account.
	AdministratorAccountID(context.Context) (string, error)
	// ListMembers returns the member accounts visible to the claimed account
	// when it is a delegated administrator. It returns an empty slice for a
	// non-administrator account.
	ListMembers(context.Context) ([]MemberAccount, error)
	// ListClassificationJobs returns classification-job metadata. Implementations
	// MUST drop the job bucket-criteria expressions and the explicit bucket list,
	// carrying only the count of buckets the job targets.
	ListClassificationJobs(context.Context) ([]ClassificationJob, error)
	// ListAllowLists returns allow-list identity metadata. Implementations MUST
	// drop allow-list contents (literal text and S3-hosted regex references).
	ListAllowLists(context.Context) ([]AllowList, error)
	// ListCustomDataIdentifiers returns custom data identifier identity metadata.
	// Implementations MUST drop the regular-expression body.
	ListCustomDataIdentifiers(context.Context) ([]CustomDataIdentifier, error)
	// ListFindingsFilters returns findings filter identity metadata.
	// Implementations MUST drop the filter criteria expressions.
	ListFindingsFilters(context.Context) ([]FindingsFilter, error)
	// FindingCountsBySeverity returns the aggregate count of Macie findings
	// grouped by severity label only. It returns an empty map when Macie is not
	// enabled or reports no findings. No finding body, finding type, finding
	// identifier, or affected-resource identity is read.
	FindingCountsBySeverity(context.Context) (map[string]int64, error)
}

// Session is the scanner-owned view of an Amazon Macie account session. It
// carries enablement and coarse configuration metadata only.
type Session struct {
	// Enabled reports whether the account has a Macie session at all. AWS returns
	// a not-enabled error rather than a record when Macie is off; the adapter maps
	// that to Enabled false.
	Enabled bool
	// Status is the Macie session status string (for example ENABLED or PAUSED).
	Status string
	// FindingPublishingFrequency is the coarse cadence Macie publishes policy
	// finding updates with (for example FIFTEEN_MINUTES).
	FindingPublishingFrequency string
	// ServiceRoleARN is the service-linked role ARN Macie uses. It is an
	// AWS-managed role identity, not a credential.
	ServiceRoleARN string
	// CreatedAt is the RFC3339 session creation time, when reported.
	CreatedAt string
	// UpdatedAt is the RFC3339 most-recent session change time, when reported.
	UpdatedAt string
}

// MemberAccount is a metadata-only Amazon Macie member account summary as
// reported by a delegated administrator account. It deliberately omits the
// member email address, which is personal contact data.
type MemberAccount struct {
	AccountID          string
	AdministratorID    string
	RelationshipStatus string
	InvitedAt          string
	UpdatedAt          string
	Tags               map[string]string
}

// ClassificationJob is a metadata-only Amazon Macie sensitive data discovery
// job summary. It carries identity, type, status, and a count of the buckets
// the job targets. It has no field able to hold the job bucket-criteria
// expressions or the explicit bucket list, so those cannot land on the scanner.
type ClassificationJob struct {
	JobID             string
	Name              string
	JobType           string
	JobStatus         string
	CreatedAt         string
	BucketCount       int
	AccountCount      int
	HasBucketCriteria bool
}

// AllowList is an Amazon Macie allow-list identity. It deliberately omits the
// list contents (literal allow text and any S3-hosted regex reference), the
// description, and the status detail, because the contents describe data the
// customer wants Macie to ignore and can themselves encode sensitive patterns.
type AllowList struct {
	ID   string
	Name string
}

// CustomDataIdentifier is an Amazon Macie custom data identifier identity. It
// deliberately omits the regular-expression body, keyword list, and ignore
// words, because those fields ARE the description of the sensitive data the
// customer is detecting and must never be persisted.
type CustomDataIdentifier struct {
	ID   string
	Name string
}

// FindingsFilter is an Amazon Macie findings filter identity. It deliberately
// omits the filter criteria expressions, which encode which findings a customer
// suppresses or surfaces and therefore reveal their detection posture.
type FindingsFilter struct {
	ID     string
	Name   string
	Action string
}
