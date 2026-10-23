// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// relationshipTypeEC2InstanceUsesAMI is the one AWS relationship whose SOURCE
// endpoint is not an ordinary aws_resource node.
//
// Declared here rather than imported from go/internal/collector/awscloud to keep
// the reducer free of a dependency on collector internals. The value must stay
// equal to catalog.RelationshipEC2InstanceUsesAMI; a drift
// makes this gate silently stop firing, which is why
// TestAWSRelationshipInstanceNodeGateMatchesTheCollectorConstant pins them
// together.
const relationshipTypeEC2InstanceUsesAMI = "ec2_instance_uses_ami"

// ec2InstanceNodeAcceptanceUnitPrefix is the acceptance unit
// DomainEC2InstanceNodeMaterialization publishes its canonical-nodes phase
// under. It is NOT this domain's own entity key, which is why the ordinary
// readiness gate misses it.
const ec2InstanceNodeAcceptanceUnitPrefix = "ec2_instance_node_materialization:"

// AWSRelationshipEC2InstanceNodesNotReadyFailureClass classifies a deferral of
// an ec2_instance_uses_ami edge whose SOURCE instance node has not committed.
//
// Registered as a non-counting reducer retry class
// (nonCountingReducerRetryFailureClasses in
// go/internal/storage/postgres/reducer_queue_readiness_sql.go) for the same
// reason as its siblings: the work is waiting on an upstream phase rather than
// failing on its own merits, so charging it against the retry budget would
// dead-letter a still-pending edge that the succeeded-only reopen path would
// never reopen.
const AWSRelationshipEC2InstanceNodesNotReadyFailureClass = "aws_relationship_ec2_instance_nodes_not_ready"

// awsRelationshipEC2InstanceNodesNotReadyError defers the batch until the EC2
// instance node phase commits.
type awsRelationshipEC2InstanceNodesNotReadyError struct {
	scopeID      string
	generationID string
}

func (e awsRelationshipEC2InstanceNodesNotReadyError) Error() string {
	return fmt.Sprintf(
		"ec2 instance nodes not committed for scope %s generation %s; deferring ec2_instance_uses_ami edges rather than writing them against an absent source",
		e.scopeID,
		e.generationID,
	)
}

func (awsRelationshipEC2InstanceNodesNotReadyError) Retryable() bool { return true }

func (awsRelationshipEC2InstanceNodesNotReadyError) FailureClass() string {
	return AWSRelationshipEC2InstanceNodesNotReadyFailureClass
}

// batchNeedsEC2InstanceNodes reports whether this batch carries an edge whose
// endpoint is an EC2 instance node.
//
// The check is per-batch on purpose. The SQL claim requirements are keyed by
// domain, and DomainAWSRelationshipMaterialization serves every AWS
// relationship type, so enrolling the domain against the instance-node phase
// would hold every AWS relationship forever in any scope that has no EC2
// instances -- the readiness gate is
// NOT EXISTS(requirement AND NOT EXISTS(phase)), and a phase that never
// publishes never satisfies it. Only the handler can see which relationship
// endpoints are actually present.
//
// Two shapes qualify. The ec2_instance_uses_ami verb names an edge whose
// SOURCE is always an instance node, so the verb alone implies EC2 work. Any
// other verb can still resolve against an instance node as source or target
// (a CloudWatch alarm observing an EC2 metric is the #6228 case: the generic
// AWS_cloudwatch_alarm_observes_metric edge raced the instance-node commit
// while the family's own AWS_COVERS_alarm edge waited on both phases), so an
// EC2 instance endpoint identity is the second trigger. That trigger
// additionally requires live EC2 inventory in the same scope generation (a
// non-tombstoned aws_ec2_instance aws_resource fact): the real EC2 scanner
// co-emits that identity fact with the ec2_instance_posture fact from the
// same DescribeInstances pass, so its presence means the instance-node phase
// will publish, while a bare EC2 ARN with no such fact (an alarm naming a
// terminated or never-scanned instance) means no EC2 work exists and waiting
// would stall the batch forever.
func batchNeedsEC2InstanceNodes(resourceEnvelopes, relationshipEnvelopes []facts.Envelope) bool {
	for _, env := range relationshipEnvelopes {
		resource, err := schemadecode.DecodeAWSRelationship(env)
		if err != nil {
			// A payload this handler cannot decode is dead-lettered downstream
			// by the ordinary extract path. Do not gate on it here: refusing to
			// run because of an undecodable neighbour would convert one bad
			// fact into a stalled scope.
			continue
		}
		if strings.EqualFold(strings.TrimSpace(resource.RelationshipType), relationshipTypeEC2InstanceUsesAMI) {
			return true
		}
	}
	if !scopeHasLiveEC2InstanceResource(resourceEnvelopes) {
		return false
	}
	for _, env := range relationshipEnvelopes {
		relationship, err := schemadecode.DecodeAWSRelationship(env)
		if err != nil {
			continue
		}
		if relationshipReferencesEC2Instance(relationship) {
			return true
		}
	}
	return false
}

