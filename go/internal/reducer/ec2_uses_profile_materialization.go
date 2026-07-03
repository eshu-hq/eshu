// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// ec2UsesProfileMaterializationDomainDefinition returns the additive definition
// for EC2 USES_PROFILE instance-profile edge projection (#1146 PR-B). It is
// additive (not part of DefaultDomainDefinitions) because the handler requires an
// explicitly wired EC2UsesProfileEdgeWriter and FactLoader; registering it without
// them would silently drop every intent. It mirrors
// s3LogsToMaterializationDomainDefinition (#1144 PR2). See issue #1146 PR-B and
// docs/internal/design/1146-ec2-uses-profile-edge.md.
func ec2UsesProfileMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainEC2UsesProfileMaterialization,
		Summary: "project ec2_instance_posture instance_profile_arn into canonical USES_PROFILE graph edges",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "ec2_uses_profile_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// ec2UsesProfileEvidenceSource tags USES_PROFILE edges written by this reducer so
// the prior-generation retract path scopes its delete to reducer-owned
// instance-profile-usage edges and never touches edges owned by other writers.
const ec2UsesProfileEvidenceSource = "reducer/ec2-uses-profile"

// ec2UsesProfileAWSResourceEntityKeyPrefix and
// ec2UsesProfileInstanceNodeEntityKeyPrefix are the two distinct entity-key
// prefixes under which the USES_PROFILE edge's two endpoint node phases publish
// their cloud_resource_uid / canonical_nodes_committed readiness:
//
//   - the IAM instance-profile target node is published by
//     DomainAWSResourceMaterialization under "aws_resource_materialization:<scope>"
//     (#805), and
//   - the EC2 instance source node is published by
//     DomainEC2InstanceNodeMaterialization under
//     "ec2_instance_node_materialization:<scope>" (#1146 PR-A).
//
// The edge gates on BOTH, exactly like the security-group reachability edge gates
// on three node phases (#1135). The two prefixes are kept in lockstep with the
// projector intents that publish them (buildAWSResourceMaterializationReducerIntent
// and buildEC2InstanceNodeMaterializationReducerIntent) and with the durable
// Postgres claim gate.
const (
	ec2UsesProfileAWSResourceEntityKeyPrefix  = "aws_resource_materialization:"
	ec2UsesProfileInstanceNodeEntityKeyPrefix = "ec2_instance_node_materialization:"
)

