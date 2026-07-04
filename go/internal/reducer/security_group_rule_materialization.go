// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// securityGroupRuleMaterializationDomainDefinition returns the additive
// definition for the :SecurityGroupRule node materialization. It is additive (not
// part of DefaultDomainDefinitions) because the handler requires an explicitly
// wired node writer and FactLoader; registering it without them would silently
// drop every rule node before the edge slice could MATCH it. See issue #1135.
func securityGroupRuleMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainSecurityGroupRuleMaterialization,
		Summary: "materialize aws_security_group_rule facts into canonical port-precise SecurityGroupRule graph nodes",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "security_group_rule_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// SecurityGroupRuleNodeWriter persists canonical :SecurityGroupRule graph nodes.
// Implementations MUST be idempotent by node uid so reducer retries and duplicate
// facts converge on one node rather than duplicating or fabricating graph state.
type SecurityGroupRuleNodeWriter interface {
	WriteSecurityGroupRuleNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error
}

// SecurityGroupRuleMaterializationHandler reduces one rule-node materialization
// follow-up into canonical :SecurityGroupRule node writes. It loads the scope
// generation's aws_resource and aws_security_group_rule facts, resolves each
// rule's SecurityGroup anchor to a committed CloudResource node through a bounded
// in-memory join index, projects the resolved rules into port-precise node rows,
// and hands the bounded batch to the node writer.
//
// After the node write succeeds (or is a legitimate no-op for an empty
// generation), the handler publishes the
// GraphProjectionKeyspaceSecurityGroupRuleUID /
// GraphProjectionPhaseCanonicalNodesCommitted readiness phase. The reachability
// edge slice gates its projection on this phase (alongside the endpoint and SG
// node phases), so edges never resolve against rule nodes that have not committed.
type SecurityGroupRuleMaterializationHandler struct {
	FactLoader FactLoader
	NodeWriter SecurityGroupRuleNodeWriter
	// PhasePublisher records the canonical-nodes-committed readiness phase that
	// gates the reachability edge projection. A nil publisher is a no-op so the
	// additive domain stays safe to register before the edge slice is wired.
	PhasePublisher GraphProjectionPhasePublisher
	// Instruments records the rule-nodes-materialized counter. Nil-safe.
	Instruments *telemetry.Instruments
}

// Handle executes one :SecurityGroupRule node materialization intent.
func (h SecurityGroupRuleMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainSecurityGroupRuleMaterialization {
		return Result{}, fmt.Errorf(
			"security group rule materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("security group rule materialization fact loader is required")
	}
	if h.NodeWriter == nil {
		return Result{}, fmt.Errorf("security group rule materialization node writer is required")
	}

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{facts.AWSResourceFactKind, facts.AWSSecurityGroupRuleFactKind},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for security group rule materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	resourceEnvelopes, ruleEnvelopes := splitSecurityGroupReachabilityEnvelopes(envelopes)

	extractStart := time.Now()
	reach, err := ExtractSecurityGroupReachability(resourceEnvelopes, ruleEnvelopes)
	if err != nil {
		// A non-decode error (transient fact-load, unsupported major, or other
		// fatal condition partitionDecodeFailures did NOT quarantine) fails the
		// whole intent so the durable queue triages it correctly.
		return Result{}, err
	}
	// Per-fact isolation: a malformed aws_resource/aws_security_group_rule fact
	// (a missing required identity field) is quarantined as a visible
	// input_invalid dead-letter — counter + structured error log — while the
	// batch's valid facts still project below.
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainSecurityGroupRuleMaterialization, intent.ScopeID, intent.GenerationID, reach.Quarantined)
	extractDuration := time.Since(extractStart)

	writeStart := time.Now()
	if len(reach.RuleNodes) > 0 {
		if err := h.NodeWriter.WriteSecurityGroupRuleNodes(ctx, reach.RuleNodes, securityGroupReachabilityEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("write canonical security group rule nodes: %w", err)
		}
	}
	writeDuration := time.Since(writeStart)

	// Publish the canonical-nodes-committed phase only after the node write
	// succeeds (or is a legitimate no-op for an empty generation). Publishing
	// before a successful write would let edges resolve against nodes that never
	// committed; not publishing on an empty generation would block the edge slice
	// forever.
	phasePublishStart := time.Now()
	if err := publishIntentGraphPhase(
		ctx,
		h.PhasePublisher,
		intent,
		GraphProjectionKeyspaceSecurityGroupRuleUID,
		GraphProjectionPhaseCanonicalNodesCommitted,
		time.Now().UTC(),
	); err != nil {
		return Result{}, fmt.Errorf("publish canonical security group rule nodes phase: %w", err)
	}
	phasePublishDuration := time.Since(phasePublishStart)

	h.recordRuleNodes(ctx, len(reach.RuleNodes))
	slog.InfoContext(
		ctx, "security group rule materialization completed",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.Domain(string(intent.Domain)),
		slog.Int("resource_fact_count", len(resourceEnvelopes)),
		slog.Int("rule_fact_count", len(ruleEnvelopes)),
		slog.Int("rule_node_count", len(reach.RuleNodes)),
		slog.Int("skipped_unresolved_anchor", reach.Tally.skippedUnresolvedAnchor),
		slog.Int("skipped_unresolved_endpoint", reach.Tally.skippedUnresolvedEndpoint),
		slog.Int("skipped_unknown_source", reach.Tally.skippedUnknownSource),
		slog.Float64("load_facts_duration_seconds", loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", extractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", writeDuration.Seconds()),
		slog.Float64("phase_publish_duration_seconds", phasePublishDuration.Seconds()),
		slog.Float64("total_duration_seconds", time.Since(totalStart).Seconds()),
	)

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainSecurityGroupRuleMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d security group rule node(s) from %d rule fact(s); %d input_invalid fact(s) quarantined",
			len(reach.RuleNodes),
			len(ruleEnvelopes),
			inputInvalidCount,
		),
		CanonicalWrites: len(reach.RuleNodes),
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
	}, nil
}

// recordRuleNodes emits the rule-nodes-materialized counter so an operator can see
// how many port-precise rule nodes one generation committed — the substrate the
// edge slice resolves against. A zero count is itself a signal (every rule named
// an unscanned group or carried an unknown source), so it is recorded even when no
// nodes materialized.
func (h SecurityGroupRuleMaterializationHandler) recordRuleNodes(ctx context.Context, count int) {
	if h.Instruments == nil || h.Instruments.SecurityGroupReachabilityRuleNodes == nil {
		return
	}
	h.Instruments.SecurityGroupReachabilityRuleNodes.Add(ctx, int64(count), metric.WithAttributes(
		telemetry.AttrDomain(string(DomainSecurityGroupRuleMaterialization)),
	))
}
