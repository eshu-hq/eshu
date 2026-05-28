package opensearch

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
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
			RelationshipType: awscloud.RelationshipOpenSearchDomainInVPC,
			SourceResourceID: sourceID,
			SourceARN:        domainARN,
			TargetResourceID: vpcID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			Attributes:       map[string]any{"vpc_id": vpcID},
			SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipOpenSearchDomainInVPC, vpcID),
		})
	}
	for _, subnetID := range cloneStrings(domain.SubnetIDs) {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipOpenSearchDomainInSubnet,
			SourceResourceID: sourceID,
			SourceARN:        domainARN,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			Attributes:       map[string]any{"subnet_id": subnetID},
			SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipOpenSearchDomainInSubnet, subnetID),
		})
	}
	for _, groupID := range cloneStrings(domain.SecurityGroupIDs) {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipOpenSearchDomainUsesSecurityGroup,
			SourceResourceID: sourceID,
			SourceARN:        domainARN,
			TargetResourceID: groupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			Attributes:       map[string]any{"security_group_id": groupID},
			SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipOpenSearchDomainUsesSecurityGroup, groupID),
		})
	}
	if kmsKey := strings.TrimSpace(domain.KMSKeyID); kmsKey != "" {
		var targetARN string
		if isARN(kmsKey) {
			targetARN = kmsKey
		}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipOpenSearchDomainUsesKMSKey,
			SourceResourceID: sourceID,
			SourceARN:        domainARN,
			TargetResourceID: kmsKey,
			TargetARN:        targetARN,
			TargetType:       awscloud.ResourceTypeKMSKey,
			SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipOpenSearchDomainUsesKMSKey, kmsKey),
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
			RelationshipType: awscloud.RelationshipOpenSearchDomainUsesIAMRole,
			SourceResourceID: sourceID,
			SourceARN:        domainARN,
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       awscloud.ResourceTypeIAMRole,
			SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipOpenSearchDomainUsesIAMRole, roleARN),
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
		RelationshipType: awscloud.RelationshipOpenSearchPackageAssociatedWithDomain,
		SourceResourceID: sourceID,
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeOpenSearchDomain,
		Attributes:       attributes,
		SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipOpenSearchPackageAssociatedWithDomain, targetID),
	}, true
}

func collectionRelationships(
	boundary awscloud.Boundary,
	collection Collection,
	vpcEndpoints []VPCEndpoint,
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
			RelationshipType: awscloud.RelationshipOpenSearchCollectionUsesKMSKey,
			SourceResourceID: sourceID,
			SourceARN:        collectionARN,
			TargetResourceID: kmsKey,
			TargetARN:        targetARN,
			TargetType:       awscloud.ResourceTypeKMSKey,
			SourceRecordID:   relationshipRecordID(sourceID, awscloud.RelationshipOpenSearchCollectionUsesKMSKey, kmsKey),
		})
	}

	// OpenSearch Serverless does not bind a collection to a single VPC endpoint
	// in the collection record; network access is governed by network security
	// policies that select collections and grant managed VPC endpoints. Emitting
	// a collection-to-endpoint edge per reported managed endpoint records the
	// observed network-access surface without inventing policy-resolution truth.
	for _, endpoint := range vpcEndpoints {
		endpointID := strings.TrimSpace(endpoint.ID)
		if endpointID == "" {
			continue
		}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipOpenSearchCollectionUsesVPCEndpoint,
			SourceResourceID: sourceID,
			SourceARN:        collectionARN,
			TargetResourceID: endpointID,
			TargetType:       awscloud.ResourceTypeOpenSearchServerlessVPCEndpoint,
			Attributes: map[string]any{
				"vpc_endpoint_name": strings.TrimSpace(endpoint.Name),
				"vpc_id":            strings.TrimSpace(endpoint.VPCID),
			},
			SourceRecordID: relationshipRecordID(sourceID, awscloud.RelationshipOpenSearchCollectionUsesVPCEndpoint, endpointID),
		})
	}
	return relationships
}
