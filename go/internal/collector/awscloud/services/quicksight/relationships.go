// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package quicksight

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// dataSourceBackingRelationship records a QuickSight data source's resolvable
// backing-store dependency by connector type. It returns nil when the connector
// has no repo-resolvable backing reference (an unscanned connector type, or a
// connection identified only by host/port with no cluster/instance id). The
// target id matches how each backing scanner publishes its resource_id: bare
// cluster id (Redshift), bare DB instance id (RDS), bare workgroup name
// (Athena), and the partition-aware synthesized bucket ARN (S3).
func dataSourceBackingRelationship(
	boundary awscloud.Boundary,
	dataSource DataSource,
) *awscloud.RelationshipObservation {
	sourceID := dataSourceResourceID(dataSource)
	identifier := strings.TrimSpace(dataSource.Backing.Identifier)
	if sourceID == "" || identifier == "" {
		return nil
	}
	base := awscloud.RelationshipObservation{
		Boundary:         boundary,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(dataSource.ARN),
	}
	switch dataSource.Backing.Kind {
	case BackingStoreRedshiftCluster:
		base.RelationshipType = awscloud.RelationshipQuickSightDataSourceUsesRedshiftCluster
		base.TargetResourceID = identifier
		base.TargetType = awscloud.ResourceTypeRedshiftCluster
	case BackingStoreRDSInstance:
		base.RelationshipType = awscloud.RelationshipQuickSightDataSourceUsesRDSInstance
		base.TargetResourceID = identifier
		base.TargetType = awscloud.ResourceTypeRDSDBInstance
	case BackingStoreAthenaWorkGroup:
		base.RelationshipType = awscloud.RelationshipQuickSightDataSourceUsesAthenaWorkGroup
		base.TargetResourceID = identifier
		base.TargetType = awscloud.ResourceTypeAthenaWorkGroup
	case BackingStoreS3Bucket:
		bucketARN := arnForBucket(awscloud.PartitionForBoundary(boundary), identifier)
		if bucketARN == "" {
			return nil
		}
		base.RelationshipType = awscloud.RelationshipQuickSightDataSourceUsesS3Bucket
		base.TargetResourceID = bucketARN
		base.TargetARN = bucketARN
		base.TargetType = awscloud.ResourceTypeS3Bucket
	default:
		return nil
	}
	base.SourceRecordID = sourceID + "->" + base.RelationshipType + ":" + base.TargetResourceID
	return &base
}

// dataSourceVPCRelationships records the security-group and subnet edges for the
// VPC connection a QuickSight data source uses. It returns nil when the data
// source has no VPC connection or the connection did not resolve to a known
// summary (for example because the account hid it). Security groups and subnets
// are keyed by bare id, matching the EC2 scanner's published resource_id.
func dataSourceVPCRelationships(
	boundary awscloud.Boundary,
	dataSource DataSource,
	connections map[string]VPCConnection,
) []awscloud.RelationshipObservation {
	connARN := strings.TrimSpace(dataSource.VPCConnectionARN)
	if connARN == "" {
		return nil
	}
	connID := vpcConnectionIDFromARN(connARN)
	if connID == "" {
		return nil
	}
	resolved, ok := connections[connID]
	if !ok {
		return nil
	}
	sourceID := dataSourceResourceID(dataSource)
	if sourceID == "" {
		return nil
	}
	sourceARN := strings.TrimSpace(dataSource.ARN)
	var edges []awscloud.RelationshipObservation
	for _, groupID := range dedupeStrings(resolved.SecurityGroupIDs) {
		edges = append(edges, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipQuickSightDataSourceUsesSecurityGroup,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: groupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			Attributes:       map[string]any{"vpc_connection_id": connID},
			SourceRecordID:   sourceID + "->" + awscloud.RelationshipQuickSightDataSourceUsesSecurityGroup + ":" + groupID,
		})
	}
	for _, subnetID := range dedupeStrings(resolved.SubnetIDs) {
		edges = append(edges, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipQuickSightDataSourceUsesSubnet,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			Attributes:       map[string]any{"vpc_connection_id": connID},
			SourceRecordID:   sourceID + "->" + awscloud.RelationshipQuickSightDataSourceUsesSubnet + ":" + subnetID,
		})
	}
	return edges
}

// dataSetDataSourceRelationships records a QuickSight dataset's physical-read
// dependency on the QuickSight data sources it reads. The target is keyed by the
// data-source ARN this scanner publishes, so the edge joins the data-source node
// emitted in the same scan. It returns nil when the dataset reads no resolvable
// data source.
func dataSetDataSourceRelationships(
	boundary awscloud.Boundary,
	dataSet DataSet,
) []awscloud.RelationshipObservation {
	sourceID := dataSetResourceID(dataSet)
	if sourceID == "" {
		return nil
	}
	sourceARN := strings.TrimSpace(dataSet.ARN)
	var edges []awscloud.RelationshipObservation
	for _, targetARN := range dedupeStrings(dataSet.DataSourceARNs) {
		edges = append(edges, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipQuickSightDataSetReadsDataSource,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: targetARN,
			TargetARN:        targetARN,
			TargetType:       awscloud.ResourceTypeQuickSightDataSource,
			SourceRecordID:   sourceID + "->" + awscloud.RelationshipQuickSightDataSetReadsDataSource + ":" + targetARN,
		})
	}
	return edges
}

// dashboardDataSetRelationships records the datasets a QuickSight dashboard's
// published version reads. The target is keyed by the dataset ARN this scanner
// publishes. It returns nil when the dashboard reads no dataset.
func dashboardDataSetRelationships(
	boundary awscloud.Boundary,
	dashboard Dashboard,
) []awscloud.RelationshipObservation {
	sourceID := dashboardResourceID(dashboard)
	if sourceID == "" {
		return nil
	}
	sourceARN := strings.TrimSpace(dashboard.ARN)
	var edges []awscloud.RelationshipObservation
	for _, targetARN := range dedupeStrings(dashboard.DataSetARNs) {
		edges = append(edges, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipQuickSightDashboardReadsDataSet,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: targetARN,
			TargetARN:        targetARN,
			TargetType:       awscloud.ResourceTypeQuickSightDataSet,
			SourceRecordID:   sourceID + "->" + awscloud.RelationshipQuickSightDashboardReadsDataSet + ":" + targetARN,
		})
	}
	return edges
}

// analysisDataSetRelationships records the datasets a QuickSight analysis reads.
// The target is keyed by the dataset ARN this scanner publishes. It returns nil
// when the analysis reads no dataset.
func analysisDataSetRelationships(
	boundary awscloud.Boundary,
	analysis Analysis,
) []awscloud.RelationshipObservation {
	sourceID := analysisResourceID(analysis)
	if sourceID == "" {
		return nil
	}
	sourceARN := strings.TrimSpace(analysis.ARN)
	var edges []awscloud.RelationshipObservation
	for _, targetARN := range dedupeStrings(analysis.DataSetARNs) {
		edges = append(edges, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipQuickSightAnalysisReadsDataSet,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: targetARN,
			TargetARN:        targetARN,
			TargetType:       awscloud.ResourceTypeQuickSightDataSet,
			SourceRecordID:   sourceID + "->" + awscloud.RelationshipQuickSightAnalysisReadsDataSet + ":" + targetARN,
		})
	}
	return edges
}
