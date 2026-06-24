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

// iamCanAssumeMaterializationDomainDefinition returns the additive definition
// for IAM CAN_ASSUME trust-graph edge projection. It is additive (not part of
// DefaultDomainDefinitions) because the handler requires an explicitly wired
// IAMCanAssumeEdgeWriter and FactLoader; registering it without them would
// silently drop every intent. It mirrors
// awsRelationshipMaterializationDomainDefinition (#805 PR2). See issue #1134 PR2
// and docs/internal/design/1134-iam-can-assume-trust-graph.md.
func iamCanAssumeMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainIAMCanAssumeMaterialization,
		Summary: "project aws_iam_permission trust statements into canonical CAN_ASSUME graph edges",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "iam_can_assume_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// iamCanAssumeEvidenceSource tags CAN_ASSUME edges written by this reducer so
// the prior-generation retract path scopes its delete to reducer-owned trust
// edges and never touches edges owned by other writers.
const iamCanAssumeEvidenceSource = "reducer/iam-can-assume"

// IAMCanAssumeEdgeWriter persists and retracts canonical CAN_ASSUME edges
// between an assuming-principal CloudResource node and the role-with-trust-policy
// CloudResource it may assume. Implementations MUST be idempotent by
// (principal uid, CAN_ASSUME, role uid) so reducer retries and duplicate facts
// converge on one edge, and MUST NOT fabricate endpoint nodes: a row whose
// principal or role node is absent is a no-op.
type IAMCanAssumeEdgeWriter interface {
	WriteIAMCanAssumeEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractIAMCanAssumeEdges(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
}

// IAMCanAssumeMaterializationHandler reduces one IAM CAN_ASSUME materialization
// follow-up into canonical trust-graph edge writes. It gates on the
// GraphProjectionPhaseCanonicalNodesCommitted readiness phase that
// DomainAWSResourceMaterialization (#805 PR1) publishes on the CloudResource
// keyspace, so trust edges never resolve against a generation whose nodes have
// not committed. It then loads the scope generation's aws_resource and
// aws_iam_permission facts, resolves the role-with-trust-policy and each
// assume-principal to CloudResource uids through a bounded in-memory join index
// (no per-edge graph round trip), and hands the resolved batch to the edge
// writer. External, AWS-service, wildcard, account-root, and unscanned
// principals are counted and logged, never written and never dropped silently.
//
// See issue #1134 PR2 and
// docs/internal/design/1134-iam-can-assume-trust-graph.md.
type IAMCanAssumeMaterializationHandler struct {
	FactLoader FactLoader
	EdgeWriter IAMCanAssumeEdgeWriter
	// ReadinessLookup reports whether the canonical-nodes-committed phase has
	// been published for the intent's scope generation. A nil lookup keeps the
	// gate open (test wiring); production wires the durable Postgres lookup.
	ReadinessLookup GraphProjectionReadinessLookup
	// PriorGenerationCheck reports whether the scope has any prior generation.
	// Nil keeps retract behavior conservative (always retract before write).
	PriorGenerationCheck PriorGenerationCheck
	Tracer               trace.Tracer
	Instruments          *telemetry.Instruments
}

// iamCanAssumeFactKinds is the bounded fact-kind allowlist the handler loads:
// the aws_resource node substrate for the join index and the aws_iam_permission
// trust statements that drive the edges.
func iamCanAssumeFactKinds() []string {
	return []string{facts.AWSResourceFactKind, facts.AWSIAMPermissionFactKind}
}

// Handle executes one IAM CAN_ASSUME materialization intent.
func (h IAMCanAssumeMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainIAMCanAssumeMaterialization {
		return Result{}, fmt.Errorf(
			"iam can-assume materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("iam can-assume materialization fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("iam can-assume materialization edge writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerIAMCanAssumeMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	// Readiness gate: trust edges may only resolve against nodes the same
	// generation already committed. If the canonical-nodes phase is not yet
	// published, the intent re-enters the durable queue (retryable) rather than
	// writing edges against a node set that does not exist yet.
	if !h.canonicalNodesReady(intent) {
		return Result{}, iamCanAssumeNodesNotReadyError{
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
		iamCanAssumeFactKinds(),
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for iam can-assume materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	resourceEnvelopes, permissionEnvelopes := splitIAMCanAssumeEnvelopes(envelopes)

	extractStart := time.Now()
	rows, tally := ExtractIAMCanAssumeEdgeRows(resourceEnvelopes, permissionEnvelopes)
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.EdgeWriter.RetractIAMCanAssumeEdges(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			iamCanAssumeEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical iam can-assume edges: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.EdgeWriter.WriteIAMCanAssumeEdges(
			ctx,
			rows,
			intent.ScopeID,
			intent.GenerationID,
			iamCanAssumeEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical iam can-assume edges: %w", err)
		}
		writeDuration = time.Since(writeStart)
	}

	h.recordEdgeCounter(ctx, rows)
	logIAMCanAssumeMaterializationCompleted(ctx, iamCanAssumeMaterializationTiming{
		intent:          intent,
		resourceCount:   len(resourceEnvelopes),
		permissionCount: len(permissionEnvelopes),
		edgeCount:       len(rows),
		resolvedByKind:  tally.resolved,
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
		Domain:   DomainIAMCanAssumeMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d CAN_ASSUME edge(s) from %d trust statement(s); %d assume-principal(s) skipped (external/service/wildcard/unscanned)",
			len(rows),
			len(permissionEnvelopes),
			tally.totalSkipped(),
		),
		CanonicalWrites: len(rows),
	}, nil
}

// canonicalNodesReady reports whether the #805 PR1 canonical-nodes-committed
// phase is published for this intent's scope generation. The phase key is
// derived the same way DomainAWSResourceMaterialization publishes it, so the
// lookup matches the published row. A nil lookup keeps the gate open for test
// wiring.
func (h IAMCanAssumeMaterializationHandler) canonicalNodesReady(intent Intent) bool {
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

// shouldSkipRetract mirrors the AWS relationship domain: skip the prior-edge
// retract on the very first generation for a scope (no prior edges to remove)
// and only on the first attempt, so a retried attempt still cleans up a partial
// prior write.
func (h IAMCanAssumeMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for iam can-assume retract: %w", err)
	}
	return !hasPrior, nil
}

// recordEdgeCounter emits the CAN_ASSUME edge-projection counter dimensioned by
// (principal_kind, resolution_mode), the contract registered for
// eshu_dp_iam_can_assume_edges_total. Each materialized edge row carries its
// assuming-principal kind (role/user) and resolution mode (arn); the skipped
// tally (external/service/wildcard/unscanned principals) goes to the completion
// log, not a metric label, so cardinality stays bounded.
func (h IAMCanAssumeMaterializationHandler) recordEdgeCounter(
	ctx context.Context,
	rows []map[string]any,
) {
	if h.Instruments == nil || h.Instruments.IAMCanAssumeEdges == nil {
		return
	}
	type kindMode struct {
		kind string
		mode string
	}
	counts := make(map[kindMode]int, len(rows))
	for _, row := range rows {
		counts[kindMode{anyToString(row["principal_kind"]), anyToString(row["resolution_mode"])}]++
	}
	for key, count := range counts {
		h.Instruments.IAMCanAssumeEdges.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrPrincipalKind(key.kind),
			telemetry.AttrResolutionMode(key.mode),
		))
	}
}

// splitIAMCanAssumeEnvelopes partitions a mixed envelope slice into aws_resource
// and aws_iam_permission facts in one pass so the join index and trust facts are
// built from a single bounded load.
func splitIAMCanAssumeEnvelopes(envelopes []facts.Envelope) (resources, permissions []facts.Envelope) {
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.AWSResourceFactKind:
			resources = append(resources, env)
		case facts.AWSIAMPermissionFactKind:
			permissions = append(permissions, env)
		}
	}
	return resources, permissions
}

// iamCanAssumeNodesNotReadyError marks the readiness-gate miss as retryable so
// the durable queue re-runs the intent once #805 PR1 nodes commit, instead of
// failing terminally or writing edges against absent nodes.
type iamCanAssumeNodesNotReadyError struct {
	scopeID      string
	generationID string
}

func (e iamCanAssumeNodesNotReadyError) Error() string {
	return fmt.Sprintf(
		"canonical cloud resource nodes not committed for scope %s generation %s",
		e.scopeID,
		e.generationID,
	)
}

func (iamCanAssumeNodesNotReadyError) Retryable() bool { return true }

func (iamCanAssumeNodesNotReadyError) FailureClass() string {
	return "iam_can_assume_nodes_not_ready"
}

// iamCanAssumeMaterializationTiming groups stage durations and the edge tally so
// the completion log identifies fact-load, resolve, retract, and graph-write
// time, plus which assuming-principal kinds materialized and which trust
// principals were skipped.
type iamCanAssumeMaterializationTiming struct {
	intent          Intent
	resourceCount   int
	permissionCount int
	edgeCount       int
	resolvedByKind  map[string]int
	skippedByReason map[string]int
	skipRetract     bool
	loadDuration    time.Duration
	extractDuration time.Duration
	retractDuration time.Duration
	writeDuration   time.Duration
	totalDuration   time.Duration
}

func logIAMCanAssumeMaterializationCompleted(
	ctx context.Context,
	timing iamCanAssumeMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "iam can-assume materialization completed",
		slog.String(telemetry.LogKeyScopeID, timing.intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, timing.intent.GenerationID),
		slog.String(telemetry.LogKeyDomain, string(timing.intent.Domain)),
		slog.Int("resource_fact_count", timing.resourceCount),
		slog.Int("permission_fact_count", timing.permissionCount),
		slog.Int("edge_count", timing.edgeCount),
		slog.String("resolved_by_kind", formatTally(timing.resolvedByKind)),
		slog.String("skipped_by_reason", formatTally(timing.skippedByReason)),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("resolve_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
