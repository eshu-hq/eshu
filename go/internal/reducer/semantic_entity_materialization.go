package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// SemanticEntityRow holds one canonical semantic-entity materialization row.
type SemanticEntityRow struct {
	RepoID       string
	EntityID     string
	EntityType   string
	EntityName   string
	FilePath     string
	RelativePath string
	Language     string
	StartLine    int
	EndLine      int
	Metadata     map[string]any
}

// SemanticEntityWrite captures one canonical semantic-entity write request.
type SemanticEntityWrite struct {
	RepoIDs     []string
	Rows        []SemanticEntityRow
	SkipRetract bool
}

// SemanticEntityWriteResult captures the canonical semantic-entity write outcome.
type SemanticEntityWriteResult struct {
	CanonicalWrites int
}

// SemanticEntityWriter persists Annotation, Typedef, TypeAlias,
// TypeAnnotation, Component, Module, ImplBlock, Protocol,
// ProtocolImplementation, Variable, and callable Function semantic nodes into
// Neo4j.
type SemanticEntityWriter interface {
	WriteSemanticEntities(context.Context, SemanticEntityWrite) (SemanticEntityWriteResult, error)
}

// SemanticEntityMaterializationHandler reduces one semantic-entity follow-up
// into canonical graph writes. It loads parser facts, extracts canonical
// semantic rows, and writes them through the Neo4j adapter.
type SemanticEntityMaterializationHandler struct {
	FactLoader           FactLoader
	Writer               SemanticEntityWriter
	PriorGenerationCheck PriorGenerationCheck
	PhasePublisher       GraphProjectionPhasePublisher
	RepairQueue          GraphProjectionPhaseRepairQueue
}

