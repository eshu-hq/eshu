// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/catalog"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
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
// ec2instance.EC2InstanceIdentityNodesNotReadyFailureClass documents.

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
func perUnitReadyLookup(readyUnits map[string]bool) gpphase.ReadinessLookup {
	return func(key gpphase.PhaseKey, _ gpphase.Phase) (bool, bool) {
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

	if relationshipTypeEC2InstanceUsesAMI != catalog.RelationshipEC2InstanceUsesAMI {
		t.Fatalf(
			"reducer copy %q has drifted from catalog.RelationshipEC2InstanceUsesAMI %q; the readiness gate would stop firing silently",
			relationshipTypeEC2InstanceUsesAMI,
			catalog.RelationshipEC2InstanceUsesAMI,
		)
	}
}

// alarmObservesEC2RelationshipEnvelope builds the relationship fact the
// observability_coverage leaf drives (#6228): a CloudWatch alarm observing an
// EC2 instance by ARN dimension. The generic AWS relationship projection turns
// this into an AWS_cloudwatch_alarm_observes_metric edge whose TARGET is an
// EC2 instance node -- the same lost-edge shape batchNeedsEC2InstanceNodes
// already covers for the ec2_instance_uses_ami SOURCE, but keyed on verb
// rather than endpoint identity, so this batch slips the EC2 gate and races
// the instance-node commit (determinism N=2 and kill-vs-baseline digests
// diverge on exactly this edge while every exact-set assert stays green).
func alarmObservesEC2RelationshipEnvelope() facts.Envelope {
	return awsRelationshipEnvelope(map[string]any{
		"account_id":         "123456789012",
		"region":             "us-east-1",
		"relationship_type":  "cloudwatch_alarm_observes_metric",
		"source_resource_id": "arn:aws:cloudwatch:us-east-1:123456789012:alarm:orders-cpu-high",
		"source_arn":         "arn:aws:cloudwatch:us-east-1:123456789012:alarm:orders-cpu-high",
		"target_resource_id": "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc123def4567890",
		"target_arn":         "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc123def4567890",
		"target_type":        "unknown",
	})
}

// liveEC2InstanceResourceEnvelope is the in-scope EC2 inventory evidence the
// extended gate requires before waiting: a non-tombstoned aws_ec2_instance
// aws_resource fact. The real EC2 scanner co-emits this identity fact with
// the ec2_instance_posture fact from the same DescribeInstances pass, so its
// presence means the EC2 phase will publish; a bare EC2 ARN reference with no
// such fact (an alarm naming a terminated or never-scanned instance) means no
// EC2 work exists and waiting would stall the batch forever.
func liveEC2InstanceResourceEnvelope() facts.Envelope {
	return awsResourceEnvelope(map[string]any{
		"account_id":    "123456789012",
		"region":        "us-east-1",
		"resource_type": "aws_ec2_instance",
		"resource_id":   "i-0abc123def4567890",
		"arn":           "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc123def4567890",
	})
}

// alarmResourceEnvelope is the observability object node the alarm endpoint
// resolves against, so the convergence test below writes a real row instead
// of counting the alarm source as unresolved.
func alarmResourceEnvelope() facts.Envelope {
	return awsResourceEnvelope(map[string]any{
		"account_id":    "123456789012",
		"region":        "us-east-1",
		"resource_type": "aws_cloudwatch_alarm",
		"resource_id":   "arn:aws:cloudwatch:us-east-1:123456789012:alarm:orders-cpu-high",
		"arn":           "arn:aws:cloudwatch:us-east-1:123456789012:alarm:orders-cpu-high",
		"name":          "orders-cpu-high",
	})
}

// ghostEC2RelationshipEnvelope names an EC2 instance that was never scanned:
// the ARN matches the instance shape but no EC2 inventory fact exists in the
// scope, so no EC2 phase will ever publish. The gate must not wait for it.
func ghostEC2RelationshipEnvelope() facts.Envelope {
	return awsRelationshipEnvelope(map[string]any{
		"account_id":         "123456789012",
		"region":             "us-east-1",
		"relationship_type":  "cloudwatch_alarm_observes_metric",
		"source_resource_id": "arn:aws:cloudwatch:us-east-1:123456789012:alarm:orders-ghost-target",
		"source_arn":         "arn:aws:cloudwatch:us-east-1:123456789012:alarm:orders-ghost-target",
		"target_resource_id": "arn:aws:ec2:us-east-1:123456789012:instance/i-ghost00000000000000",
		"target_arn":         "arn:aws:ec2:us-east-1:123456789012:instance/i-ghost00000000000000",
		"target_type":        "unknown",
	})
}

// The #6228 regression: an alarm-observes-EC2 batch with live EC2 inventory
// in scope must defer while the instance-node phase is uncommitted, exactly
// like the uses_ami batch. Before the gate extension the handler succeeded
// immediately and the edge writer's target MATCH raced the EC2 node commit.
func TestAWSRelationshipDefersAlarmObservingEC2UntilInstanceNodesCommit(t *testing.T) {
	t.Parallel()

	handler := AWSRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			liveEC2InstanceResourceEnvelope(),
			alarmObservesEC2RelationshipEnvelope(),
		}},
		EdgeWriter: &recordingCloudResourceEdgeWriter{},
		ReadinessLookup: perUnitReadyLookup(map[string]bool{
			"aws_resource_materialization:scope-1": true,
			// ec2_instance_node_materialization:scope-1 deliberately absent.
		}),
	}

	_, err := handler.Handle(context.Background(), awsRelationshipIntent())
	if err == nil {
		t.Fatal("handler succeeded while the EC2 instance node phase was uncommitted; the alarm-observes-EC2 edge races the node commit")
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

// The stall guard for the extended gate: an EC2-ARN reference with no EC2
// inventory in scope must NOT wait, because no EC2 phase will ever publish.
// This batch shape (ghost target) ships in the observability_coverage leaf
// itself, so the guard is load-bearing, not hypothetical.
func TestAWSRelationshipDoesNotWaitForInstanceNodesForGhostEC2Reference(t *testing.T) {
	t.Parallel()

	handler := AWSRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			ghostEC2RelationshipEnvelope(),
		}},
		EdgeWriter: &recordingCloudResourceEdgeWriter{},
		ReadinessLookup: perUnitReadyLookup(map[string]bool{
			"aws_resource_materialization:scope-1": true,
			// No instance-node phase, as in any scope without EC2 instances.
		}),
	}

	if _, err := handler.Handle(context.Background(), awsRelationshipIntent()); err != nil {
		t.Fatalf("a ghost EC2 reference with no EC2 inventory waited on the EC2 instance node phase: %v", err)
	}
}

