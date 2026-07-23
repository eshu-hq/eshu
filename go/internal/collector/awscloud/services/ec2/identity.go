// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// instanceIdentityEnvelopes builds the #5448 EC2 instance identity facts for
// one instance: an aws_resource fact (resource_type=aws_ec2_instance) carrying
// the AMI (ImageId) the instance was launched from, and, when an AMI id is
// present, an aws_relationship fact recording the instance->AMI usage. Both
// facts reuse the instance record already fetched for the posture pass, so
// they add no AWS API call.
//
// This is deliberately separate from instancePostureEnvelopes
// (ec2_instance_posture) and from the CloudResource node it materializes
// (go/internal/reducer/ec2_instance_node_materialization.go): the identity
// aws_resource fact resolves to the SAME canonical cloud_resource_uid
// (identical account/region/resource_type/resource_id inputs), but the
// reducer's generic AWS resource node materialization explicitly excludes
// aws_ec2_instance from writing the shared base CloudResource properties (see
// go/internal/reducer/aws_resource_materialization.go), so the two facts never
// race to set the same property with different values. A dedicated
// EC2InstanceIdentityMaterialization domain augments the already-materialized
// node with only the disjoint ami_id property.
//
// The AMI relationship stays Postgres-only in this increment: no AMI/
// MachineImage CloudResource node class exists yet, so the generic AWS
// relationship edge projection's target join never resolves it (counted as
// unresolved, never fabricated). See
// go/internal/collector/awscloud/constants_ec2.go's ResourceTypeEC2AMI doc and
// the tracked follow-up for the AMI node class.
func instanceIdentityEnvelopes(boundary awscloud.Boundary, instance Instance) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(instanceIdentityObservation(boundary, instance))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	amiID := strings.TrimSpace(instance.ImageID)
	if amiID == "" {
		return envelopes, nil
	}
	relationship, err := awscloud.NewRelationshipEnvelope(instanceAMIRelationship(boundary, instance, amiID))
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, relationship)
	return envelopes, nil
}

// instanceIdentityObservation maps the scanner-owned instance into the
// aws_resource identity observation. It carries only the identity fields plus
// the launch AMI id in Attributes; it never reads user-data content, tag
// values, or any field the metadata-only posture contract does not already
// read from the same DescribeInstances entry.
func instanceIdentityObservation(boundary awscloud.Boundary, instance Instance) awscloud.ResourceObservation {
	instanceID := strings.TrimSpace(instance.ID)
	arn := strings.TrimSpace(instance.ARN)
	resourceID := firstNonEmpty(instanceID, arn)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeEC2Instance,
		// The identity fact carries no Name tag, mirroring the posture fact's
		// own identity derivation: the instance id is the stable name and no
		// tag value is ever read for identity.
		Name:  resourceID,
		State: strings.TrimSpace(instance.State),
		Attributes: map[string]any{
			"ami_id": strings.TrimSpace(instance.ImageID),
		},
		CorrelationAnchors: []string{resourceID, arn},
		SourceRecordID:     resourceID,
	}
}

// instanceAMIRelationship builds the instance->AMI aws_relationship
// observation. The target is identified only by the AMI id: no AMI
// aws_resource fact is ever emitted, so this relationship's graph edge
// projection resolves as unresolved (Postgres-only) until a future AMI node
// class lands.
func instanceAMIRelationship(boundary awscloud.Boundary, instance Instance, amiID string) awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(strings.TrimSpace(instance.ID), strings.TrimSpace(instance.ARN))
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipEC2InstanceUsesAMI,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(instance.ARN),
		TargetResourceID: amiID,
		TargetType:       awscloud.ResourceTypeEC2AMI,
		SourceRecordID:   sourceID + "#ami#" + amiID,
	}
}
