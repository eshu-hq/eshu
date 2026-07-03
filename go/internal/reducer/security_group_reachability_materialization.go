// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// securityGroupReachabilityEvidenceSource tags the :SecurityGroupRule nodes and
// the ALLOWS_INGRESS/EGRESS + TO edges this reducer writes so the
// prior-generation retract scopes its delete to reducer-owned reachability edges
// and never touches edges or nodes owned by other writers.
const securityGroupReachabilityEvidenceSource = "reducer/security-group-reachability"

// Edge-type labels for the reachability edges-materialized counter. They are the
// bounded edge_type metric dimension members for
// eshu_dp_security_group_reachability_edges_total.
const (
	securityGroupEdgeTypeSGRule       = "sg_rule"
	securityGroupEdgeTypeRuleEndpoint = "rule_endpoint"
)

// Skip-reason labels for the reachability skipped counter. They are the bounded
// skip_reason metric dimension members for
// eshu_dp_security_group_reachability_skipped_total.
const (
	securityGroupSkipUnresolvedAnchor   = "unresolved_anchor"
	securityGroupSkipUnresolvedEndpoint = "unresolved_endpoint"
	securityGroupSkipUnknownSource      = "unknown_source"
)

// securityGroupReachabilityMaterializationDomainDefinition returns the additive
// definition for the security-group network-reachability edge projection. It is
// additive (not part of DefaultDomainDefinitions) because the handler requires an
// explicitly wired SecurityGroupReachabilityWriter and FactLoader; registering it
// without them would silently drop every reachability intent. See issue #1135.
func securityGroupReachabilityMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainSecurityGroupReachabilityMaterialization,
		Summary: "project aws_security_group_rule facts into the Option D network-reachability graph (SecurityGroupRule nodes + ALLOWS_INGRESS/EGRESS + TO edges)",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "security_group_reachability_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// SecurityGroupReachabilityWriter persists and retracts the Option D
// network-reachability graph: idempotent :SecurityGroupRule nodes, the
// SecurityGroup -> rule direction edges, and the rule -> endpoint TO edges.
// Implementations MUST be idempotent by node uid and by (source uid, rel type,
// target uid) so reducer retries and duplicate facts converge, and MUST NOT
// fabricate endpoint nodes: a row whose anchor or endpoint node is absent is a
// no-op.
type SecurityGroupReachabilityWriter interface {
	WriteSecurityGroupRuleNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error
	WriteSecurityGroupSGRuleEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	WriteSecurityGroupRuleEndpointEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractSecurityGroupReachability(ctx context.Context, scopeIDs []string, generationID, evidenceSource string) error
}

// securityGroupReachabilityGateKeyspaces are the three canonical-nodes keyspaces
// the edge domain gates on, in a stable order for deterministic gate evaluation
// and logging. An edge may only resolve once the rule nodes, the CIDR/prefix
// endpoint nodes, AND the SG CloudResource nodes have all committed for this
// scope generation.
var securityGroupReachabilityGateKeyspaces = []GraphProjectionKeyspace{
	GraphProjectionKeyspaceSecurityGroupRuleUID,
	GraphProjectionKeyspaceSecurityGroupEndpointUID,
	GraphProjectionKeyspaceCloudResourceUID,
}

// SecurityGroupReachabilityMaterializationHandler reduces one reachability
// follow-up into the Option D graph. It gates on the three
// canonical-nodes-committed phases (rule nodes, endpoint nodes, SG nodes), loads
// the scope generation's aws_resource and aws_security_group_rule facts, resolves
// each rule's SG anchor and source endpoint through a bounded in-memory join
// index (no per-edge graph round trip), writes the rule nodes first and then the
// edges so each edge has a committed node to MATCH, and counts unresolved/unknown
// rules instead of dropping them silently.
type SecurityGroupReachabilityMaterializationHandler struct {
	FactLoader FactLoader
	Writer     SecurityGroupReachabilityWriter
	// ReadinessLookup reports whether a canonical-nodes-committed phase has been
	// published for the intent's scope generation on a given keyspace. A nil lookup
	// keeps the gate open (test wiring); production wires the durable Postgres
	// lookup.
	ReadinessLookup GraphProjectionReadinessLookup
	// PriorGenerationCheck reports whether the scope has any prior generation. Nil
	// keeps retract behavior conservative (always retract before write).
	PriorGenerationCheck PriorGenerationCheck
	Tracer               trace.Tracer
	Instruments          *telemetry.Instruments
}

