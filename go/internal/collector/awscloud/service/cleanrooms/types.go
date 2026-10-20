// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cleanrooms

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS Clean Rooms collaboration, configured
// table, and membership observations for one AWS claim. Implementations read
// control-plane metadata through the Clean Rooms management APIs and never read
// or persist analysis-rule SQL, query bodies, allowed-column names, or member
// secrets.
type Client interface {
	// Snapshot returns every Clean Rooms collaboration, configured table, and
	// membership visible to the configured AWS credentials in this boundary.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Clean Rooms collaboration, configured-table, and membership
// metadata plus non-fatal scan warnings.
type Snapshot struct {
	// Collaborations is the metadata-only set of Clean Rooms collaborations.
	Collaborations []Collaboration
	// ConfiguredTables is the metadata-only set of Clean Rooms configured
	// tables, each carrying its backing-table reference for edge resolution.
	ConfiguredTables []ConfiguredTable
	// Memberships is the metadata-only set of Clean Rooms memberships.
	Memberships []Membership
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Collaboration is the scanner-owned Clean Rooms collaboration model. It carries
// control-plane metadata only.
type Collaboration struct {
	// ARN is the Amazon Resource Name that uniquely identifies the collaboration.
	ARN string
	// ID is the Clean Rooms collaboration identifier.
	ID string
	// Name is the human-readable collaboration display name.
	Name string
	// CreatorAccountID is the AWS account id of the collaboration creator.
	CreatorAccountID string
	// CreatorDisplayName is the display name of the collaboration creator.
	CreatorDisplayName string
	// MemberStatus is the caller's member status within the collaboration.
	MemberStatus string
	// AnalyticsEngine is the reported analytics engine (for example SPARK).
	AnalyticsEngine string
	// CreateTime is when the collaboration was created.
	CreateTime time.Time
	// UpdateTime is when the collaboration metadata was last updated.
	UpdateTime time.Time
	// Tags carries the collaboration resource tags.
	Tags map[string]string
}

// ConfiguredTable is the scanner-owned Clean Rooms configured-table model. It
// carries control-plane metadata only and intentionally excludes analysis-rule
// SQL, query bodies, and the allowed-column names (only their count is kept).
type ConfiguredTable struct {
	// ARN is the Amazon Resource Name that uniquely identifies the configured
	// table.
	ARN string
	// ID is the Clean Rooms configured-table identifier.
	ID string
	// Name is the configured-table name.
	Name string
	// AnalysisMethod is the reported analysis method (for example DIRECT_QUERY).
	AnalysisMethod string
	// AnalysisRuleTypes are the analysis-rule type names associated with the
	// configured table. These are type labels (for example AGGREGATION), not
	// rule bodies or SQL.
	AnalysisRuleTypes []string
	// AllowedColumnCount is the number of allowed columns AWS reports for the
	// configured table. The column names themselves are intentionally not
	// persisted to stay metadata-only.
	AllowedColumnCount int
	// TableReferenceKind names the backing-table source kind (glue, athena, or
	// snowflake) the configured table represents.
	TableReferenceKind string
	// GlueDatabaseName is the Glue database name when the backing table is a Glue
	// table, empty otherwise.
	GlueDatabaseName string
	// GlueTableName is the Glue table name when the backing table is a Glue
	// table, empty otherwise.
	GlueTableName string
	// CreateTime is when the configured table was created.
	CreateTime time.Time
	// UpdateTime is when the configured table metadata was last updated.
	UpdateTime time.Time
	// Tags carries the configured-table resource tags.
	Tags map[string]string
}

// Membership is the scanner-owned Clean Rooms membership model. It carries
// control-plane metadata only.
type Membership struct {
	// ARN is the Amazon Resource Name that uniquely identifies the membership.
	ARN string
	// ID is the Clean Rooms membership identifier.
	ID string
	// CollaborationARN is the ARN of the membership's associated collaboration.
	CollaborationARN string
	// CollaborationID is the identifier of the membership's collaboration.
	CollaborationID string
	// CollaborationName is the name of the membership's collaboration.
	CollaborationName string
	// CollaborationCreatorAccountID is the AWS account id that created the
	// collaboration.
	CollaborationCreatorAccountID string
	// MemberAbilities are the abilities granted to the collaboration member.
	MemberAbilities []string
	// Status is the membership lifecycle status (for example ACTIVE).
	Status string
	// CreateTime is when the membership was created.
	CreateTime time.Time
	// UpdateTime is when the membership metadata was last updated.
	UpdateTime time.Time
	// Tags carries the membership resource tags.
	Tags map[string]string
}
