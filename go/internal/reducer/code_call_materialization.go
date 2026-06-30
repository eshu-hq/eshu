// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const (
	codeCallEvidenceSource            = "parser/code-calls"
	pythonMetaclassEvidenceSource     = "parser/python-metaclass"
	codeCallRepoRefreshEvidenceSource = "reducer/code-call-refresh"
)

// CanonicalNodeChecker checks whether canonical code entity nodes (Function,
// Class, File) exist in the graph. The code-call handler no longer uses this
// preflight check, but the type remains for compatibility with older wiring.
type CanonicalNodeChecker interface {
	HasCanonicalCodeTargets(ctx context.Context) (bool, error)
}

// CodeCallIntentWriter persists durable shared-intent rows for code-call
// materialization.
type CodeCallIntentWriter interface {
	UpsertIntents(ctx context.Context, rows []SharedProjectionIntentRow) error
}

type codeCallSymbolDefinitionFactLoader interface {
	LoadActiveCodeCallSymbolDefinitionFacts(
		ctx context.Context,
		symbolKeys []string,
	) ([]facts.Envelope, error)
}

// CodeCallMaterializationHandler reduces one parser relationship follow-up into
// durable shared-intent emission for code-call and Python metaclass rows.
type CodeCallMaterializationHandler struct {
	FactLoader   FactLoader
	IntentWriter CodeCallIntentWriter

	// EdgeWriter is retained for compatibility with older wiring and tests.
	// The handler no longer writes canonical edges directly.
	EdgeWriter SharedProjectionEdgeWriter
}

