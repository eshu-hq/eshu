// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalogappregistry

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// applicationAttributeGroupRelationships records the attribute groups an
// AppRegistry application is associated with. Each edge's source id is
// applicationResourceID (the value the application node publishes) and the
// target id is the attribute-group ARN, matching how the attribute-group node
// publishes its resource_id, so the edge joins the group node by ARN equality.
// AWS reports an attribute-group ARN per association; non-ARN identifiers are
// skipped so the edge never dangles. Duplicate group ARNs are de-duplicated.
func applicationAttributeGroupRelationships(
	boundary awscloud.Boundary,
	application Application,
) []awscloud.RelationshipObservation {
	sourceID := applicationResourceID(application)
	if sourceID == "" || len(application.AttributeGroupARNs) == 0 {
		return nil
	}
	observations := make([]awscloud.RelationshipObservation, 0, len(application.AttributeGroupARNs))
	seen := make(map[string]struct{}, len(application.AttributeGroupARNs))
	for _, raw := range application.AttributeGroupARNs {
		targetID := strings.TrimSpace(raw)
		if targetID == "" {
			continue
		}
		if _, exists := seen[targetID]; exists {
			continue
		}
		seen[targetID] = struct{}{}
		targetARN := ""
		if isARN(targetID) {
			targetARN = targetID
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipServiceCatalogAppRegistryApplicationHasAttributeGroup,
			SourceResourceID: sourceID,
			SourceARN:        strings.TrimSpace(application.ARN),
			TargetResourceID: targetID,
			TargetARN:        targetARN,
			TargetType:       awscloud.ResourceTypeServiceCatalogAppRegistryAttributeGroup,
			SourceRecordID: sourceID + "->" +
				awscloud.RelationshipServiceCatalogAppRegistryApplicationHasAttributeGroup +
				":" + targetID,
		})
	}
	if len(observations) == 0 {
		return nil
	}
	return observations
}

// applicationStackRelationships records the CloudFormation stacks an
// AppRegistry application is associated with. An edge is emitted only for
// CFN_STACK associated resources whose reported ARN is a CloudFormation stack
// ARN. The edge is ARN-keyed: the cloudformation scanner publishes a stack
// node's resource_id as the stack ARN, so the target id and target ARN are both
// the stack ARN. The source id is applicationResourceID, matching the
// application node's own resource_id, so the outgoing edge resolves to its
// source node. Non-CFN associations (for example RESOURCE_TAG_VALUE) have no
// scanned target node and are skipped rather than dangled. Duplicate stack ARNs
// are de-duplicated.
func applicationStackRelationships(
	boundary awscloud.Boundary,
	application Application,
) []awscloud.RelationshipObservation {
	sourceID := applicationResourceID(application)
	if sourceID == "" || len(application.AssociatedResources) == 0 {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	seen := make(map[string]struct{}, len(application.AssociatedResources))
	for _, resource := range application.AssociatedResources {
		if !strings.EqualFold(strings.TrimSpace(resource.ResourceType), cfnStackAssociationType) {
			continue
		}
		stackARN := strings.TrimSpace(resource.ARN)
		if !isCloudFormationStackARN(stackARN) {
			continue
		}
		if _, exists := seen[stackARN]; exists {
			continue
		}
		seen[stackARN] = struct{}{}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipServiceCatalogAppRegistryApplicationAssociatesCloudFormationStack,
			SourceResourceID: sourceID,
			SourceARN:        strings.TrimSpace(application.ARN),
			TargetResourceID: stackARN,
			TargetARN:        stackARN,
			TargetType:       awscloud.ResourceTypeCloudFormationStack,
			SourceRecordID: sourceID + "->" +
				awscloud.RelationshipServiceCatalogAppRegistryApplicationAssociatesCloudFormationStack +
				":" + stackARN,
		})
	}
	return observations
}

// isARN reports whether value carries the canonical AWS ARN prefix.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}