// EC2UsesProfileEdgeWriter persists and retracts canonical USES_PROFILE edges
// between a source EC2 instance CloudResource node and the IAM instance-profile
// CloudResource node it uses. Implementations MUST be idempotent by
// (source uid, USES_PROFILE, target uid) so reducer retries and duplicate facts
// converge on one edge, and MUST NOT fabricate endpoint nodes: a row whose source
// or target node is absent is a no-op.
type EC2UsesProfileEdgeWriter interface {
	WriteEC2UsesProfileEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractEC2UsesProfileEdges(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
}

// EC2UsesProfileMaterializationHandler reduces one EC2 USES_PROFILE
// materialization follow-up into canonical instance-profile-usage edge writes.
//
// It is the FIRST edge in Eshu's EC2 blast-radius chain
// (EC2 → instance-profile → role → CAN_ESCALATE_TO). The edge consumes TWO
// CloudResource node families that publish their canonical-nodes-committed phase
// under DIFFERENT entity keys: the IAM instance-profile target node
// (aws_resource_materialization:<scope>, #805) and the EC2 instance source node
// (ec2_instance_node_materialization:<scope>, #1146 PR-A). The handler gates on
// BOTH phases — if either is missing the intent re-enters the durable queue rather
// than resolving an edge against a not-yet-materialized endpoint (a silent missed
// edge). This is the dual-key readiness gate; it mirrors the security-group
// reachability edge's three-phase gate (#1135), generalized to two distinct entity
// keys.
//
// After both gates pass it loads the scope generation's aws_resource and
// ec2_instance_posture facts, resolves each instance_profile_arn to a scanned IAM
// instance-profile CloudResource uid through a bounded in-memory ARN join index
// (no per-edge graph round trip), and hands the resolved batch to the edge writer.
// Cross-account, out-of-scope, and unscanned profiles are counted and logged,
// never written and never dropped silently.
//
// See issue #1146 PR-B and docs/internal/design/1146-ec2-uses-profile-edge.md.
type EC2UsesProfileMaterializationHandler struct {
	FactLoader FactLoader
	EdgeWriter EC2UsesProfileEdgeWriter
	// ReadinessLookup reports whether a canonical-nodes-committed phase has been
	// published for a given phase key. The handler queries it once per endpoint
	// node phase (aws_resource and ec2_instance_node entity keys). A nil lookup
	// keeps the gate open (test wiring); production wires the durable Postgres
	// lookup, and the durable Postgres claim gate is the load-bearing fence.
	ReadinessLookup GraphProjectionReadinessLookup
	// PriorGenerationCheck reports whether the scope has any prior generation. Nil
	// keeps retract behavior conservative (always retract before write).
	PriorGenerationCheck PriorGenerationCheck
	Tracer               trace.Tracer
	Instruments          *telemetry.Instruments
}

// ec2UsesProfileFactKinds is the bounded fact-kind allowlist the handler loads:
// the aws_resource IAM instance-profile node substrate for the join index and the
// ec2_instance_posture facts that drive the edges.
func ec2UsesProfileFactKinds() []string {
	return []string{facts.AWSResourceFactKind, facts.EC2InstancePostureFactKind}
}

// Handle executes one EC2 USES_PROFILE materialization intent.
func (h EC2UsesProfileMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainEC2UsesProfileMaterialization {
		return Result{}, fmt.Errorf(
			"ec2 uses-profile materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("ec2 uses-profile materialization fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("ec2 uses-profile materialization edge writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerEC2UsesProfileMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	// Dual-key readiness gate: a USES_PROFILE edge may only resolve once BOTH the
	// IAM instance-profile node phase AND the EC2 instance node phase have
	// committed for this scope generation. If either is missing, the intent
	// re-enters the durable queue (retryable) rather than writing an edge against
	// a node set that does not exist yet — a silent missed edge.
	if missing, ok := h.firstMissingNodePhase(intent); !ok {
		return Result{}, ec2UsesProfileNodesNotReadyError{
			scopeID:         intent.ScopeID,
			generationID:    intent.GenerationID,
			missingPhaseFor: missing,
		}
	}

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		ec2UsesProfileFactKinds(),
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for ec2 uses-profile materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	resourceEnvelopes, postureEnvelopes := splitEC2UsesProfileEnvelopes(envelopes)

	extractStart := time.Now()
	rows, tally, err := ExtractEC2UsesProfileEdgeRows(resourceEnvelopes, postureEnvelopes)
	if err != nil {
		// A malformed aws_resource payload (a missing required identity field)
		// is a classified input_invalid decode failure; dead-letter the intent
		// instead of resolving an edge against an empty-string node identity.
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
		if err := h.EdgeWriter.RetractEC2UsesProfileEdges(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			ec2UsesProfileEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical ec2 uses-profile edges: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.EdgeWriter.WriteEC2UsesProfileEdges(
			ctx,
			rows,
			intent.ScopeID,
			intent.GenerationID,
			ec2UsesProfileEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical ec2 uses-profile edges: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	h.recordEdgeCounter(ctx, rows)
	h.recordSkipCounter(ctx, tally)
	logEC2UsesProfileMaterializationCompleted(ctx, ec2UsesProfileMaterializationTiming{
		intent:          intent,
		resourceCount:   len(resourceEnvelopes),
		postureCount:    len(postureEnvelopes),
		edgeCount:       len(rows),
		resolvedByMode:  tally.resolved,
		skippedByReason: tally.skipped,
		skipRetract:     skipRetract,
		loadDuration:    loadDuration,
		extractDuration: extractDuration,
		retractDuration: retractDuration,
		writeDuration:   writeDuration,
		totalDuration:   time.Since(totalStart),
	})

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainEC2UsesProfileMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d USES_PROFILE edge(s) from %d posture fact(s); %d profile(s) skipped (source/target unscanned)",
			len(rows),
			len(postureEnvelopes),
			tally.totalSkipped(),
		),
		CanonicalWrites: len(rows),
	}, nil
}

// firstMissingNodePhase reports whether BOTH endpoint node phases are committed
// for this intent's scope generation. It returns ("", true) when both are ready,
// or (keyspace-label, false) naming the first missing phase otherwise. A nil
// lookup keeps the gate open for test wiring; the durable Postgres claim gate is
// the load-bearing fence in production.
//
// Unlike the single-phase edges, the two phases publish under DIFFERENT entity
// keys, so each lookup uses a fixed entity-key derived from the scope id rather
// than the intent's own entity key.
func (h EC2UsesProfileMaterializationHandler) firstMissingNodePhase(intent Intent) (string, bool) {
	if h.ReadinessLookup == nil {
		return "", true
	}
	scopeID := strings.TrimSpace(intent.ScopeID)
	generationID := strings.TrimSpace(intent.GenerationID)
	if scopeID == "" || generationID == "" {
		return "", false
	}

	checks := []struct {
		label     string
		entityKey string
	}{
		{label: "instance_profile_node", entityKey: ec2UsesProfileAWSResourceEntityKeyPrefix + scopeID},
		{label: "instance_node", entityKey: ec2UsesProfileInstanceNodeEntityKeyPrefix + scopeID},
	}
	for _, check := range checks {
		key := GraphProjectionPhaseKey{
			ScopeID:          scopeID,
			AcceptanceUnitID: check.entityKey,
			SourceRunID:      generationID,
			GenerationID:     generationID,
			Keyspace:         GraphProjectionKeyspaceCloudResourceUID,
		}
		ready, found := h.ReadinessLookup(key, GraphProjectionPhaseCanonicalNodesCommitted)
		if !found || !ready {
			return check.label, false
		}
	}
	return "", true
}

// shouldSkipRetract mirrors the AWS relationship domain: skip the prior-edge
// retract on the very first generation for a scope (no prior edges to remove) and
// only on the first attempt, so a retried attempt still cleans up a partial prior
// write.
func (h EC2UsesProfileMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for ec2 uses-profile retract: %w", err)
	}
	return !hasPrior, nil
}

// recordEdgeCounter emits the USES_PROFILE edge-projection counter dimensioned by
// resolution_mode (arn), the contract registered for
// eshu_dp_ec2_uses_profile_edges_total. Each materialized edge row carries its
// resolution mode; skipped profiles go to the skip counter and completion log, not
// this metric, so cardinality stays bounded.
func (h EC2UsesProfileMaterializationHandler) recordEdgeCounter(
	ctx context.Context,
	rows []map[string]any,
) {
	if h.Instruments == nil || h.Instruments.EC2UsesProfileEdges == nil {
		return
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[anyToString(row["resolution_mode"])]++
	}
	for mode, count := range counts {
		h.Instruments.EC2UsesProfileEdges.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrResolutionMode(mode),
		))
	}
}

// recordSkipCounter emits the USES_PROFILE skip counter dimensioned by the bounded
// skip_reason set (source_unresolved / target_unresolved), the contract registered
// for eshu_dp_ec2_uses_profile_skipped_total. It lets an operator see how many
// posture facts named an instance profile that produced no edge because an
// endpoint was not scanned, without a per-fact log line. Blank-profile facts (no
// attached profile) are not counted here.
func (h EC2UsesProfileMaterializationHandler) recordSkipCounter(
	ctx context.Context,
	tally ec2UsesProfileEdgeTally,
) {
	if h.Instruments == nil || h.Instruments.EC2UsesProfileSkipped == nil {
		return
	}
	for reason, count := range tally.skipped {
		if count == 0 {
			continue
		}
		h.Instruments.EC2UsesProfileSkipped.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrSkipReason(reason),
		))
	}
}