// Convergence: with both phases committed the alarm-observes-EC2 batch must
// proceed and materialize its edge. A gate that never opens is just a
// different way to lose the edge.
func TestAWSRelationshipWritesAlarmObservingEC2OnceBothPhasesCommit(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	handler := AWSRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			liveEC2InstanceResourceEnvelope(),
			alarmResourceEnvelope(),
			alarmObservesEC2RelationshipEnvelope(),
		}},
		EdgeWriter: writer,
		ReadinessLookup: perUnitReadyLookup(map[string]bool{
			"aws_resource_materialization:scope-1":      true,
			"ec2_instance_node_materialization:scope-1": true,
		}),
	}

	if _, err := handler.Handle(context.Background(), awsRelationshipIntent()); err != nil {
		t.Fatalf("handler deferred with both phases committed: %v", err)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1: the alarm-observes-EC2 edge never materialized", writer.writeCalls)
	}
	if len(writer.writtenRows) != 1 {
		t.Fatalf("writtenRows = %d, want 1", len(writer.writtenRows))
	}
	if writer.writtenRows[0]["relationship_type"] != "cloudwatch_alarm_observes_metric" {
		t.Errorf("relationship_type = %v, want cloudwatch_alarm_observes_metric", writer.writtenRows[0]["relationship_type"])
	}
}