// Handle executes the semantic-entity materialization path.
func (h SemanticEntityMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	if intent.Domain != DomainSemanticEntityMaterialization {
		return Result{}, fmt.Errorf(
			"semantic entity materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("semantic entity materialization fact loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("semantic entity materialization writer is required")
	}

	totalStart := time.Now()
	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{factKindRepository, factKindContentEntity},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for semantic entity materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	extractStart := time.Now()
	targetRepoID := semanticTargetRepoID(intent, envelopes)
	repoIDs, rows := ExtractSemanticEntityRowsForRepo(envelopes, targetRepoID)
	extractDuration := time.Since(extractStart)
	if len(repoIDs) == 0 && len(rows) == 0 {
		logSemanticEntityMaterializationCompleted(ctx, semanticEntityMaterializationTiming{
			intent:          intent,
			factCount:       len(envelopes),
			repoCount:       0,
			rowCount:        0,
			loadDuration:    loadDuration,
			extractDuration: extractDuration,
			totalDuration:   time.Since(totalStart),
		})
		return Result{
			IntentID:        intent.IntentID,
			Domain:          DomainSemanticEntityMaterialization,
			Status:          ResultStatusSucceeded,
			EvidenceSummary: "no semantic entities found",
		}, nil
	}

	retractDecisionStart := time.Now()
	skipRetract, err := h.shouldSkipSemanticRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	retractDecisionDuration := time.Since(retractDecisionStart)

	graphWriteStart := time.Now()
	writeResult, err := h.Writer.WriteSemanticEntities(ctx, SemanticEntityWrite{
		RepoIDs:     repoIDs,
		Rows:        rows,
		SkipRetract: skipRetract,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write semantic entities: %w", err)
	}
	graphWriteDuration := time.Since(graphWriteStart)
	phasePublishStart := time.Now()
	if err := h.publishSemanticGraphPhases(ctx, intent.GenerationID, envelopes, repoIDs, len(rows)); err != nil {
		return Result{}, fmt.Errorf("publish semantic graph phases: %w", err)
	}
	phasePublishDuration := time.Since(phasePublishStart)

	summary := fmt.Sprintf("materialized %d semantic entities across %d repositories", len(rows), len(repoIDs))
	if len(rows) == 0 {
		summary = fmt.Sprintf("retracted semantic entities across %d repositories", len(repoIDs))
	}

	logSemanticEntityMaterializationCompleted(ctx, semanticEntityMaterializationTiming{
		intent:                  intent,
		factCount:               len(envelopes),
		repoCount:               len(repoIDs),
		rowCount:                len(rows),
		skipRetract:             skipRetract,
		loadDuration:            loadDuration,
		extractDuration:         extractDuration,
		retractDecisionDuration: retractDecisionDuration,
		graphWriteDuration:      graphWriteDuration,
		phasePublishDuration:    phasePublishDuration,
		totalDuration:           time.Since(totalStart),
	})

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainSemanticEntityMaterialization,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: summary,
		CanonicalWrites: writeResult.CanonicalWrites,
	}, nil
}

// semanticEntityMaterializationTiming keeps stage durations together so the
// completion log cannot accidentally mix timings from different intents.
type semanticEntityMaterializationTiming struct {
	intent                  Intent
	factCount               int
	repoCount               int
	rowCount                int
	skipRetract             bool
	loadDuration            time.Duration
	extractDuration         time.Duration
	retractDecisionDuration time.Duration
	graphWriteDuration      time.Duration
	phasePublishDuration    time.Duration
	totalDuration           time.Duration
}

// logSemanticEntityMaterializationCompleted emits one low-cardinality summary
// that separates handler CPU work from graph backend and readiness publication.
func logSemanticEntityMaterializationCompleted(
	ctx context.Context,
	timing semanticEntityMaterializationTiming,
) {
	slog.InfoContext(ctx, "semantic entity materialization completed",
		slog.String(telemetry.LogKeyScopeID, timing.intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, timing.intent.GenerationID),
		slog.String(telemetry.LogKeyDomain, string(timing.intent.Domain)),
		slog.Int("fact_count", timing.factCount),
		slog.Int("repo_count", timing.repoCount),
		slog.Int("row_count", timing.rowCount),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_decision_duration_seconds", timing.retractDecisionDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.graphWriteDuration.Seconds()),
		slog.Float64("phase_publish_duration_seconds", timing.phasePublishDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}

func (h SemanticEntityMaterializationHandler) shouldSkipSemanticRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for semantic retract: %w", err)
	}
	return !hasPrior, nil
}

func (h SemanticEntityMaterializationHandler) publishSemanticGraphPhases(
	ctx context.Context,
	generationID string,
	envelopes []facts.Envelope,
	repoIDs []string,
	rowCount int,
) error {
	if h.PhasePublisher == nil || rowCount == 0 || len(repoIDs) == 0 {
		return nil
	}

	states := semanticGraphPhaseStates(generationID, envelopes, repoIDs)
	if len(states) == 0 {
		return nil
	}
	if err := h.PhasePublisher.PublishGraphProjectionPhases(ctx, states); err != nil {
		if h.RepairQueue != nil {
			repairs := GraphProjectionPhaseRepairsFromStates(states, err.Error(), time.Now().UTC())
			if enqueueErr := h.RepairQueue.Enqueue(ctx, repairs); enqueueErr != nil {
				return fmt.Errorf("publish semantic graph phases: %w (enqueue repairs: %v)", err, enqueueErr)
			}
		}
		return err
	}
	return nil
}

func semanticTargetRepoID(intent Intent, envelopes []facts.Envelope) string {
	if len(intent.EntityKeys) == 0 {
		return ""
	}

	key := strings.TrimSpace(intent.EntityKeys[0])
	if key == "" {
		return ""
	}
	if repoID, ok := semanticAcceptanceUnitRepoID(key); ok {
		return repoID
	}
	for _, env := range envelopes {
		if env.FactKind != "content_entity" {
			continue
		}
		if semanticPayloadString(env.Payload, "entity_id") != key {
			continue
		}
		if repoID := semanticPayloadString(env.Payload, "repo_id"); repoID != "" {
			return repoID
		}
	}
	return ""
}

func semanticAcceptanceUnitRepoID(key string) (string, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	if strings.HasPrefix(key, "repo:") {
		repoID := strings.TrimSpace(strings.TrimPrefix(key, "repo:"))
		if repoID != "" {
			return repoID, true
		}
	}
	return "", false
}

func semanticGraphPhaseStates(
	generationID string,
	envelopes []facts.Envelope,
	repoIDs []string,
) []GraphProjectionPhaseState {
	repoSet := make(map[string]struct{}, len(repoIDs))
	for _, repoID := range repoIDs {
		if trimmed := strings.TrimSpace(repoID); trimmed != "" {
			repoSet[trimmed] = struct{}{}
		}
	}

	seen := make(map[string]struct{}, len(repoSet))
	states := make([]GraphProjectionPhaseState, 0, len(repoSet))
	for _, env := range envelopes {
		if env.FactKind != "repository" {
			continue
		}
		repoID := semanticPayloadString(env.Payload, "repo_id")
		if _, ok := repoSet[repoID]; !ok {
			continue
		}
		sourceRunID := semanticPayloadString(env.Payload, "source_run_id")
		if strings.TrimSpace(env.ScopeID) == "" || sourceRunID == "" || strings.TrimSpace(generationID) == "" {
			continue
		}
		composite := strings.Join([]string{env.ScopeID, repoID, sourceRunID, generationID}, "|")
		if _, ok := seen[composite]; ok {
			continue
		}
		seen[composite] = struct{}{}
		publishedAt := time.Now().UTC()
		states = append(states, GraphProjectionPhaseState{
			Key: GraphProjectionPhaseKey{
				ScopeID:          env.ScopeID,
				AcceptanceUnitID: repoID,
				SourceRunID:      sourceRunID,
				GenerationID:     generationID,
				Keyspace:         GraphProjectionKeyspaceCodeEntitiesUID,
			},
			Phase:       GraphProjectionPhaseSemanticNodesCommitted,
			CommittedAt: publishedAt,
			UpdatedAt:   publishedAt,
		})
	}

	return states
}

// ExtractSemanticEntityRows returns the touched repository IDs and canonical
// semantic rows extracted from fact envelopes.
func ExtractSemanticEntityRows(envelopes []facts.Envelope) ([]string, []SemanticEntityRow) {
	return ExtractSemanticEntityRowsForRepo(envelopes, "")
}

// ExtractSemanticEntityRowsForRepo returns the touched repository IDs and
// canonical semantic rows extracted from fact envelopes, optionally filtered
// to one repo acceptance unit.
func ExtractSemanticEntityRowsForRepo(envelopes []facts.Envelope, targetRepoID string) ([]string, []SemanticEntityRow) {
	if len(envelopes) == 0 {
		return nil, nil
	}

	targetRepoID = strings.TrimSpace(targetRepoID)
	repoIDs := collectSemanticRepoIDs(envelopes)
	if targetRepoID != "" {
		repoIDs = filterSemanticRepoIDs(repoIDs, targetRepoID)
	}
	rows := make([]SemanticEntityRow, 0)
	for _, env := range envelopes {
		if env.FactKind != "content_entity" {
			continue
		}

		repoID := semanticPayloadString(env.Payload, "repo_id")
		entityType := semanticPayloadString(env.Payload, "entity_type")
		if targetRepoID != "" && repoID != targetRepoID {
			continue
		}
		if repoID == "" || !isSemanticEntityType(env.Payload, entityType) {
			continue
		}

		entityID := semanticPayloadString(env.Payload, "entity_id")
		entityName := semanticPayloadString(env.Payload, "entity_name")
		filePath := strings.TrimSpace(env.SourceRef.SourceURI)
		if entityID == "" || entityName == "" || filePath == "" {
			continue
		}

		row := SemanticEntityRow{
			RepoID:       repoID,
			EntityID:     entityID,
			EntityType:   entityType,
			EntityName:   entityName,
			FilePath:     filePath,
			RelativePath: semanticPayloadString(env.Payload, "relative_path"),
			Language:     semanticPayloadString(env.Payload, "language"),
			StartLine:    semanticPayloadInt(env.Payload, "start_line"),
			EndLine:      semanticPayloadInt(env.Payload, "end_line"),
			Metadata:     collectSemanticMetadata(env.Payload),
		}
		rows = append(rows, row)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		left := rows[i]
		right := rows[j]
		if left.RepoID != right.RepoID {
			return left.RepoID < right.RepoID
		}
		if left.FilePath != right.FilePath {
			return left.FilePath < right.FilePath
		}
		if left.EntityType != right.EntityType {
			return left.EntityType < right.EntityType
		}
		if left.StartLine != right.StartLine {
			return left.StartLine < right.StartLine
		}
		return left.EntityID < right.EntityID
	})

	return repoIDs, rows
}

func filterSemanticRepoIDs(repoIDs []string, targetRepoID string) []string {
	if targetRepoID == "" {
		return repoIDs
	}
	for _, repoID := range repoIDs {
		if repoID == targetRepoID {
			return []string{repoID}
		}
	}
	return nil
}