// Handle executes one security-group reachability materialization intent.
func (h SecurityGroupReachabilityMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainSecurityGroupReachabilityMaterialization {
		return Result{}, fmt.Errorf(
			"security group reachability materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("security group reachability materialization fact loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("security group reachability materialization writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerSecurityGroupReachabilityMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	// Triple readiness gate: edges may only resolve against nodes the same
	// generation already committed. If any of the three canonical-nodes phases is
	// not yet published, the intent re-enters the durable queue (retryable) rather
	// than writing edges against a node set that does not exist yet.
	if notReady := h.firstNotReadyKeyspace(intent); notReady != "" {
		return Result{}, securityGroupReachabilityNotReadyError{
			scopeID:      intent.ScopeID,
			generationID: intent.GenerationID,
			keyspace:     notReady,
		}
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
		return Result{}, fmt.Errorf("load facts for security group reachability materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	resourceEnvelopes, ruleEnvelopes := splitSecurityGroupReachabilityEnvelopes(envelopes)

	extractStart := time.Now()
	reach, err := ExtractSecurityGroupReachability(resourceEnvelopes, ruleEnvelopes)
	if err != nil {
		// A malformed aws_resource payload (a missing required identity field)
		// is a classified input_invalid decode failure; dead-letter the intent
		// instead of resolving reachability against an empty-string node identity.
		return Result{}, err
	}
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.Writer.RetractSecurityGroupReachability(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			securityGroupReachabilityEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical security group reachability edges: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	// Write the rule nodes before the edges so each edge has a committed node to
	// MATCH. The node write and both edge writes are each idempotent by uid, so a
	// retry after a partial failure converges on the same graph.
	writeStart := time.Now()
	if len(reach.RuleNodes) > 0 {
		if err := h.Writer.WriteSecurityGroupRuleNodes(ctx, reach.RuleNodes, securityGroupReachabilityEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("write canonical security group rule nodes: %w", err)
		}
	}
	if len(reach.SGRuleEdges) > 0 {
		if err := h.Writer.WriteSecurityGroupSGRuleEdges(ctx, reach.SGRuleEdges, intent.ScopeID, intent.GenerationID, securityGroupReachabilityEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("write canonical security group sg-rule edges: %w", err)
		}
	}
	if len(reach.RuleEndpointEdges) > 0 {
		if err := h.Writer.WriteSecurityGroupRuleEndpointEdges(ctx, reach.RuleEndpointEdges, intent.ScopeID, intent.GenerationID, securityGroupReachabilityEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("write canonical security group rule-endpoint edges: %w", err)
		}
	}
	writeDuration := time.Since(writeStart)

	h.recordTally(ctx, reach)
	logSecurityGroupReachabilityCompleted(ctx, securityGroupReachabilityTiming{
		intent:          intent,
		resourceCount:   len(resourceEnvelopes),
		ruleCount:       len(ruleEnvelopes),
		ruleNodeCount:   len(reach.RuleNodes),
		sgEdgeCount:     len(reach.SGRuleEdges),
		toEdgeCount:     len(reach.RuleEndpointEdges),
		tally:           reach.Tally,
		skipRetract:     skipRetract,
		loadDuration:    loadDuration,
		extractDuration: extractDuration,
		retractDuration: retractDuration,
		writeDuration:   writeDuration,
		totalDuration:   time.Since(totalStart),
	})

	canonicalWrites := len(reach.RuleNodes) + len(reach.SGRuleEdges) + len(reach.RuleEndpointEdges)
	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainSecurityGroupReachabilityMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d security group rule node(s) + %d sg-rule edge(s) + %d rule-endpoint edge(s) from %d rule fact(s); %d skipped",
			len(reach.RuleNodes),
			len(reach.SGRuleEdges),
			len(reach.RuleEndpointEdges),
			len(ruleEnvelopes),
			reach.Tally.total(),
		),
		CanonicalWrites: canonicalWrites,
	}, nil
}

// firstNotReadyKeyspace returns the first of the three gate keyspaces whose
// canonical-nodes-committed phase is not yet published for this intent's scope
// generation, or "" when all three are ready. A nil ReadinessLookup keeps the
// gate open for test wiring. The phase key is derived the same way each node
// materializer publishes it, so the lookup matches the published row.
func (h SecurityGroupReachabilityMaterializationHandler) firstNotReadyKeyspace(intent Intent) GraphProjectionKeyspace {
	if h.ReadinessLookup == nil {
		return ""
	}
	now := time.Now().UTC()
	for _, keyspace := range securityGroupReachabilityGateKeyspaces {
		state, ok := graphProjectionPhaseStateForIntent(intent, keyspace, GraphProjectionPhaseCanonicalNodesCommitted, now)
		if !ok {
			return keyspace
		}
		ready, found := h.ReadinessLookup(state.Key, GraphProjectionPhaseCanonicalNodesCommitted)
		if !found || !ready {
			return keyspace
		}
	}
	return ""
}

// shouldSkipRetract mirrors the AWS relationship and Kubernetes correlation edge
// domains: skip the prior-edge retract on the very first generation for a scope
// (no prior edges to remove) and only on the first attempt, so a retried attempt
// still cleans up a partial prior write.
func (h SecurityGroupReachabilityMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for security group reachability retract: %w", err)
	}
	return !hasPrior, nil
}

// recordTally emits the reachability edge counters: edges committed split by
// edge_type, and skipped rules split by skip_reason. The rule-node counter is the
// rule-node materialization domain's job; this edge domain owns the edge and skip
// surfaces. The skip counters are the honest graceful-degradation surface so an
// operator can see why reachability edges are missing without a per-rule log line.
func (h SecurityGroupReachabilityMaterializationHandler) recordTally(ctx context.Context, reach SecurityGroupReachabilityResult) {
	if h.Instruments == nil {
		return
	}
	if h.Instruments.SecurityGroupReachabilityEdges != nil {
		h.Instruments.SecurityGroupReachabilityEdges.Add(ctx, int64(len(reach.SGRuleEdges)), metric.WithAttributes(
			telemetry.AttrEdgeType(securityGroupEdgeTypeSGRule),
		))
		h.Instruments.SecurityGroupReachabilityEdges.Add(ctx, int64(len(reach.RuleEndpointEdges)), metric.WithAttributes(
			telemetry.AttrEdgeType(securityGroupEdgeTypeRuleEndpoint),
		))
	}
	if h.Instruments.SecurityGroupReachabilitySkipped != nil {
		h.recordSkip(ctx, securityGroupSkipUnresolvedAnchor, reach.Tally.skippedUnresolvedAnchor)
		h.recordSkip(ctx, securityGroupSkipUnresolvedEndpoint, reach.Tally.skippedUnresolvedEndpoint)
		h.recordSkip(ctx, securityGroupSkipUnknownSource, reach.Tally.skippedUnknownSource)
	}
}

// recordSkip emits one skip-reason data point. A zero count is still recorded so
// the time series exists and an operator can chart a rising skip rate from zero.
func (h SecurityGroupReachabilityMaterializationHandler) recordSkip(ctx context.Context, reason string, count int) {
	h.Instruments.SecurityGroupReachabilitySkipped.Add(ctx, int64(count), metric.WithAttributes(
		telemetry.AttrSkipReason(reason),
	))
}

// splitSecurityGroupReachabilityEnvelopes partitions a mixed envelope slice into
// resource and security-group-rule facts in one pass so the join index and the
// rule facts are built from a single bounded load.
func splitSecurityGroupReachabilityEnvelopes(envelopes []facts.Envelope) (resources, rules []facts.Envelope) {
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.AWSResourceFactKind:
			resources = append(resources, env)
		case facts.AWSSecurityGroupRuleFactKind:
			rules = append(rules, env)
		}
	}
	return resources, rules
}

// securityGroupReachabilityNotReadyError marks a readiness-gate miss as retryable
// so the durable queue re-runs the intent once the missing canonical nodes
// commit, instead of failing terminally or writing edges against absent nodes.
type securityGroupReachabilityNotReadyError struct {
	scopeID      string
	generationID string
	keyspace     GraphProjectionKeyspace
}

func (e securityGroupReachabilityNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical nodes not committed on keyspace %s for scope %s generation %s",
		e.keyspace, e.scopeID, e.generationID,
	)
}

