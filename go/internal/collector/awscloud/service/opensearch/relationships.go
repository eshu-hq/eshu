// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package opensearch

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/catalog"
)

func domainRelationships(boundary awscloud.Boundary, domain Domain) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(domain.ARN, domain.Name, domain.ID)
	if sourceID == "" {
		return nil
	}
	domainARN := strings.TrimSpace(domain.ARN)
	var relationships []awscloud.RelationshipObservation

	if vpcID := strings.TrimSpace(domain.VPCID); vpcID != "" {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipOpenSearchDomainInVPC,
			SourceResourceID: sourceID,
			SourceARN:        domainARN,
			TargetResourceID: vpcID,
			TargetType:       catalog.ResourceTypeEC2VPC,
			Attributes:       map[string]any{"vpc_id": vpcID},
			SourceRecordID:   relationshipRecordID(sourceID, catalog.RelationshipOpenSearchDomainInVPC, vpcID),
		})
	}
	for _, subnetID := range cloneStrings(domain.SubnetIDs) {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipOpenSearchDomainInSubnet,
			SourceResourceID: sourceID,
			SourceARN:        domainARN,
			TargetResourceID: subnetID,
			TargetType:       catalog.ResourceTypeEC2Subnet,
			Attributes:       map[string]any{"subnet_id": subnetID},
			SourceRecordID:   relationshipRecordID(sourceID, catalog.RelationshipOpenSearchDomainInSubnet, subnetID),
		})
	}
	for _, groupID := range cloneStrings(domain.SecurityGroupIDs) {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipOpenSearchDomainUsesSecurityGroup,
			SourceResourceID: sourceID,
			SourceARN:        domainARN,
			TargetResourceID: groupID,
			TargetType:       catalog.ResourceTypeEC2SecurityGroup,
			Attributes:       map[string]any{"security_group_id": groupID},
			SourceRecordID:   relationshipRecordID(sourceID, catalog.RelationshipOpenSearchDomainUsesSecurityGroup, groupID),
		})
	}
	if kmsKey := strings.TrimSpace(domain.KMSKeyID); kmsKey != "" {
		var targetARN string
		if isARN(kmsKey) {
			targetARN = kmsKey
		}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipOpenSearchDomainUsesKMSKey,
			SourceResourceID: sourceID,
			SourceARN:        domainARN,
			TargetResourceID: kmsKey,
			TargetARN:        targetARN,
			TargetType:       catalog.ResourceTypeKMSKey,
			SourceRecordID:   relationshipRecordID(sourceID, catalog.RelationshipOpenSearchDomainUsesKMSKey, kmsKey),
		})
	}
	seenRoles := make(map[string]struct{}, len(domain.MasterUserRoleARNs))
	for _, roleARN := range cloneStrings(domain.MasterUserRoleARNs) {
		if _, ok := seenRoles[roleARN]; ok {
			continue
		}
		seenRoles[roleARN] = struct{}{}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipOpenSearchDomainUsesIAMRole,
			SourceResourceID: sourceID,
			SourceARN:        domainARN,
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       catalog.ResourceTypeIAMRole,
			SourceRecordID:   relationshipRecordID(sourceID, catalog.RelationshipOpenSearchDomainUsesIAMRole, roleARN),
		})
	}
	return relationships
}

func packageDomainRelationship(
	boundary awscloud.Boundary,
	pkg Package,
	association PackageAssociation,
	domainARNs map[string]string,
) (awscloud.RelationshipObservation, bool) {
	sourceID := strings.TrimSpace(pkg.ID)
	domainName := strings.TrimSpace(association.DomainName)
	if sourceID == "" || domainName == "" {
		return awscloud.RelationshipObservation{}, false
	}
	targetID := domainName
	targetARN := ""
	if arn := strings.TrimSpace(domainARNs[domainName]); arn != "" {
		targetID = arn
		targetARN = arn
	}
	attributes := map[string]any{"domain_name": domainName}
	if status := strings.TrimSpace(association.DomainPackageStat); status != "" {
		attributes["domain_package_status"] = status
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: catalog.RelationshipOpenSearchPackageAssociatedWithDomain,
		SourceResourceID: sourceID,
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       catalog.ResourceTypeOpenSearchDomain,
		Attributes:       attributes,
		SourceRecordID:   relationshipRecordID(sourceID, catalog.RelationshipOpenSearchPackageAssociatedWithDomain, targetID),
	}, true
}

// collectionRelationships emits relationship evidence for one OpenSearch
// Serverless collection.
//
// It deliberately emits no collection-to-VPC-endpoint edge. Serverless does not
// bind a collection to a managed VPC endpoint in the collection record, and a
// managed VPC endpoint record reports no collection; the only true binding lives
// in network security policies that this scanner does not resolve. Emitting an
// edge per reported managed endpoint would fabricate an N×M cross-product that
// implies every endpoint grants access to every collection, so the edge is
// dropped until a reliable association join key (resolved policy selectors)
// exists. The managed VPC endpoint is still emitted as a standalone resource.
func collectionRelationships(
	boundary awscloud.Boundary,
	collection Collection,
) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(collection.ARN, collection.ID, collection.Name)
	if sourceID == "" {
		return nil
	}
	collectionARN := strings.TrimSpace(collection.ARN)
	var relationships []awscloud.RelationshipObservation

	if kmsKey := strings.TrimSpace(collection.KMSKeyARN); kmsKey != "" {
		var targetARN string
		if isARN(kmsKey) {
			targetARN = kmsKey
		}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipOpenSearchCollectionUsesKMSKey,
			SourceResourceID: sourceID,
			SourceARN:        collectionARN,
			TargetResourceID: kmsKey,
			TargetARN:        targetARN,
			TargetType:       catalog.ResourceTypeKMSKey,
			SourceRecordID:   relationshipRecordID(sourceID, catalog.RelationshipOpenSearchCollectionUsesKMSKey, kmsKey),
		})
	}

	return relationships
}
