// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// amiResourceEnvelopes returns the #5717 AMI aws_resource identity fact for
// instance's launch AMI id, but only the first time a given AMI id is
// observed within one Scan call: seenAMIIDs is the scan-scoped dedup set
// (keyed by AMI id, which is already boundary-scoped to one account/region per
// Scan invocation) and is mutated in place. Many instances commonly launch
// from the same AMI, and emitting a duplicate aws_resource fact per instance
// would bloat fact volume for no identity benefit — the reducer's uid-keyed
// materialization (ExtractCloudResourceNodeRows) would converge duplicates to
// one node anyway, but the collector should not manufacture them in the first
// place. Returns nil when the instance carries no AMI id (mirrors
// instanceIdentityEnvelopes' relationship-emission guard) or when the AMI id
// was already emitted earlier in this scan.
func amiResourceEnvelopes(boundary awscloud.Boundary, instance Instance, seenAMIIDs map[string]struct{}) ([]facts.Envelope, error) {
	amiID := strings.TrimSpace(instance.ImageID)
	if amiID == "" {
		return nil, nil
	}
	if _, ok := seenAMIIDs[amiID]; ok {
		return nil, nil
	}
	seenAMIIDs[amiID] = struct{}{}

	resource, err := awscloud.NewResourceEnvelope(amiResourceObservation(boundary, amiID))
	if err != nil {
		return nil, err
	}
	return []facts.Envelope{resource}, nil
}

// amiResourceObservation builds the #5717 aws_resource identity observation
// for an AMI (resource_type=aws_ec2_ami), the node-class target the
// instance->AMI relationship (RelationshipEC2InstanceUsesAMI) resolves
// against. It carries ONLY the identity fields the existing DescribeInstances
// pass already reports (account, region, and the AMI id itself). The `name`
// field is set, but only to the bare AMI id — there is no human-readable name,
// state, owner, or creation-date metadata. That AMI metadata lives on the
// separate DescribeImages API, which this increment deliberately does not
// call: the scope is "make the edge resolve" (#5717), not "enrich the AMI
// node" (a distinct, separately costed follow-up). No ARN is set: AWS reports
// an AMI ARN, but DescribeInstances does not surface it, so this fact's
// identity is bare-id-only (the shape resolveTarget's byResourceID path
// expects; see go/internal/reducer/aws_relationship_join.go).
func amiResourceObservation(boundary awscloud.Boundary, amiID string) awscloud.ResourceObservation {
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ResourceID:         amiID,
		ResourceType:       awscloud.ResourceTypeEC2AMI,
		Name:               amiID,
		CorrelationAnchors: []string{amiID},
		SourceRecordID:     amiID,
	}
}
