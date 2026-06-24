// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package datazone

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// domainKMSRelationship records a DataZone domain's reported KMS encryption key
// dependency. DataZone reports a key id, key ARN, or alias; the edge is keyed by
// that identifier as the KMS scanner publishes its key resource_id, with the ARN
// set only when the identifier is an ARN. It returns nil when no key is
// reported.
func domainKMSRelationship(boundary awscloud.Boundary, domain Domain) *awscloud.RelationshipObservation {
	targetID := strings.TrimSpace(domain.KMSKeyIdentifier)
	if targetID == "" {
		return nil
	}
	sourceID := domainResourceID(domain)
	if sourceID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipDatazoneDomainUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(domain.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipDatazoneDomainUsesKMSKey + ":" + targetID,
	}
}

// domainIAMRoleRelationship records a DataZone domain's reported IAM role
// dependency for one role ARN. DataZone reports the domain execution role and
// the service role as IAM role ARNs, which match how the IAM scanner publishes
// its role resource_id. roleKind labels which role the edge records. It returns
// nil when the role ARN is missing or is not an IAM role ARN.
func domainIAMRoleRelationship(
	boundary awscloud.Boundary,
	domain Domain,
	roleARN string,
	roleKind string,
) *awscloud.RelationshipObservation {
	roleARN = strings.TrimSpace(roleARN)
	if !isIAMRoleARN(roleARN) {
		return nil
	}
	sourceID := domainResourceID(domain)
	if sourceID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipDatazoneDomainUsesIAMRole,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(domain.ARN),
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       awscloud.ResourceTypeIAMRole,
		Attributes: map[string]any{
			"role_kind": roleKind,
		},
		SourceRecordID: sourceID + "->" + awscloud.RelationshipDatazoneDomainUsesIAMRole + ":" + roleARN,
	}
}

// childInDomainRelationship records a DataZone child resource's membership in
// its parent domain. childID is the resource_id the child node publishes,
// domainID is the resource_id the domain node publishes (its domain id), and
// relationshipType / childType describe the specific child. It returns nil when
// either endpoint identity is missing.
func childInDomainRelationship(
	boundary awscloud.Boundary,
	relationshipType string,
	childID string,
	childARN string,
	domainID string,
) *awscloud.RelationshipObservation {
	childID = strings.TrimSpace(childID)
	domainID = strings.TrimSpace(domainID)
	if childID == "" || domainID == "" {
		return nil
	}
	targetARN := ""
	if isARN(domainID) {
		targetARN = domainID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: childID,
		SourceARN:        strings.TrimSpace(childARN),
		TargetResourceID: domainID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeDatazoneDomain,
		SourceRecordID:   childID + "->" + relationshipType + ":" + domainID,
	}
}

// dataSourceGlueRelationships records a DataZone Glue data source's backing AWS
// Glue Data Catalog databases. The edges are keyed by the Glue database name,
// matching how the Glue scanner publishes its database resource_id. It returns
// nil when the data source reports no resolvable Glue database.
func dataSourceGlueRelationships(
	boundary awscloud.Boundary,
	dataSource DataSource,
) []awscloud.RelationshipObservation {
	sourceID := strings.TrimSpace(dataSource.ID)
	if sourceID == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	for _, database := range cloneStrings(dataSource.GlueDatabaseNames) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipDatazoneDataSourceBacksGlueDatabase,
			SourceResourceID: sourceID,
			TargetResourceID: database,
			TargetType:       awscloud.ResourceTypeGlueDatabase,
			SourceRecordID:   sourceID + "->" + awscloud.RelationshipDatazoneDataSourceBacksGlueDatabase + ":" + database,
		})
	}
	return observations
}

// dataSourceRedshiftRelationship records a DataZone Redshift data source's
// backing provisioned cluster. The edge is keyed by the partition-aware cluster
// ARN the Redshift scanner synthesizes and publishes for a cluster node. It
// returns nil when the data source reports no provisioned cluster (serverless
// workgroups are not resolvable to a published node id and are omitted).
func dataSourceRedshiftRelationship(
	boundary awscloud.Boundary,
	dataSource DataSource,
) *awscloud.RelationshipObservation {
	sourceID := strings.TrimSpace(dataSource.ID)
	if sourceID == "" {
		return nil
	}
	clusterARN := redshiftClusterARN(
		boundary,
		dataSource.RedshiftClusterName,
		dataSource.BackingAccountID,
		dataSource.BackingRegion,
	)
	if clusterARN == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipDatazoneDataSourceBacksRedshiftCluster,
		SourceResourceID: sourceID,
		TargetResourceID: clusterARN,
		TargetARN:        clusterARN,
		TargetType:       awscloud.ResourceTypeRedshiftCluster,
		Attributes: map[string]any{
			"cluster_name": strings.TrimSpace(dataSource.RedshiftClusterName),
		},
		SourceRecordID: sourceID + "->" + awscloud.RelationshipDatazoneDataSourceBacksRedshiftCluster + ":" + clusterARN,
	}
}

// isIAMRoleARN reports whether value is an IAM role ARN
// (arn:<partition>:iam::<account>:role/...). DataZone may report a non-role
// principal in unusual configurations; the edge is only keyed when the value is
// a genuine IAM role ARN so it joins the IAM scanner's role node exactly.
func isIAMRoleARN(value string) bool {
	value = strings.TrimSpace(value)
	if !isARN(value) {
		return false
	}
	parts := strings.SplitN(value, ":", 6)
	if len(parts) < 6 || parts[2] != "iam" {
		return false
	}
	return strings.HasPrefix(parts[5], "role/")
}
