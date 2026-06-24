// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package licensemanager

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// resourceTypeEC2Instance is the License-Manager-reported resource category for
// an EC2 instance association. Only this category resolves to a node Eshu keys
// today; EC2_HOST, EC2_AMI, RDS, and SYSTEMS_MANAGER_MANAGED_INSTANCE
// associations are recorded as configuration metadata but never keyed as edges,
// so no relationship dangles.
const resourceTypeEC2Instance = "EC2_INSTANCE"

// ec2InstanceTargetType is the relationship target_type for the
// configuration-applies-to-instance edge. EC2 instances are not yet emitted as
// their own CloudResource; the value is a documented forward reference in
// relguard.KnownTargetTypeAllowlist keyed by the bare instance id (i-...), the
// same convention every other scanner uses for an EC2 instance target.
const ec2InstanceTargetType = "aws_ec2_instance"

// configurationInstanceRelationship records that a license configuration is
// associated with an EC2 instance. It returns nil unless the association is an
// EC2_INSTANCE whose ResourceARN yields a bare instance id (i-...). The edge
// targets the EC2 instance by that bare id, which is how EC2 instance nodes are
// keyed (a forward reference until a dedicated EC2 instance scanner exists), so
// the edge joins the instance node instead of dangling. EC2_HOST, EC2_AMI, RDS,
// and SSM-managed-instance associations have no resolvable target node and are
// skipped here rather than keyed to a non-existent resource family.
func configurationInstanceRelationship(
	boundary awscloud.Boundary,
	configurationID string,
	configurationARN string,
	association Association,
) *awscloud.RelationshipObservation {
	if !strings.EqualFold(strings.TrimSpace(association.ResourceType), resourceTypeEC2Instance) {
		return nil
	}
	sourceID := strings.TrimSpace(configurationID)
	if sourceID == "" {
		return nil
	}
	instanceID := instanceIDFromARN(association.ResourceARN)
	if instanceID == "" {
		return nil
	}
	attributes := map[string]any{
		"resource_arn":  strings.TrimSpace(association.ResourceARN),
		"resource_type": strings.TrimSpace(association.ResourceType),
	}
	if owner := strings.TrimSpace(association.ResourceOwnerID); owner != "" {
		attributes["resource_owner_id"] = owner
	}
	sourceARN := ""
	if isARN(configurationARN) {
		sourceARN = strings.TrimSpace(configurationARN)
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipLicenseManagerConfigurationAppliesToInstance,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: instanceID,
		TargetType:       ec2InstanceTargetType,
		Attributes:       attributes,
		SourceRecordID: sourceID + "->" +
			awscloud.RelationshipLicenseManagerConfigurationAppliesToInstance + ":" + instanceID,
	}
}
