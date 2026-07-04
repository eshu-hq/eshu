// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

// ec2InstanceNodeMaterializationDomainDefinition returns the additive definition
// for EC2 instance CloudResource node materialization (#1146 PR-A). It is additive
// (not part of DefaultDomainDefinitions) because the handler requires an
// explicitly wired EC2InstanceNodeWriter and FactLoader; registering it without
// them would silently drop every ec2_instance_posture fact before it reached the
// graph. The future USES_PROFILE edge slice (#1146 PR-B) joins against the
// CloudResource nodes this domain commits.
func ec2InstanceNodeMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainEC2InstanceNodeMaterialization,
		Summary: "materialize ec2_instance_posture facts into canonical EC2 instance CloudResource graph nodes",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "ec2_instance_node_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// ec2InstanceEvidenceSource tags EC2 instance CloudResource nodes written by this
// reducer so the prior-generation retract path (and the future USES_PROFILE edge
// projection) can scope its writes to reducer-owned EC2 instance materialization,
// distinct from the #805 aws_resource node materialization.
const ec2InstanceEvidenceSource = "reducer/ec2-instances"

// EC2InstanceNodeWriter persists canonical EC2 instance CloudResource graph nodes
// from extracted node rows. Implementations MUST be idempotent by node uid (the
// canonical cloud_resource_uid) so reducer retries and duplicate posture facts
// converge on one node rather than duplicating or fabricating graph state.
type EC2InstanceNodeWriter interface {
	WriteEC2InstanceNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error
}

// EC2InstanceNodeMaterializationHandler reduces one EC2 instance node
// materialization follow-up into canonical CloudResource node writes. It loads the
// scope generation's ec2_instance_posture facts, projects them into deterministic
// node rows keyed by the canonical cloud_resource_uid, and hands the bounded batch
// to the node writer.
//
// The EC2 scanner deliberately does not emit an aws_resource inventory fact for
// instances (instances are high-cardinality and ephemeral); this handler is the
// only path that materializes an EC2 instance as a graph node. It intentionally
// does not write edges: the USES_PROFILE edge (#1146 PR-B) resolves a workload's
// instance-profile arn to its CloudResource node in a separate, gated stage,
// mirroring how the AWS relationship edge projection (#805) joins against
// CloudResource nodes.
//
// After the canonical node write succeeds, the handler publishes the
// GraphProjectionKeyspaceCloudResourceUID /
// GraphProjectionPhaseCanonicalNodesCommitted readiness phase under the intent's
// own (distinct) entity key. PR-B gates its edge projection on this phase, so the
// edge never resolves against a generation whose instance nodes have not committed.
type EC2InstanceNodeMaterializationHandler struct {
	FactLoader FactLoader
	NodeWriter EC2InstanceNodeWriter
	// PhasePublisher records the canonical-nodes-committed readiness phase that
	// gates the USES_PROFILE edge projection. A nil publisher is a no-op so the
	// additive domain stays safe to register before PR-B is wired.
	PhasePublisher GraphProjectionPhasePublisher
	// Instruments records the nodes-materialized and nodes-skipped counters.
	// Nil-safe.
	Instruments *telemetry.Instruments
}

// Handle executes one EC2 instance node materialization intent.
func (h EC2InstanceNodeMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainEC2InstanceNodeMaterialization {
		return Result{}, fmt.Errorf(
			"ec2 instance node materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("ec2 instance node materialization fact loader is required")
	}
	if h.NodeWriter == nil {
		return Result{}, fmt.Errorf("ec2 instance node materialization node writer is required")
	}

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{facts.EC2InstancePostureFactKind},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for ec2 instance node materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	extractStart := time.Now()
	rows, skipped, quarantined, err := ExtractEC2InstanceNodeRowsWithSkips(envelopes)
	if err != nil {
		// A non-decode error (transient fact-load, unsupported major, or other
		// fatal condition partitionDecodeFailures did NOT quarantine) fails the
		// whole intent so the durable queue triages it correctly.
		return Result{}, err
	}
	// Per-fact isolation: a malformed ec2_instance_posture fact (a missing
	// required identity field) is quarantined as a visible input_invalid
	// dead-letter — counter + structured error log — while the batch's valid
	// facts still materialize below.
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainEC2InstanceNodeMaterialization, intent.ScopeID, intent.GenerationID, quarantined)
	extractDuration := time.Since(extractStart)

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.NodeWriter.WriteEC2InstanceNodes(ctx, rows, ec2InstanceEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("write canonical ec2 instance nodes: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	// Publish the canonical-nodes-committed readiness phase only after the node
	// write succeeds (or is a legitimate no-op for an empty generation). PR-B gates
	// its USES_PROFILE edge projection on this phase: publishing before a successful
	// write would let edges resolve against nodes that never committed, and not
	// publishing on an empty generation would block PR-B forever.
	phasePublishStart := time.Now()
	if err := publishIntentGraphPhase(
		ctx,
		h.PhasePublisher,
		intent,
		GraphProjectionKeyspaceCloudResourceUID,
		GraphProjectionPhaseCanonicalNodesCommitted,
		time.Now().UTC(),
	); err != nil {
		return Result{}, fmt.Errorf("publish canonical ec2 instance nodes phase: %w", err)
	}
	phasePublishDuration := time.Since(phasePublishStart)

	h.recordNodesMaterialized(ctx, len(rows))
	h.recordNodesSkipped(ctx, skipped)
	logEC2InstanceNodeMaterializationCompleted(ctx, ec2InstanceNodeMaterializationTiming{
		intent:               intent,
		factCount:            len(envelopes),
		nodeCount:            len(rows),
		skipped:              skipped,
		loadDuration:         loadDuration,
		extractDuration:      extractDuration,
		writeDuration:        writeDuration,
		phasePublishDuration: phasePublishDuration,
		totalDuration:        time.Since(totalStart),
	})

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainEC2InstanceNodeMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d canonical ec2 instance node(s) from %d posture fact(s); %d input_invalid fact(s) quarantined",
			len(rows),
			len(envelopes),
			inputInvalidCount,
		),
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
		CanonicalWrites: len(rows),
	}, nil
}

// recordNodesMaterialized emits the EC2InstanceNodes counter so an operator can
// see how many instance nodes one generation committed — the substrate the later
// USES_PROFILE edge slice resolves against. A zero count for a non-empty
// generation is itself a signal (every posture fact lacked an identity), so the
// counter is recorded even when no rows materialized.
func (h EC2InstanceNodeMaterializationHandler) recordNodesMaterialized(ctx context.Context, count int) {
	if h.Instruments == nil || h.Instruments.EC2InstanceNodes == nil {
		return
	}
	h.Instruments.EC2InstanceNodes.Add(ctx, int64(count), metric.WithAttributes(
		telemetry.AttrDomain(string(DomainEC2InstanceNodeMaterialization)),
	))
}

// recordNodesSkipped emits the EC2InstanceNodesSkipped counter dimensioned by the
// conservative skip reason (missing_identity / tombstone) so an operator can see
// which posture facts produced no node and why, without a per-fact log line.
func (h EC2InstanceNodeMaterializationHandler) recordNodesSkipped(ctx context.Context, skipped ec2InstanceSkipTally) {
	if h.Instruments == nil || h.Instruments.EC2InstanceNodesSkipped == nil {
		return
	}
	for reason, count := range skipped {
		if count == 0 {
			continue
		}
		h.Instruments.EC2InstanceNodesSkipped.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrSkipReason(reason),
		))
	}
}
