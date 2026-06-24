package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// activePackageOwnershipFactLoader exposes the cross-scope package-registry
// facts needed to resolve an import coordinate to its owning repository:
// package identity facts plus exact/derived ownership and publication
// correlation facts. The loader is bounded by the active package-registry
// generation, never the file corpus, so the code-import projection inherits the
// same cost profile as the #3598 package-consumption join.
type activePackageOwnershipFactLoader interface {
	ListActivePackageOwnershipFacts(ctx context.Context) ([]facts.Envelope, error)
}

// codeImportOwnershipLoader reports whether the wired FactLoader can serve the
// bounded cross-scope package-ownership facts the code-import projection needs.
// The default reducer binary wires the Postgres FactStore, which satisfies the
// interface; test doubles and fact-only profiles that do not implement it leave
// the code-import domain unregistered so no intent is silently dropped.
func codeImportOwnershipLoader(loader FactLoader) (activePackageOwnershipFactLoader, bool) {
	if loader == nil {
		return nil, false
	}
	owner, ok := loader.(activePackageOwnershipFactLoader)
	return owner, ok
}

// CodeImportRepoEdgeHandler projects repo-to-repo DEPENDS_ON edges from per-file
// external import sources correlated to package-registry ownership (issue
// #3642). It runs in the git-repository scope so the per-file `file` facts that
// carry import identity are scope-local; owner identity is resolved from
// cross-scope package-registry facts through the sanctioned (ecosystem, name)
// join used by #3598.
//
// The handler emits durable repo-dependency projection intents through the
// shared RepoDependencyIntentWriter lane; the existing repo-dependency
// projection runner drains them into canonical DEPENDS_ON edges with an
// idempotent MERGE keyed on (source_repo, target_repo). When the intent writer
// or the ownership loader is nil the handler is a no-op, so a deployment without
// package-registry ingestion stays unaffected.
type CodeImportRepoEdgeHandler struct {
	FactLoader                 FactLoader
	OwnershipLoader            activePackageOwnershipFactLoader
	RepoDependencyIntentWriter RepoDependencyIntentWriter
	Instruments                *telemetry.Instruments
	Tracer                     trace.Tracer
	// Now overrides the wall clock for deterministic intent created_at in tests.
	Now func() time.Time
}

// Handle executes code-import repo-edge projection for one git-repository scope.
func (h CodeImportRepoEdgeHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainCodeImportRepoEdge {
		return Result{}, fmt.Errorf(
			"code_import_repo_edge handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("code import repo edge fact loader is required")
	}

	if h.tracerStartSkippable() {
		var span trace.Span
		ctx, span = h.Tracer.Start(ctx, telemetry.SpanCodeImportRepoEdge)
		defer span.End()
	}

	if h.RepoDependencyIntentWriter == nil || h.OwnershipLoader == nil {
		return h.skippedResult(intent, "code import repo edge writer or ownership loader not wired"), nil
	}

	fileEnvelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{factKindFile},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load file facts for code import repo edge: %w", err)
	}

	ownershipEnvelopes, err := h.OwnershipLoader.ListActivePackageOwnershipFacts(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load active package ownership facts: %w", err)
	}
	owners := buildCodeImportOwnerIndex(
		ownershipEnvelopes,
		decodePackageOwnershipCorrelationDecisions(ownershipEnvelopes),
		decodePackagePublicationCorrelationDecisions(ownershipEnvelopes),
	)

	edgeInput := CodeImportRepoDependencyInput{
		ScopeID:       intent.ScopeID,
		GenerationID:  intent.GenerationID,
		SourceRunID:   codeImportRepoEdgeSourceRunID(intent.ScopeID),
		CreatedAt:     h.now(),
		FileEnvelopes: fileEnvelopes,
		Owners:        owners,
	}

	counts := classifyCodeImportEdges(edgeInput)
	upsertIntents := BuildCodeImportRepoDependencyIntents(edgeInput)
	// Refresh-first: consumers that appear in this full snapshot but resolve no
	// owner this generation must still reprocess so the shared lane retracts any
	// projection/code-imports edge they held in a prior generation. Without these
	// the stale DEPENDS_ON edge would persist because the upsert build emits
	// nothing for them (mirrors the #3598 package-consumption refresh pattern).
	refreshIntents := BuildCodeImportRepoEdgeRefreshIntents(edgeInput)

	h.emitCounters(ctx, counts, len(upsertIntents))
	if len(refreshIntents) > 0 {
		h.emitRefreshCounter(ctx, len(refreshIntents))
	}
	h.logCompleted(ctx, intent, counts, len(upsertIntents))

	intents := make([]SharedProjectionIntentRow, 0, len(upsertIntents)+len(refreshIntents))
	intents = append(intents, upsertIntents...)
	intents = append(intents, refreshIntents...)
	if len(intents) == 0 {
		return h.succeededResult(intent, counts, 0), nil
	}
	if err := h.RepoDependencyIntentWriter.UpsertIntents(ctx, intents); err != nil {
		return Result{}, fmt.Errorf("upsert code import repo dependency intents: %w", err)
	}
	return h.succeededResult(intent, counts, len(upsertIntents)), nil
}

