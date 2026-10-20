// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package detective

import "context"

// Client lists Amazon Detective metadata for one claimed account and region. It
// exposes only the three read-only list APIs the scanner needs: behavior graphs,
// graph members, and graph tags. It reads no investigation, finding-group, or
// indicator data.
type Client interface {
	// ListGraphs returns the behavior graphs the claimed account administers in
	// the boundary region.
	ListGraphs(context.Context) ([]Graph, error)
	// ListMembers returns the member accounts enrolled in one behavior graph,
	// identified by its ARN.
	ListMembers(ctx context.Context, graphARN string) ([]MemberAccount, error)
	// ListTags returns the AWS resource tags for one behavior graph, identified
	// by its ARN.
	ListTags(ctx context.Context, graphARN string) (map[string]string, error)
}

// Graph is the metadata-only scanner view of an Amazon Detective behavior
// graph. The ARN carries the graph's partition, so synthesized identities never
// hardcode a partition.
type Graph struct {
	// ARN is the behavior graph ARN reported by Detective. It is the graph
	// node's stable identity and resource id.
	ARN string
	// CreatedAt is the graph creation timestamp in RFC3339 form, or empty when
	// Detective reports none.
	CreatedAt string
	// GuardDutyDetectorID is the GuardDuty detector id Detective ingests for
	// this graph, when an out-of-band resolver supplies one. Detective's own
	// metadata APIs never report a detector id, so this stays empty for an
	// API-only scan and the GuardDuty data-source edge is omitted rather than
	// dangled.
	GuardDutyDetectorID string
}

// MemberAccount is the metadata-only scanner view of an account enrolled in a
// Detective behavior graph. The member's contact email is intentionally absent:
// it is personal data and is never read into this type.
type MemberAccount struct {
	// AccountID is the enrolled member account's 12-digit AWS account id. It is
	// the join key to the AWS Organizations account node.
	AccountID string
	// AdministratorID is the account id of the graph's administrator account.
	AdministratorID string
	// GraphARN is the ARN of the behavior graph the account is enrolled in.
	GraphARN string
	// Status is the membership status (for example ENABLED, INVITED, or
	// ACCEPTED_BUT_DISABLED).
	Status string
	// InvitationType is how the account joined the graph (for example
	// ORGANIZATION or INVITATION).
	InvitationType string
	// InvitedAt is the invitation timestamp in RFC3339 form, or empty.
	InvitedAt string
	// UpdatedAt is the last-updated timestamp in RFC3339 form, or empty.
	UpdatedAt string
	// DatasourcePackages are the Detective data-source package names enabled for
	// this member (for example DETECTIVE_CORE, which ingests GuardDuty). Only
	// the package names survive; no usage volume or finding content is read.
	DatasourcePackages []string
}