// splitEC2UsesProfileEnvelopes partitions a mixed envelope slice into aws_resource
// and ec2_instance_posture facts in one pass so the join index and posture facts
// are built from a single bounded load.
func splitEC2UsesProfileEnvelopes(envelopes []facts.Envelope) (resources, postures []facts.Envelope) {
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.AWSResourceFactKind:
			resources = append(resources, env)
		case facts.EC2InstancePostureFactKind:
			postures = append(postures, env)
		}
	}
	return resources, postures
}

// ec2UsesProfileNodesNotReadyError marks the dual-key readiness-gate miss as
// retryable so the durable queue re-runs the intent once both endpoint node phases
// commit, instead of failing terminally or writing an edge against absent nodes.
type ec2UsesProfileNodesNotReadyError struct {
	scopeID         string
	generationID    string
	missingPhaseFor string
}

func (e ec2UsesProfileNodesNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical %s nodes not committed for scope %s generation %s",
		e.missingPhaseFor,
		e.scopeID,
		e.generationID,
	)
}

func (ec2UsesProfileNodesNotReadyError) Retryable() bool { return true }

func (ec2UsesProfileNodesNotReadyError) FailureClass() string {
	return "ec2_uses_profile_nodes_not_ready"
}

// ec2UsesProfileMaterializationTiming groups stage durations and the edge tally so
// the completion log identifies fact-load, resolve, retract, and graph-write time,
// plus how many USES_PROFILE edges materialized and which profiles were skipped.
type ec2UsesProfileMaterializationTiming struct {
	intent          Intent
	resourceCount   int
	postureCount    int
	edgeCount       int
	resolvedByMode  map[string]int
	skippedByReason map[string]int
	skipRetract     bool
	loadDuration    time.Duration
	extractDuration time.Duration
	retractDuration time.Duration
	writeDuration   time.Duration
	totalDuration   time.Duration
}

func logEC2UsesProfileMaterializationCompleted(
	ctx context.Context,
	timing ec2UsesProfileMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "ec2 uses-profile materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("resource_fact_count", timing.resourceCount),
		slog.Int("posture_fact_count", timing.postureCount),
		slog.Int("edge_count", timing.edgeCount),
		slog.String("resolved_by_mode", formatTally(timing.resolvedByMode)),
		slog.String("skipped_by_reason", formatTally(timing.skippedByReason)),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("resolve_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