// Handle executes the code-call materialization path.
func (h CodeCallMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	if intent.Domain != DomainCodeCallMaterialization {
		return Result{}, fmt.Errorf(
			"code call materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("code call materialization fact loader is required")
	}
	if h.IntentWriter == nil {
		return Result{}, fmt.Errorf("code call materialization intent writer is required")
	}

	totalStart := time.Now()
	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{factKindRepository, factKindFile},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for code call materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	contextStart := time.Now()
	contextByRepoID := buildCodeCallProjectionContexts(envelopes, intent.GenerationID)
	contextDuration := time.Since(contextStart)
	if len(contextByRepoID) == 0 {
		totalDuration := time.Since(totalStart)
		logCodeCallMaterializationCompleted(ctx, codeCallMaterializationTiming{
			intent:          intent,
			factCount:       len(envelopes),
			loadDuration:    loadDuration,
			contextDuration: contextDuration,
			totalDuration:   totalDuration,
		})
		// No projection context built from the loaded facts: the handler ran
		// before its upstream repository/file facts existed — an ordering stall,
		// signaled by input_ready=0.
		return Result{
			IntentID:        intent.IntentID,
			Domain:          DomainCodeCallMaterialization,
			Status:          ResultStatusSucceeded,
			EvidenceSummary: "no repositories available for code call materialization",
			SubDurations: codeCallMaterializationSubDurations(codeCallMaterializationTiming{
				loadDuration:    loadDuration,
				contextDuration: contextDuration,
				totalDuration:   totalDuration,
			}),
			SubSignals: materializationDiagnosticSignals(false, 0),
		}, nil
	}

	symbolLoadStart := time.Now()
	symbolKeys := codeCallReferencedSymbolKeys(envelopes)
	symbolDefinitionEnvelopes, err := loadActiveCodeCallSymbolDefinitionFacts(ctx, h.FactLoader, symbolKeys)
	if err != nil {
		return Result{}, fmt.Errorf("load active code call symbol definition facts: %w", err)
	}
	relationshipEnvelopes := envelopes
	if len(symbolDefinitionEnvelopes) > 0 {
		relationshipEnvelopes = make([]facts.Envelope, 0, len(envelopes)+len(symbolDefinitionEnvelopes))
		relationshipEnvelopes = append(relationshipEnvelopes, envelopes...)
		relationshipEnvelopes = append(relationshipEnvelopes, symbolDefinitionEnvelopes...)
	}
	symbolLoadDuration := time.Since(symbolLoadStart)

	extractStart := time.Now()
	_, codeCallRows, _, metaclassRows, entityIndex := extractAllCodeRelationshipRowsWithIndex(relationshipEnvelopes)
	extractDuration := time.Since(extractStart)
	createdAt := intent.EnqueuedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	intentBuildStart := time.Now()
	fileScopeResult := buildCodeCallFileScopesByRepoID(envelopes)
	fileScopesByRepoID := fileScopeResult.scopesByRepoID
	intentRows := buildCodeCallRefreshIntentsWithDeltaFileScopes(contextByRepoID, fileScopesByRepoID, createdAt)
	intentRows = append(
		intentRows,
		buildCodeCallSharedIntentRows(
			codeCallRows,
			contextByRepoID,
			createdAt,
			codeCallEvidenceSource,
			fileScopesByRepoID,
		)...,
	)
	intentRows = append(
		intentRows,
		buildCodeCallSharedIntentRows(
			metaclassRows,
			contextByRepoID,
			createdAt,
			pythonMetaclassEvidenceSource,
			fileScopesByRepoID,
		)...,
	)
	// Symbol→runtime domains (handles_route, runs_in, invokes_cloud_action) do a
	// repo-wide retract but emit per-edge partition keys; each per-edge batch is
	// paired in the same pass with a whole-scope refresh intent that owns the
	// single repo-wide retract, so the worker can fence per-edge writes behind it
	// and stop partitions wiping each other's edges (#2898/#2910).
	intentRows = append(
		intentRows,
		buildSymbolRuntimeIntentRows(envelopes, entityIndex, contextByRepoID, createdAt)...,
	)
	intentBuildDuration := time.Since(intentBuildStart)

	if len(intentRows) == 0 {
		totalDuration := time.Since(totalStart)
		logCodeCallMaterializationCompleted(ctx, codeCallMaterializationTiming{
			intent:              intent,
			factCount:           len(envelopes),
			symbolKeyCount:      len(symbolKeys),
			symbolFactCount:     len(symbolDefinitionEnvelopes),
			repoCount:           len(contextByRepoID),
			codeCallRowCount:    len(codeCallRows),
			metaclassRowCount:   len(metaclassRows),
			intentRowCount:      0,
			fileScopedRepoCount: len(fileScopesByRepoID),
			fullRefreshScoped:   fileScopeResult.fullRefreshScopedRepos,
			fullRefreshFallback: fileScopeResult.fullRefreshFallbackRepos,
			loadDuration:        loadDuration,
			contextDuration:     contextDuration,
			symbolLoadDuration:  symbolLoadDuration,
			extractDuration:     extractDuration,
			intentBuildDuration: intentBuildDuration,
			totalDuration:       totalDuration,
		})
		// Projection context was built (input present) but extraction produced no
		// edges: genuine empty work, signaled by input_ready=1 and written_rows=0.
		return Result{
			IntentID:        intent.IntentID,
			Domain:          DomainCodeCallMaterialization,
			Status:          ResultStatusSucceeded,
			EvidenceSummary: "no code-call or metaclass intents available for materialization",
			SubDurations: codeCallMaterializationSubDurations(codeCallMaterializationTiming{
				loadDuration:        loadDuration,
				contextDuration:     contextDuration,
				symbolLoadDuration:  symbolLoadDuration,
				extractDuration:     extractDuration,
				intentBuildDuration: intentBuildDuration,
				totalDuration:       totalDuration,
			}),
			SubSignals: materializationDiagnosticSignals(true, 0),
		}, nil
	}

	upsertStart := time.Now()
	if err := h.IntentWriter.UpsertIntents(ctx, intentRows); err != nil {
		return Result{}, fmt.Errorf("write code call intents: %w", err)
	}
	upsertDuration := time.Since(upsertStart)
	totalDuration := time.Since(totalStart)

	successTiming := codeCallMaterializationTiming{
		loadDuration:        loadDuration,
		contextDuration:     contextDuration,
		symbolLoadDuration:  symbolLoadDuration,
		extractDuration:     extractDuration,
		intentBuildDuration: intentBuildDuration,
		upsertDuration:      upsertDuration,
		totalDuration:       totalDuration,
	}

	logCodeCallMaterializationCompleted(ctx, codeCallMaterializationTiming{
		intent:              intent,
		factCount:           len(envelopes),
		symbolKeyCount:      len(symbolKeys),
		symbolFactCount:     len(symbolDefinitionEnvelopes),
		repoCount:           len(contextByRepoID),
		codeCallRowCount:    len(codeCallRows),
		metaclassRowCount:   len(metaclassRows),
		intentRowCount:      len(intentRows),
		fileScopedRepoCount: len(fileScopesByRepoID),
		fullRefreshScoped:   fileScopeResult.fullRefreshScopedRepos,
		fullRefreshFallback: fileScopeResult.fullRefreshFallbackRepos,
		loadDuration:        loadDuration,
		contextDuration:     contextDuration,
		symbolLoadDuration:  symbolLoadDuration,
		extractDuration:     extractDuration,
		intentBuildDuration: intentBuildDuration,
		upsertDuration:      upsertDuration,
		totalDuration:       totalDuration,
	})

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainCodeCallMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"emitted %d durable code call intents across %d repositories",
			len(intentRows),
			len(contextByRepoID),
		),
		CanonicalWrites: len(intentRows),
		SubDurations:    codeCallMaterializationSubDurations(successTiming),
		// Projection context was built (input present) and intents were emitted.
		SubSignals: materializationDiagnosticSignals(true, len(intentRows)),
	}, nil
}