func (h CodeImportRepoEdgeHandler) tracerStartSkippable() bool {
	return h.Tracer != nil
}

func (h CodeImportRepoEdgeHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h CodeImportRepoEdgeHandler) skippedResult(intent Intent, reason string) Result {
	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainCodeImportRepoEdge,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: reason,
	}
}

func (h CodeImportRepoEdgeHandler) succeededResult(intent Intent, counts codeImportEdgeCounts, written int) Result {
	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainCodeImportRepoEdge,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"code import edges considered=%d written=%d skipped_relative=%d skipped_unresolved=%d skipped_ambiguous=%d skipped_no_owner=%d skipped_self=%d",
			counts.considered,
			written,
			counts.skippedRelative,
			counts.skippedUnresolved,
			counts.skippedAmbiguous,
			counts.skippedNoOwner,
			counts.skippedSelf,
		),
	}
}

func (h CodeImportRepoEdgeHandler) emitCounters(ctx context.Context, counts codeImportEdgeCounts, written int) {
	if h.Instruments == nil {
		return
	}
	add := func(outcome string, value int) {
		if value <= 0 {
			return
		}
		h.Instruments.CodeImportRepoEdges.Add(
			ctx,
			int64(value),
			metric.WithAttributes(
				telemetry.AttrDomain(string(DomainRepoDependency)),
				telemetry.AttrOutcome(outcome),
			),
		)
	}
	add("considered", counts.considered)
	add("written", written)
	add("skipped_relative", counts.skippedRelative)
	add("skipped_unresolved", counts.skippedUnresolved)
	add("skipped_ambiguous", counts.skippedAmbiguous)
	add("skipped_no_owner", counts.skippedNoOwner)
	add("skipped_self", counts.skippedSelf)
}

// emitRefreshCounter records the number of retract-only refresh intents
// emitted for consumers that produced no upsert edge this generation.
func (h CodeImportRepoEdgeHandler) emitRefreshCounter(ctx context.Context, count int) {
	if h.Instruments == nil || count <= 0 {
		return
	}
	h.Instruments.CodeImportRepoEdges.Add(
		ctx,
		int64(count),
		metric.WithAttributes(
			telemetry.AttrDomain(string(DomainRepoDependency)),
			telemetry.AttrOutcome("refreshed_no_owner"),
		),
	)
}

func (h CodeImportRepoEdgeHandler) logCompleted(
	ctx context.Context,
	intent Intent,
	counts codeImportEdgeCounts,
	written int,
) {
	slog.InfoContext(
		ctx, "code import repo edge projection completed",
		slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
		slog.String(telemetry.LogKeyDomain, string(DomainCodeImportRepoEdge)),
		slog.Int("considered", counts.considered),
		slog.Int("written", written),
		slog.Int("skipped_relative", counts.skippedRelative),
		slog.Int("skipped_unresolved", counts.skippedUnresolved),
		slog.Int("skipped_ambiguous", counts.skippedAmbiguous),
		slog.Int("skipped_no_owner", counts.skippedNoOwner),
		slog.Int("skipped_self", counts.skippedSelf),
	)
}

// codeImportRepoEdgeSourceRunID returns a deterministic acceptance source-run id
// for code-import repo-dependency intents. It is a stable function of the git
// scope ONLY, deliberately excluding the generation, so re-projecting the same
// scope in a new generation reuses the same shared-projection acceptance key and
// the repo-dependency lane treats the new edge as a refresh of the prior edge.
// It mirrors packageConsumptionRepoEdgeSourceRunID for the same reason.
func codeImportRepoEdgeSourceRunID(scopeID string) string {
	scopeID = strings.TrimSpace(scopeID)
	if scopeID == "" {
		return "code_import_repo_dependency"
	}
	return "code_import_repo_dependency:" + scopeID
}
