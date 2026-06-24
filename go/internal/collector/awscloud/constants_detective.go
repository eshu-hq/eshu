// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceDetective identifies the regional Amazon Detective metadata scan
	// slice. Detective is a regional service, so one boundary covers one
	// account and region.
	ServiceDetective = "detective"
)

const (
	// ResourceTypeDetectiveGraph identifies an Amazon Detective behavior graph
	// metadata resource. Investigation, finding-group, and indicator data are
	// not part of this resource.
	ResourceTypeDetectiveGraph = "aws_detective_graph"
	// ResourceTypeDetectiveMemberAccount identifies an account enrolled in a
	// Detective behavior graph as reported by the graph administrator. It
	// carries membership status only, never the member's contact email.
	ResourceTypeDetectiveMemberAccount = "aws_detective_member_account"
)

const (
	// RelationshipDetectiveGraphHasMemberAccount records a Detective behavior
	// graph's enrolled member account. The edge targets the member account's
	// AWS Organizations account node so graph membership joins org context.
	RelationshipDetectiveGraphHasMemberAccount = "detective_graph_has_member_account"
	// RelationshipDetectiveGraphSourcesGuardDutyDetector records that a
	// Detective behavior graph ingests data from a GuardDuty detector. The edge
	// targets the GuardDuty detector node and is emitted only when a real
	// detector id is resolvable for the graph.
	RelationshipDetectiveGraphSourcesGuardDutyDetector = "detective_graph_sources_guardduty_detector"
)
