// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package opensearch

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func domainRelationships(boundary aws.Boundary, domain Domain) []aws.RelationshipObservation {
	sourceID := firstNonEmpty(domain.ARN, domain.Name, domain.ID)
	if sourceID == "" {
		return nil
	}
	domainARN := strings.TrimSpace(domain.ARN)
	var relationships []aws.RelationshipObservation

	if vpcID := strings.TrimSpace(domain.VPCID); vpcID != "" {
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipOpenSearchDomainInVPC,
			SourceResourceID: sourceID,
			SourceARN:        domainARN,
			TargetResourceID: vpcID,
			TargetType:       aws.ResourceTypeEC2VPC,
			Attributes:       map[string]any{"vpc_id": vpcID},
			SourceRecordID:   relationshipRecordID(sourceID, aws.RelationshipOpenSearchDomainInVPC, vpcID),
		})
	}
	for _, subnetID := range cloneStrings(domain.SubnetIDs) {
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipOpenSearchDomainInSubnet,
			SourceResourceID: sourceID,
			SourceARN:        domainARN,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			Attributes:       map[string]any{"subnet_id": subnetID},
			SourceRecordID:   relationshipRecordID(sourceID, aws.RelationshipOpenSearchDomainInSubnet, subnetID),
		})
	}
	for _, groupID := range cloneStrings(domain.SecurityGroupIDs) {
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipOpenSearchDomainUsesSecurityGroup,
			SourceResourceID: sourceID,
			SourceARN:        domainARN,
			TargetResourceID: groupID,
			TargetType:       aws.ResourceTypeEC2SecurityGroup,
			Attributes:       map[string]any{"security_group_id": groupID},
			SourceRecordID:   relationshipRecordID(sourceID, aws.RelationshipOpenSearchDomainUsesSecurityGroup, groupID),
		})
	}
	if kmsKey := strings.TrimSpace(domain.KMSKeyID); kmsKey != "" {
		var targetARN string
		if isARN(kmsKey) {
			targetARN = kmsKey
		}
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipOpenSearchDomainUsesKMSKey,
			SourceResourceID: sourceID,
			SourceARN:        domainARN,
			TargetResourceID: kmsKey,
			TargetARN:        targetARN,
			TargetType:       aws.ResourceTypeKMSKey,
			SourceRecordID:   relationshipRecordID(sourceID, aws.RelationshipOpenSearchDomainUsesKMSKey, kmsKey),
		})
	}
	seenRoles := make(map[string]struct{}, len(domain.MasterUserRoleARNs))
	for _, roleARN := range cloneStrings(domain.MasterUserRoleARNs) {
		if _, ok := seenRoles[roleARN]; ok {
			continue
		}
		seenRoles[roleARN] = struct{}{}
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipOpenSearchDomainUsesIAMRole,
			SourceResourceID: sourceID,
			SourceARN:        domainARN,
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       aws.ResourceTypeIAMRole,
			SourceRecordID:   relationshipRecordID(sourceID, aws.RelationshipOpenSearchDomainUsesIAMRole, roleARN),
		})
	}
	return relationships
}

func packageDomainRelationship(
	boundary aws.Boundary,
	pkg Package,
	association PackageAssociation,
	domainARNs map[string]string,
) (aws.RelationshipObservation, bool) {
	sourceID := strings.TrimSpace(pkg.ID)
	domainName := strings.TrimSpace(association.DomainName)
	if sourceID == "" || domainName == "" {
		return aws.RelationshipObservation{}, false
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
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipOpenSearchPackageAssociatedWithDomain,
		SourceResourceID: sourceID,
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeOpenSearchDomain,
		Attributes:       attributes,
		SourceRecordID:   relationshipRecordID(sourceID, aws.RelationshipOpenSearchPackageAssociatedWithDomain, targetID),
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
	boundary aws.Boundary,
	collection Collection,
) []aws.RelationshipObservation {
	sourceID := firstNonEmpty(collection.ARN, collection.ID, collection.Name)
	if sourceID == "" {
		return nil
	}
	collectionARN := strings.TrimSpace(collection.ARN)
	var relationships []aws.RelationshipObservation

	if kmsKey := strings.TrimSpace(collection.KMSKeyARN); kmsKey != "" {
		var targetARN string
		if isARN(kmsKey) {
			targetARN = kmsKey
		}
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipOpenSearchCollectionUsesKMSKey,
			SourceResourceID: sourceID,
			SourceARN:        collectionARN,
			TargetResourceID: kmsKey,
			TargetARN:        targetARN,
			TargetType:       aws.ResourceTypeKMSKey,
			SourceRecordID:   relationshipRecordID(sourceID, aws.RelationshipOpenSearchCollectionUsesKMSKey, kmsKey),
		})
	}

	return relationships
}
