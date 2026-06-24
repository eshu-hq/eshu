// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceMacie identifies the regional Amazon Macie metadata scan slice.
	// Amazon Macie's SDK service id is macie2; the collector service_kind is
	// "macie2" so metric labels match the AWS SDK service identity.
	ServiceMacie = "macie2"
)

const (
	// ResourceTypeMacieSession identifies an Amazon Macie account session
	// status record (enabled or paused) for one account and region. The session
	// is the account-level Macie resource; member accounts attach to it.
	ResourceTypeMacieSession = "aws_macie_session"
	// ResourceTypeMacieClassificationJob identifies an Amazon Macie sensitive
	// data discovery (classification) job. Only job identity, type, status, and
	// a bucket-criteria-summary count are persisted; the job's bucket-criteria
	// expressions and the explicit bucket list are never stored.
	ResourceTypeMacieClassificationJob = "aws_macie_classification_job"
	// ResourceTypeMacieAllowList identifies an Amazon Macie allow list. Only the
	// list identity (id, name) is persisted; allow-list contents (the literal
	// text or S3-hosted regex that suppresses matches) are never stored.
	ResourceTypeMacieAllowList = "aws_macie_allow_list"
	// ResourceTypeMacieCustomDataIdentifier identifies an Amazon Macie custom
	// data identifier. Only the identity (id, name) is persisted; the regular
	// expression body is never stored because it is itself a description of the
	// sensitive data the customer is detecting.
	ResourceTypeMacieCustomDataIdentifier = "aws_macie_custom_data_identifier"
	// ResourceTypeMacieFindingsFilter identifies an Amazon Macie findings
	// filter. Only the identity (id, name, action) is persisted; the filter
	// criteria expressions are never stored.
	ResourceTypeMacieFindingsFilter = "aws_macie_findings_filter"
	// ResourceTypeMacieMemberAccount identifies an Amazon Macie member account
	// reported by a delegated administrator account.
	ResourceTypeMacieMemberAccount = "aws_macie_member_account"
)

const (
	// RelationshipMacieMemberManagedByAdministrator records that an Amazon Macie
	// member account is managed by a delegated administrator account. The edge
	// targets the administrator account's Macie session resource.
	RelationshipMacieMemberManagedByAdministrator = "macie_member_managed_by_administrator"
)
