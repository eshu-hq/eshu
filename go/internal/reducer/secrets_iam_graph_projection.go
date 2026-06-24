package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

// SecretsIAMGraphWriter persists and retracts the reducer-owned secrets/IAM
// graph projection: the four SecretsIAM* node families and the five resolvable
// SECRETS_IAM_* edge families, plus scoped retract. Implementations MUST be
// idempotent (uid-only node identity, endpoint-pair edge identity) so reducer
// retries and duplicate facts converge, and MUST NOT fabricate endpoint nodes:
// a row whose endpoint node is absent is a no-op.
type SecretsIAMGraphWriter interface {
	WriteServiceAccountNodes(ctx context.Context, rows []map[string]any) error
	WriteVaultAuthRoleNodes(ctx context.Context, rows []map[string]any) error
	WriteVaultPolicyNodes(ctx context.Context, rows []map[string]any) error
	WriteSecretMetadataPathNodes(ctx context.Context, rows []map[string]any) error
	WriteUsesServiceAccountEdges(ctx context.Context, rows []map[string]any) error
	WriteAssumesIAMRoleEdges(ctx context.Context, rows []map[string]any) error
	WriteAuthenticatesVaultRoleEdges(ctx context.Context, rows []map[string]any) error
	WriteUsesVaultPolicyEdges(ctx context.Context, rows []map[string]any) error
	WriteGrantsSecretReadEdges(ctx context.Context, rows []map[string]any) error
	RetractScope(ctx context.Context, scopeIDs []string, evidenceSource string) error
}

// secretsIAMGraphProjectionDomainDefinition returns the additive definition for
// the secrets/IAM graph projection. It is additive (not part of
// DefaultDomainDefinitions) because the handler requires an explicitly wired
// SecretsIAMGraphWriter and FactLoader; registering it without them would
// silently drop every projection intent.
func secretsIAMGraphProjectionDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainSecretsIAMGraphProjection,
		Summary: "project exact reducer secrets/IAM trust-chain read-model rows into SecretsIAM* nodes and the five resolvable SECRETS_IAM_* edges",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "secrets_iam_graph_projection",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// SecretsIAMGraphProjectionHandler projects one secrets/IAM read-model scope
// generation into the graph. It loads the reducer-owned identity_trust_chain and
// secret_access_path facts, extracts the exact-only node/edge rows (ADR #1314
// §4), retracts the prior generation's reducer-owned SecretsIAM* graph state,
// writes nodes then edges (so edges MATCH already-committed nodes), and counts
// skipped rows instead of dropping them. A missing CloudResource or
// KubernetesWorkload endpoint is a writer-side no-op, never a fabricated node.
//
// When PresenceLookup is wired, the handler gates the projection on uid-exact
// cross-scope endpoint readiness (issue #1380): before retracting or writing, it
// confirms every cross-scope endpoint node the rows reference (KubernetesWorkload
// for USES_SERVICE_ACCOUNT, CloudResource for ASSUMES_IAM_ROLE) is committed. If
// any is missing it returns a retryable not-ready error so the intent re-enqueues
// instead of silently dropping edges to not-yet-committed endpoints.
type SecretsIAMGraphProjectionHandler struct {
	FactLoader FactLoader
	Writer     SecretsIAMGraphWriter
	// PriorGenerationCheck reports whether the scope has any prior generation.
	// Nil keeps retract conservative (always retract before write).
	PriorGenerationCheck PriorGenerationCheck
	// PresenceLookup answers uid-exact cross-scope endpoint readiness. Nil
	// disables gating (the projection writes whatever resolves, leaving any
	// not-yet-committed endpoint edge as a writer no-op). It is wired only when
	// the secrets/IAM graph projection feature is enabled.
	PresenceLookup EndpointPresenceLookup
	Tracer         trace.Tracer
	Instruments    *telemetry.Instruments
}