// codeCallMaterializationSubDurations converts per-phase timing into the
// Result.SubDurations map so the service layer emits sub_duration_<key>_seconds
// log attributes. Keys follow the workload_materialization naming convention
// for cross-domain log correlation. The non-duration diagnostic signals
// (input_ready, written_rows) are carried separately in Result.SubSignals so
// the _seconds suffix stays honest.
func codeCallMaterializationSubDurations(t codeCallMaterializationTiming) map[string]float64 {
	return map[string]float64{
		"load_facts":     t.loadDuration.Seconds(),
		"build_context":  t.contextDuration.Seconds(),
		"load_symbols":   t.symbolLoadDuration.Seconds(),
		"extract_rows":   t.extractDuration.Seconds(),
		"build_intents":  t.intentBuildDuration.Seconds(),
		"upsert_intents": t.upsertDuration.Seconds(),
		"total":          t.totalDuration.Seconds(),
	}
}

func loadActiveCodeCallSymbolDefinitionFacts(
	ctx context.Context,
	loader FactLoader,
	symbolKeys []string,
) ([]facts.Envelope, error) {
	symbolKeys = cleanFactFilterValues(symbolKeys)
	if len(symbolKeys) == 0 {
		return nil, nil
	}
	typed, ok := loader.(codeCallSymbolDefinitionFactLoader)
	if !ok {
		return nil, nil
	}
	envelopes, err := typed.LoadActiveCodeCallSymbolDefinitionFacts(ctx, symbolKeys)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return envelopes, nil
}

type codeCallMaterializationTiming struct {
	intent              Intent
	factCount           int
	symbolKeyCount      int
	symbolFactCount     int
	repoCount           int
	codeCallRowCount    int
	metaclassRowCount   int
	intentRowCount      int
	fileScopedRepoCount int
	fullRefreshScoped   int
	fullRefreshFallback int
	loadDuration        time.Duration
	contextDuration     time.Duration
	symbolLoadDuration  time.Duration
	extractDuration     time.Duration
	intentBuildDuration time.Duration
	upsertDuration      time.Duration
	totalDuration       time.Duration
}

func logCodeCallMaterializationCompleted(ctx context.Context, timing codeCallMaterializationTiming) {
	slog.InfoContext(
		ctx, "code call materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("fact_count", timing.factCount),
		slog.Int("symbol_key_count", timing.symbolKeyCount),
		slog.Int("symbol_definition_fact_count", timing.symbolFactCount),
		slog.Int("repo_count", timing.repoCount),
		slog.Int("code_call_row_count", timing.codeCallRowCount),
		slog.Int("metaclass_row_count", timing.metaclassRowCount),
		slog.Int("intent_row_count", timing.intentRowCount),
		slog.Int("file_scoped_repo_count", timing.fileScopedRepoCount),
		slog.Int("full_refresh_file_scoped_repo_count", timing.fullRefreshScoped),
		slog.Int("full_refresh_file_scope_fallback_repo_count", timing.fullRefreshFallback),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("context_duration_seconds", timing.contextDuration.Seconds()),
		slog.Float64("load_symbol_definitions_duration_seconds", timing.symbolLoadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("build_intents_duration_seconds", timing.intentBuildDuration.Seconds()),
		slog.Float64("upsert_intents_duration_seconds", timing.upsertDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
