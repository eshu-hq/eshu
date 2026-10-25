// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package obscoverage

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// EC2 instance-node hold outcomes, logged as ec2_node_hold on completion so an
// operator can tell a satisfied hold from one the scope never needed (#9055).
const (
	ec2NodeHoldSatisfied   = "satisfied"
	ec2NodeHoldNotRequired = "not_required"
)

// canonicalNodesReady reports whether the #805 PR1 canonical-nodes-committed
// phase is published for this intent's scope generation, and which way the EC2
// instance-node hold resolved. The phase key is derived the same way
// DomainAWSResourceMaterialization publishes it (see [gpphase.KeyFromScope]),
// so the lookup matches the published row. A nil lookup keeps the gate open for
// test wiring. A non-nil error is a failed posture probe, never a readiness
// miss.
func (h ObservabilityCoverageMaterializationHandler) canonicalNodesReady(
	ctx context.Context,
	intent reducercontract.Intent,
) (bool, string, error) {
	if h.ReadinessLookup == nil {
		return true, ec2NodeHoldSatisfied, nil
	}
	key, ok := gpphase.KeyFromScope(intent.ScopeID, intent.GenerationID, intent.EntityKeys, gpphase.KeyspaceCloudResourceUID)
	if !ok {
		return false, "", nil
	}
	ready, found := h.ReadinessLookup(key, gpphase.PhaseCanonicalNodesCommitted)
	if !found || !ready {
		return false, "", nil
	}
	// The AWS_COVERS_alarm edge resolves against the EC2 instance node, which
	// commits under its own canonical-nodes phase from ec2_instance_posture
	// facts (an aws_ec2_instance aws_resource fact never produces a node row,
	// #5448 CRUX-1). The durable claim gate opens on the AWS phase alone, so
	// this second lookup is what holds the handler until the EC2 node exists:
	// without it the live leaf's first fault-shard run claimed on the AWS
	// phase and wrote 4/5 edges with the alarm edge silently missing. The
	// deferral error is budget-safe (ObservabilityCoverageNodesNotReadyFailureClass
	// is nonCounting), so an early claim retries instead of writing a partial
	// edge set.
	ec2Key, ok := gpphase.KeyFromScope(intent.ScopeID, intent.GenerationID, []string{"ec2_instance_node_materialization:" + intent.ScopeID}, gpphase.KeyspaceCloudResourceUID)
	if !ok {
		return false, "", nil
	}
	if ec2Ready, found := h.ReadinessLookup(ec2Key, gpphase.PhaseCanonicalNodesCommitted); found && ec2Ready {
		return true, ec2NodeHoldSatisfied, nil
	}
	// The EC2 phase is published only for a scope generation that carries an
	// ec2_instance_posture fact: BuildInstanceNodeMaterializationReducerIntent
	// returns ok=false otherwise. Without one the phase never appears, and
	// because the miss is non-counting the intent would retry forever and
	// never dead-letter (#9055). Facts are committed before the intent is
	// claimable, so the probe's answer is stable for the generation.
	hasPosture, err := h.scopeCarriesEC2InstancePosture(ctx, intent)
	if err != nil {
		return false, "", err
	}
	if !hasPosture {
		return true, ec2NodeHoldNotRequired, nil
	}
	return false, "", nil
}

// scopeCarriesEC2InstancePosture reports whether the intent's scope generation
// holds at least one ec2_instance_posture fact, the same condition that creates
// the ec2_instance_node_materialization intent. It filters by kind itself
// because a loader without [factload.FactKindLoader] returns every fact.
func (h ObservabilityCoverageMaterializationHandler) scopeCarriesEC2InstancePosture(
	ctx context.Context,
	intent reducercontract.Intent,
) (bool, error) {
	envelopes, err := factload.LoadFactsForKinds(
		ctx, h.FactLoader, intent.ScopeID, intent.GenerationID,
		[]string{cloud.EC2InstancePostureFactKind},
	)
	if err != nil {
		return false, fmt.Errorf("load ec2 instance posture facts for observability coverage readiness: %w", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == cloud.EC2InstancePostureFactKind {
			return true, nil
		}
	}
	return false, nil
}