func (securityGroupReachabilityNotReadyError) Retryable() bool { return true }

func (securityGroupReachabilityNotReadyError) FailureClass() string {
	return "security_group_reachability_nodes_not_ready"
}

// securityGroupReachabilityTiming groups stage durations and the resolution tally
// so the completion log identifies fact-load, extraction, retract, and graph-write
// time, plus why rules lost edges.
type securityGroupReachabilityTiming struct {
	intent          Intent
	resourceCount   int
	ruleCount       int
	ruleNodeCount   int
	sgEdgeCount     int
	toEdgeCount     int
	tally           securityGroupReachabilityTally
	skipRetract     bool
	loadDuration    time.Duration
	extractDuration time.Duration
	retractDuration time.Duration
	writeDuration   time.Duration
	totalDuration   time.Duration
}

func logSecurityGroupReachabilityCompleted(ctx context.Context, timing securityGroupReachabilityTiming) {
	slog.InfoContext(
		ctx, "security group reachability materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("resource_fact_count", timing.resourceCount),
		slog.Int("rule_fact_count", timing.ruleCount),
		slog.Int("rule_node_count", timing.ruleNodeCount),
		slog.Int("sg_rule_edge_count", timing.sgEdgeCount),
		slog.Int("rule_endpoint_edge_count", timing.toEdgeCount),
		slog.Int("skipped_unresolved_anchor", timing.tally.skippedUnresolvedAnchor),
		slog.Int("skipped_unresolved_endpoint", timing.tally.skippedUnresolvedEndpoint),
		slog.Int("skipped_unknown_source", timing.tally.skippedUnknownSource),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