// alarmObservesLambdaRelationshipEnvelope is the precision control: the same
// verb as the #6228 batch, but neither endpoint names an instance node. Live
// EC2 inventory elsewhere in the scope must not hold this batch: the gate is
// endpoint-specific, not scope-wide.
func alarmObservesLambdaRelationshipEnvelope() facts.Envelope {
	return awsRelationshipEnvelope(map[string]any{
		"account_id":         "123456789012",
		"region":             "us-east-1",
		"relationship_type":  "cloudwatch_alarm_observes_metric",
		"source_resource_id": "arn:aws:cloudwatch:us-east-1:123456789012:alarm:checkout-rollup",
		"source_arn":         "arn:aws:cloudwatch:us-east-1:123456789012:alarm:checkout-rollup",
		"target_resource_id": "arn:aws:lambda:us-east-1:123456789012:function:orders-api",
		"target_arn":         "arn:aws:lambda:us-east-1:123456789012:function:orders-api",
		"target_type":        "unknown",
	})
}

// alarmObservesEC2ByBareIDRelationshipEnvelope carries the instance identity
// as a bare id rather than an ARN, the shape real dimension values take when
// the collector reports InstanceId without the ARN.
func alarmObservesEC2ByBareIDRelationshipEnvelope() facts.Envelope {
	return awsRelationshipEnvelope(map[string]any{
		"account_id":         "123456789012",
		"region":             "us-east-1",
		"relationship_type":  "cloudwatch_alarm_observes_metric",
		"source_resource_id": "arn:aws:cloudwatch:us-east-1:123456789012:alarm:orders-cpu-high",
		"source_arn":         "arn:aws:cloudwatch:us-east-1:123456789012:alarm:orders-cpu-high",
		"target_resource_id": "i-0abc123def4567890",
		"target_type":        "unknown",
	})
}

func TestBatchNeedsEC2InstanceNodesForAlarmObservingEC2WithInventory(t *testing.T) {
	t.Parallel()
	if !batchNeedsEC2InstanceNodes(
		[]facts.Envelope{liveEC2InstanceResourceEnvelope()},
		[]facts.Envelope{alarmObservesEC2RelationshipEnvelope()},
	) {
		t.Fatal("alarm-observes-EC2 batch with live EC2 inventory does not wait on the instance-node phase; the edge races the node commit")
	}
}

func TestBatchKeepsVerbTriggerWithoutInventory(t *testing.T) {
	t.Parallel()
	if !batchNeedsEC2InstanceNodes(nil, []facts.Envelope{ec2AMIRelationshipEnvelope()}) {
		t.Fatal("ec2_instance_uses_ami batch stopped waiting on the instance-node phase")
	}
}

func TestBatchIgnoresEC2EndpointWithoutInventory(t *testing.T) {
	t.Parallel()
	if batchNeedsEC2InstanceNodes(nil, []facts.Envelope{alarmObservesEC2RelationshipEnvelope()}) {
		t.Fatal("EC2 endpoint with no EC2 inventory waits on a phase that never publishes; ghost-only scopes would deadlock")
	}
}

func TestBatchIgnoresNonEC2EndpointsDespiteInventory(t *testing.T) {
	t.Parallel()
	if batchNeedsEC2InstanceNodes(
		[]facts.Envelope{liveEC2InstanceResourceEnvelope()},
		[]facts.Envelope{alarmObservesLambdaRelationshipEnvelope()},
	) {
		t.Fatal("non-EC2 batch waits on the instance-node phase; the gate must be endpoint-specific, not scope-wide")
	}
}

func TestBatchIgnoresTombstonedEC2Inventory(t *testing.T) {
	t.Parallel()
	reaped := liveEC2InstanceResourceEnvelope()
	reaped.IsTombstone = true
	if batchNeedsEC2InstanceNodes(
		[]facts.Envelope{reaped},
		[]facts.Envelope{alarmObservesEC2RelationshipEnvelope()},
	) {
		t.Fatal("tombstoned-only EC2 inventory waits on a phase no posture fact will publish; reaped scopes would deadlock")
	}
}

func TestBatchDetectsBareInstanceIDEndpoint(t *testing.T) {
	t.Parallel()
	if !batchNeedsEC2InstanceNodes(
		[]facts.Envelope{liveEC2InstanceResourceEnvelope()},
		[]facts.Envelope{alarmObservesEC2ByBareIDRelationshipEnvelope()},
	) {
		t.Fatal("bare instance-id endpoint with live EC2 inventory does not wait on the instance-node phase; the edge races the node commit")
	}
}
