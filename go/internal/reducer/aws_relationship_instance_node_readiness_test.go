// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// DomainAWSRelationshipMaterialization gates on the canonical-nodes phase
// published under its own entity key, aws_resource_materialization:<scope>.
// That covers the TARGET of ec2_instance_uses_ami (the AMI, an ordinary
// aws_resource) and misses the SOURCE.
//
// The EC2 instance is deliberately excluded from generic resource
// materialization (aws_resource_materialization.go:272, #5448) and is created by
// the independent DomainEC2InstanceNodeMaterialization under
// ec2_instance_node_materialization:<scope>. Nothing makes the relationship wait
// for it. When relationship work runs first the edge writer's source MATCH finds
// nothing, writes nothing, returns no error, and the work item SUCCEEDS. The
// later instance-node commit never reopens it, so the edge is lost for the
// generation.
//
// That is #5717's CI failure exactly: both endpoint nodes present at assert
// time, no edge of that type anywhere, stable on retry, every work item
// succeeded, and the outcome flipping with reducer scheduling between CI and a
// developer machine.
//
// The gate cannot live in the SQL claim requirements. Those are keyed by domain,
// and this domain serves every AWS relationship type; a blanket requirement on
// ec2_instance_node_materialization: would make the readiness gate
// (NOT EXISTS(requirement AND NOT EXISTS(phase))) hold every AWS relationship
// forever in any scope with no EC2 instances, because that phase never
// publishes there. The check therefore has to be conditional on the batch, which
// only the Go handler can see -- the same defense-in-depth position
// EC2InstanceIdentityNodesNotReadyFailureClass documents.

// ec2AMIRelationshipEnvelope builds the relationship fact whose source endpoint
// is an EC2 instance node.
func ec2AMIRelationshipEnvelope() facts.Envelope {
	return awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "ec2_instance_uses_ami",
		"source_resource_id": "i-0000000000000001",
		"target_resource_id": "ami-000000000000000a",
		"target_type":        "aws_ec2_ami",
	})
}

// perUnitReadyLookup answers readiness per acceptance unit, so a test can commit
// one node phase and withhold the other. readyLookup cannot express that: it
// returns the same answer for every key.
func perUnitReadyLookup(readyUnits map[string]bool) GraphProjectionReadinessLookup {
	return func(key GraphProjectionPhaseKey, _ GraphProjectionPhase) (bool, bool) {
		ready, found := readyUnits[key.AcceptanceUnitID]
		return ready, found
	}
}

// The gate itself. With an ec2_instance_uses_ami fact in the batch and the
// instance-node phase uncommitted, the handler must defer instead of writing a
// sourceless edge and reporting success.
func TestAWSRelationshipDefersWhenTheEC2InstanceNodePhaseHasNotCommitted(t *testing.T) {
	t.Parallel()

	handler := AWSRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{ec2AMIRelationshipEnvelope()}},
		EdgeWriter: &recordingCloudResourceEdgeWriter{},
		ReadinessLookup: perUnitReadyLookup(map[string]bool{
			"aws_resource_materialization:scope-1": true,
			// ec2_instance_node_materialization:scope-1 deliberately absent.
		}),
	}

	_, err := handler.Handle(context.Background(), awsRelationshipIntent())
	if err == nil {
		t.Fatal("handler succeeded while the EC2 instance node phase was uncommitted; the edge would be silently lost")
	}

	var notReady interface{ Retryable() bool }
	if !errors.As(err, &notReady) || !notReady.Retryable() {
		t.Fatalf("error is not retryable, so the queue will not re-run it: %v", err)
	}

	var classed interface{ FailureClass() string }
	if !errors.As(err, &classed) {
		t.Fatalf("error carries no failure class, so the queue cannot exempt it from the retry budget: %v", err)
	}
	if classed.FailureClass() != AWSRelationshipEC2InstanceNodesNotReadyFailureClass {
		t.Errorf("FailureClass() = %q, want %q", classed.FailureClass(), AWSRelationshipEC2InstanceNodesNotReadyFailureClass)
	}
}

// Once the instance-node phase commits, the same batch must proceed. A gate that
// never opens is just a different way to lose the edge.
func TestAWSRelationshipProceedsOnceTheEC2InstanceNodePhaseCommits(t *testing.T) {
	t.Parallel()

	handler := AWSRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{ec2AMIRelationshipEnvelope()}},
		EdgeWriter: &recordingCloudResourceEdgeWriter{},
		ReadinessLookup: perUnitReadyLookup(map[string]bool{
			"aws_resource_materialization:scope-1":      true,
			"ec2_instance_node_materialization:scope-1": true,
		}),
	}

	if _, err := handler.Handle(context.Background(), awsRelationshipIntent()); err != nil {
		t.Fatalf("handler deferred with both phases committed: %v", err)
	}
}

// The stall guard, and the reason this check is in Go rather than in the SQL
// claim requirements. A scope with no EC2 instances never publishes
// ec2_instance_node_materialization:<scope>. A batch carrying no
// ec2_instance_uses_ami fact must not wait for it, or every AWS relationship in
// every non-EC2 scope deadlocks.
func TestAWSRelationshipDoesNotWaitForInstanceNodesWhenNoAMIEdgeIsPresent(t *testing.T) {
	t.Parallel()

	unrelated := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_KMS_KEY",
		"source_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"source_arn":         "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"target_resource_id": "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_arn":         "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_type":        "aws_kms_key",
	})

	handler := AWSRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{unrelated}},
		EdgeWriter: &recordingCloudResourceEdgeWriter{},
		ReadinessLookup: perUnitReadyLookup(map[string]bool{
			"aws_resource_materialization:scope-1": true,
			// No instance-node phase, as in any scope without EC2 instances.
		}),
	}

	if _, err := handler.Handle(context.Background(), awsRelationshipIntent()); err != nil {
		t.Fatalf("a non-EC2 relationship batch waited on the EC2 instance node phase: %v", err)
	}
}

// The doc comment on relationshipTypeEC2InstanceUsesAMI cites this test by name
// as the thing keeping the duplicated constant honest, so it has to exist and it
// has to actually compare the two values. A drift here does not fail loudly: the
// gate simply stops recognising the AMI edge and #5717's silent edge loss
// returns with every other test still green.
func TestAWSRelationshipInstanceNodeGateMatchesTheCollectorConstant(t *testing.T) {
	t.Parallel()

	if relationshipTypeEC2InstanceUsesAMI != awscloud.RelationshipEC2InstanceUsesAMI {
		t.Fatalf(
			"reducer copy %q has drifted from awscloud.RelationshipEC2InstanceUsesAMI %q; the readiness gate would stop firing silently",
			relationshipTypeEC2InstanceUsesAMI,
			awscloud.RelationshipEC2InstanceUsesAMI,
		)
	}
}
