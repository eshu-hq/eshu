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
)

func iamInstanceProfileRoleMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainIAMInstanceProfileRoleMaterialization,
		Summary: "project IAM instance-profile role_arns into canonical HAS_ROLE graph edges",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "iam_instance_profile_role_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

const iamInstanceProfileRoleEvidenceSource = "reducer/iam-instance-profile-role"

// IAMInstanceProfileRoleEdgeWriter persists and retracts canonical HAS_ROLE
// edges between IAM instance-profile CloudResource nodes and IAM role
// CloudResource nodes. Implementations MUST be idempotent by
// (profile uid, HAS_ROLE, role uid), and MUST NOT fabricate endpoint nodes.
type IAMInstanceProfileRoleEdgeWriter interface {
	WriteIAMInstanceProfileRoleEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractIAMInstanceProfileRoleEdges(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
}

// IAMInstanceProfileRoleMaterializationHandler projects scanned IAM instance
// profiles into HAS_ROLE edges. It consumes only aws_resource facts: profile
// resources carry role_arns, and target roles are aws_iam_role resources in the
// same generation. The handler gates on the cloud_resource_uid canonical-nodes
// phase so edges never resolve against uncommitted CloudResource nodes.
type IAMInstanceProfileRoleMaterializationHandler struct {
	FactLoader FactLoader
	EdgeWriter IAMInstanceProfileRoleEdgeWriter
	// ReadinessLookup reports whether the canonical-nodes-committed phase has
	// been published for the intent's scope generation. A nil lookup keeps the
	// gate open for tests; production wires the durable Postgres lookup.
	ReadinessLookup GraphProjectionReadinessLookup
	// PriorGenerationCheck reports whether the scope has any prior generation.
	// Nil keeps retract behavior conservative.
	PriorGenerationCheck PriorGenerationCheck
	Tracer               trace.Tracer
	Instruments          *telemetry.Instruments
}

func iamInstanceProfileRoleFactKinds() []string {
	return []string{facts.AWSResourceFactKind}
}

func (h IAMInstanceProfileRoleMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainIAMInstanceProfileRoleMaterialization {
		return Result{}, fmt.Errorf(
			"iam instance-profile role materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("iam instance-profile role materialization fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("iam instance-profile role materialization edge writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerIAMInstanceProfileRoleMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	if !h.canonicalNodesReady(intent) {
		return Result{}, iamInstanceProfileRoleNodesNotReadyError{
			scopeID:      intent.ScopeID,
			generationID: intent.GenerationID,
		}
	}

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		iamInstanceProfileRoleFactKinds(),
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for iam instance-profile role materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	extractStart := time.Now()
	rows, tally := ExtractIAMInstanceProfileRoleEdgeRows(envelopes)
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.EdgeWriter.RetractIAMInstanceProfileRoleEdges(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			iamInstanceProfileRoleEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical iam instance-profile HAS_ROLE edges: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.EdgeWriter.WriteIAMInstanceProfileRoleEdges(
			ctx,
			rows,
			intent.ScopeID,
			intent.GenerationID,
			iamInstanceProfileRoleEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical iam instance-profile HAS_ROLE edges: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	h.recordEdgeCounter(ctx, rows)
	h.recordSkipCounter(ctx, tally)
	logIAMInstanceProfileRoleMaterializationCompleted(ctx, iamInstanceProfileRoleMaterializationTiming{
		intent:          intent,
		resourceCount:   len(envelopes),
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
		Domain:   DomainIAMInstanceProfileRoleMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d HAS_ROLE edge(s) from %d aws resource fact(s); %d profile role(s) skipped (missing profile identity or unscanned target role)",
			len(rows),
			len(envelopes),
			tally.totalSkipped(),
		),
		CanonicalWrites: len(rows),
	}, nil
}

func (h IAMInstanceProfileRoleMaterializationHandler) canonicalNodesReady(intent Intent) bool {
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
	ready, found := h.ReadinessLookup(state.Key, GraphProjectionPhaseCanonicalNodesCommitted)
	return found && ready
}

func (h IAMInstanceProfileRoleMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for iam instance-profile role retract: %w", err)
	}
	return !hasPrior, nil
}

func (h IAMInstanceProfileRoleMaterializationHandler) recordEdgeCounter(
	ctx context.Context,
	rows []map[string]any,
) {
	if h.Instruments == nil || h.Instruments.IAMInstanceProfileRoleEdges == nil {
		return
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[anyToString(row["resolution_mode"])]++
	}
	for mode, count := range counts {
		h.Instruments.IAMInstanceProfileRoleEdges.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrResolutionMode(mode),
		))
	}
}

func (h IAMInstanceProfileRoleMaterializationHandler) recordSkipCounter(
	ctx context.Context,
	tally iamInstanceProfileRoleEdgeTally,
) {
	if h.Instruments == nil || h.Instruments.IAMInstanceProfileRoleSkipped == nil {
		return
	}
	for reason, count := range tally.skipped {
		if count == 0 {
			continue
		}
		h.Instruments.IAMInstanceProfileRoleSkipped.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrSkipReason(reason),
		))
	}
}

type iamInstanceProfileRoleNodesNotReadyError struct {
	scopeID      string
	generationID string
}

func (e iamInstanceProfileRoleNodesNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical cloud resource nodes not committed for scope %s generation %s",
		e.scopeID,
		e.generationID,
	)
}

func (iamInstanceProfileRoleNodesNotReadyError) Retryable() bool { return true }

func (iamInstanceProfileRoleNodesNotReadyError) FailureClass() string {
	return "iam_instance_profile_role_nodes_not_ready"
}

type iamInstanceProfileRoleMaterializationTiming struct {
	intent          Intent
	resourceCount   int
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

func logIAMInstanceProfileRoleMaterializationCompleted(
	ctx context.Context,
	timing iamInstanceProfileRoleMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "iam instance-profile role materialization completed",
		slog.String(telemetry.LogKeyScopeID, timing.intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, timing.intent.GenerationID),
		slog.String(telemetry.LogKeyDomain, string(timing.intent.Domain)),
		slog.Int("resource_fact_count", timing.resourceCount),
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