// scopeHasLiveEC2InstanceResource reports whether the scope generation
// carries a non-tombstoned aws_ec2_instance inventory fact. A tombstoned-only
// EC2 fact (a reaped instance) is not evidence of EC2 work: no posture fact
// accompanies it, so no instance-node phase will publish for it.
func scopeHasLiveEC2InstanceResource(resourceEnvelopes []facts.Envelope) bool {
	for _, env := range resourceEnvelopes {
		if env.FactKind != cloud.AWSResourceFactKind || env.IsTombstone {
			continue
		}
		resource, err := schemadecode.DecodeAWSResource(env)
		if err != nil {
			// Undecodable here is dead-lettered downstream by the ordinary
			// extract path; it is never evidence of live EC2 work.
			continue
		}
		if resource.ResourceType == awsv1.ResourceTypeEC2Instance {
			return true
		}
	}
	return false
}

// relationshipReferencesEC2Instance reports whether either endpoint of the
// relationship names an EC2 instance node: an EC2 instance ARN or a bare
// instance id, carried on any of the four endpoint identity fields.
func relationshipReferencesEC2Instance(relationship awsv1.Relationship) bool {
	return isEC2InstanceIdentity(relationship.SourceResourceID) ||
		isEC2InstanceIdentity(relationship.TargetResourceID) ||
		isEC2InstanceIdentity(payloadcore.DerefString(relationship.SourceARN)) ||
		isEC2InstanceIdentity(payloadcore.DerefString(relationship.TargetARN))
}

// isEC2InstanceIdentity reports whether one endpoint identity names an EC2
// instance node: an EC2 instance ARN (arn:...:instance/i-...) or a bare
// instance id (i-...). The ARN check keys on the instance resource segment
// rather than a service prefix so other partitions (aws-cn, aws-us-gov) and
// regions match the same shape.
func isEC2InstanceIdentity(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, ":instance/i-") {
		return true
	}
	return isEC2InstanceBareID(trimmed)
}

// isEC2InstanceBareID reports whether value looks like a bare EC2 instance
// id: the i- prefix plus hex. The i- prefix is EC2-instance-exclusive in
// AWS, so no service allowlist is needed to keep this precise.
func isEC2InstanceBareID(value string) bool {
	rest, ok := strings.CutPrefix(value, "i-")
	if !ok || rest == "" {
		return false
	}
	for _, r := range rest {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

// ec2InstanceNodesReady reports whether DomainEC2InstanceNodeMaterialization has
// committed its canonical-nodes phase for this scope and generation.
//
// It reuses the intent's scope/generation and substitutes the instance-node
// acceptance unit, because that phase is published under a different entity key
// than this domain's own. A nil lookup keeps the gate open for test wiring,
// matching canonicalNodesReady.
func (h AWSRelationshipMaterializationHandler) ec2InstanceNodesReady(intent reducercontract.Intent) bool {
	if h.ReadinessLookup == nil {
		return true
	}
	state, ok := graphProjectionPhaseStateForIntent(
		intent,
		gpphase.KeyspaceCloudResourceUID,
		gpphase.PhaseCanonicalNodesCommitted,
		time.Now().UTC(),
	)
	if !ok {
		return false
	}
	state.Key.AcceptanceUnitID = ec2InstanceNodeAcceptanceUnitPrefix + strings.TrimSpace(intent.ScopeID)
	ready, found := h.ReadinessLookup(state.Key, gpphase.PhaseCanonicalNodesCommitted)
	return found && ready
}