// Handle executes one secrets/IAM graph projection intent.
func (h SecretsIAMGraphProjectionHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainSecretsIAMGraphProjection {
		return Result{}, fmt.Errorf("secrets/iam graph projection handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("secrets/iam graph projection fact loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("secrets/iam graph projection writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerSecretsIAMGraphProjection,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, []string{
		secretsIAMIdentityTrustChainFactKind,
		secretsIAMSecretAccessPathFactKind,
	})
	if err != nil {
		return Result{}, fmt.Errorf("load facts for secrets/iam graph projection: %w", err)
	}
	loadDuration := time.Since(loadStart)

	extractStart := time.Now()
	rows := ExtractSecretsIAMGraphRows(envelopes)
	extractDuration := time.Since(extractStart)

	// Gate on uid-exact cross-scope endpoint readiness before touching graph
	// state. If any referenced KubernetesWorkload or CloudResource endpoint is not
	// yet committed, re-enqueue (retryable) rather than retracting and rewriting a
	// projection that would silently drop those edges. Returning before retract
	// leaves the prior generation's edges intact until the endpoints land.
	if err := h.checkEndpointReadiness(ctx, intent, rows); err != nil {
		return Result{}, err
	}

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.Writer.RetractScope(ctx, []string{intent.ScopeID}, SecretsIAMGraphEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("retract secrets/iam graph projection: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	writeStart := time.Now()
	if err := h.writeRows(ctx, rows); err != nil {
		return Result{}, err
	}
	writeDuration := time.Since(writeStart)

	h.recordTally(ctx, rows.Tally)
	nodeCount := totalRows(rows.ServiceAccountNodes, rows.VaultAuthRoleNodes, rows.VaultPolicyNodes, rows.SecretMetadataPathNodes)
	edgeCount := totalRows(rows.UsesServiceAccountEdges, rows.AssumesIAMRoleEdges, rows.AuthenticatesVaultRoleEdges, rows.UsesVaultPolicyEdges, rows.GrantsSecretReadEdges)
	h.logCompleted(ctx, intent, nodeCount, edgeCount, rows.Tally, skipRetract,
		loadDuration, extractDuration, retractDuration, writeDuration, time.Since(totalStart))

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainSecretsIAMGraphProjection,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"projected %d secrets/iam node(s) and %d edge(s) from exact read-model rows; %d skipped",
			nodeCount, edgeCount, totalTally(rows.Tally.SkippedByReason),
		),
		CanonicalWrites: nodeCount + edgeCount,
	}, nil
}

// writeRows writes nodes before edges so every edge MATCH resolves against
// already-committed SecretsIAM* nodes. Each write is a no-op for an empty set.
func (h SecretsIAMGraphProjectionHandler) writeRows(ctx context.Context, rows SecretsIAMGraphRows) error {
	nodeWrites := []struct {
		fn   func(context.Context, []map[string]any) error
		rows []map[string]any
	}{
		{h.Writer.WriteServiceAccountNodes, rows.ServiceAccountNodes},
		{h.Writer.WriteVaultAuthRoleNodes, rows.VaultAuthRoleNodes},
		{h.Writer.WriteVaultPolicyNodes, rows.VaultPolicyNodes},
		{h.Writer.WriteSecretMetadataPathNodes, rows.SecretMetadataPathNodes},
	}
	for _, w := range nodeWrites {
		if err := w.fn(ctx, w.rows); err != nil {
			return fmt.Errorf("write secrets/iam graph nodes: %w", err)
		}
	}
	edgeWrites := []struct {
		fn   func(context.Context, []map[string]any) error
		rows []map[string]any
	}{
		{h.Writer.WriteUsesServiceAccountEdges, rows.UsesServiceAccountEdges},
		{h.Writer.WriteAssumesIAMRoleEdges, rows.AssumesIAMRoleEdges},
		{h.Writer.WriteAuthenticatesVaultRoleEdges, rows.AuthenticatesVaultRoleEdges},
		{h.Writer.WriteUsesVaultPolicyEdges, rows.UsesVaultPolicyEdges},
		{h.Writer.WriteGrantsSecretReadEdges, rows.GrantsSecretReadEdges},
	}
	for _, w := range edgeWrites {
		if err := w.fn(ctx, w.rows); err != nil {
			return fmt.Errorf("write secrets/iam graph edges: %w", err)
		}
	}
	return nil
}

func (h SecretsIAMGraphProjectionHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for secrets/iam graph retract: %w", err)
	}
	return !hasPrior, nil
}

// recordTally emits the projection counters keyed by bounded enums (node label,
// edge type, skip reason). No path, ARN, namespace, or identifier becomes a
// label. It emits only the enums that occurred this generation (the extractor's
// skip-reason set is large and sparse); an operator charting a rate sees the
// series appear when the first occurrence fires, rather than a pre-seeded zero
// baseline for every possible reason.
func (h SecretsIAMGraphProjectionHandler) recordTally(ctx context.Context, tally SecretsIAMGraphTally) {
	if h.Instruments == nil {
		return
	}
	for label, count := range tally.NodesByLabel {
		h.Instruments.SecretsIAMGraphNodesWritten.Add(ctx, int64(count), metric.WithAttributes(telemetry.AttrNodeType(label)))
	}
	for edgeType, count := range tally.EdgesByType {
		h.Instruments.SecretsIAMGraphEdgesWritten.Add(ctx, int64(count), metric.WithAttributes(telemetry.AttrEdgeType(edgeType)))
	}
	for reason, count := range tally.SkippedByReason {
		h.Instruments.SecretsIAMGraphSkipped.Add(ctx, int64(count), metric.WithAttributes(telemetry.AttrSkipReason(reason)))
	}
}

func (h SecretsIAMGraphProjectionHandler) logCompleted(
	ctx context.Context, intent Intent, nodeCount, edgeCount int, tally SecretsIAMGraphTally, skipRetract bool,
	load, extract, retract, write, total time.Duration,
) {
	slog.InfoContext(
		ctx, "secrets/iam graph projection completed",
		slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
		slog.Int("node_count", nodeCount),
		slog.Int("edge_count", edgeCount),
		slog.Int("skipped_count", totalTally(tally.SkippedByReason)),
		slog.Bool("skip_retract", skipRetract),
		slog.Float64("load_seconds", load.Seconds()),
		slog.Float64("extract_seconds", extract.Seconds()),
		slog.Float64("retract_seconds", retract.Seconds()),
		slog.Float64("write_seconds", write.Seconds()),
		slog.Float64("total_seconds", total.Seconds()),
	)
}

func totalRows(sets ...[]map[string]any) int {
	total := 0
	for _, s := range sets {
		total += len(s)
	}
	return total
}

func totalTally(m map[string]int) int {
	total := 0
	for _, v := range m {
		total += v
	}
	return total
}
