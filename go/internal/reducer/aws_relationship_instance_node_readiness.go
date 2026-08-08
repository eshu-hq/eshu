// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// relationshipTypeEC2InstanceUsesAMI is the one AWS relationship whose SOURCE
// endpoint is not an ordinary aws_resource node.
//
// Declared here rather than imported from go/internal/collector/awscloud to keep
// the reducer free of a dependency on collector internals, matching how
// readinessDeferredFailureClasses is duplicated in the golden-corpus gate. The
// value must stay equal to awscloud.RelationshipEC2InstanceUsesAMI; a drift
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
// source endpoint is an EC2 instance node.
//
// The check is per-batch on purpose. The SQL claim requirements are keyed by
// domain, and DomainAWSRelationshipMaterialization serves every AWS
// relationship type, so enrolling the domain against the instance-node phase
// would hold every AWS relationship forever in any scope that has no EC2
// instances -- the readiness gate is
// NOT EXISTS(requirement AND NOT EXISTS(phase)), and a phase that never
// publishes never satisfies it. Only the handler can see which relationship
// types are actually present.
func batchNeedsEC2InstanceNodes(relationshipEnvelopes []facts.Envelope) bool {
	for _, env := range relationshipEnvelopes {
		resource, err := decodeAWSRelationship(env)
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
	return false
}

// ec2InstanceNodesReady reports whether DomainEC2InstanceNodeMaterialization has
// committed its canonical-nodes phase for this scope and generation.
//
// It reuses the intent's scope/generation and substitutes the instance-node
// acceptance unit, because that phase is published under a different entity key
// than this domain's own. A nil lookup keeps the gate open for test wiring,
// matching canonicalNodesReady.
func (h AWSRelationshipMaterializationHandler) ec2InstanceNodesReady(intent Intent) bool {
	if h.ReadinessLookup == nil {
		return true
	}
	state, ok := graphProjectionPhaseStateForIntent(
		intent,
		GraphProjectionKeyspaceCloudResourceUID,
		GraphProjectionPhaseCanonicalNodesCommitted,
		time.Now().UTC(),
	)
	if !ok {
		return false
	}
	state.Key.AcceptanceUnitID = ec2InstanceNodeAcceptanceUnitPrefix + strings.TrimSpace(intent.ScopeID)
	ready, found := h.ReadinessLookup(state.Key, GraphProjectionPhaseCanonicalNodesCommitted)
	return found && ready
}
