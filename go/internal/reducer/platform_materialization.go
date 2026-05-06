package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// PlatformMaterializationWrite captures the bounded canonical reconciliation
// request for one platform materialization reducer intent.
type PlatformMaterializationWrite struct {
	IntentID        string
	ScopeID         string
	GenerationID    string
	SourceSystem    string
	Cause           string
	EntityKeys      []string
	RelatedScopeIDs []string
}

// PlatformMaterializationWriteResult captures the canonical platform
// materialization write outcome returned by the backend adapter.
type PlatformMaterializationWriteResult struct {
	CanonicalID     string
	CanonicalWrites int
	EvidenceSummary string
}

// PlatformMaterializationWriter persists one platform materialization request
// into a canonical reducer-owned target (Neo4j PROVISIONS_PLATFORM and
// RUNS_ON edges).
type PlatformMaterializationWriter interface {
	WritePlatformMaterialization(context.Context, PlatformMaterializationWrite) (PlatformMaterializationWriteResult, error)
}

// WorkloadMaterializationReplayer requeues workload materialization after
// stronger deployment evidence becomes available for the same scope generation.
type WorkloadMaterializationReplayer interface {
	ReplayWorkloadMaterialization(ctx context.Context, scopeID, generationID, entityKey string) (bool, error)
}

// PlatformMaterializationHandler reduces one platform materialization intent
// into a bounded canonical write request. When FactLoader and
// InfrastructureMaterializer are set, the handler also writes
// PROVISIONS_PLATFORM edges to the canonical graph. When CrossRepoResolver
// is set, the handler also resolves cross-repo dependency edges from
// persisted evidence facts after platform materialization completes.
type PlatformMaterializationHandler struct {
	Writer                          PlatformMaterializationWriter
	FactLoader                      FactLoader
	InfrastructureMaterializer      *InfrastructurePlatformMaterializer
	CrossRepoResolver               *CrossRepoRelationshipHandler
	WorkloadMaterializationReplayer WorkloadMaterializationReplayer
	PhasePublisher                  GraphProjectionPhasePublisher
}

// platformMaterializationTiming records success-path stage timings for the
// deployment_mapping reducer domain without affecting reducer ordering.
type platformMaterializationTiming struct {
	platformWriteDuration       time.Duration
	factLoadDuration            time.Duration
	infrastructureExtract       time.Duration
	infrastructureGraphWrite    time.Duration
	crossRepoResolutionDuration time.Duration
	workloadReplayDuration      time.Duration
	phasePublishDuration        time.Duration
	totalDuration               time.Duration
}

// Handle executes the platform materialization reduction path.
func (h PlatformMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStarted := time.Now()
	var timing platformMaterializationTiming

	if intent.Domain != DomainDeploymentMapping {
		return Result{}, fmt.Errorf(
			"platform materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("platform materialization writer is required")
	}

	request, err := platformMaterializationWriteFromIntent(intent)
	if err != nil {
		return Result{}, err
	}

	platformWriteStarted := time.Now()
	writeResult, err := h.Writer.WritePlatformMaterialization(ctx, request)
	timing.platformWriteDuration = time.Since(platformWriteStarted)
	if err != nil {
		return Result{}, err
	}

	canonicalWrites := writeResult.CanonicalWrites
	infraRows := 0
	infraWrites := 0

	// When both FactLoader and InfrastructureMaterializer are provided,
	// also write PROVISIONS_PLATFORM edges to the canonical graph.
	if h.FactLoader != nil && h.InfrastructureMaterializer != nil {
		factLoadStarted := time.Now()
		facts, err := loadFactsForKinds(
			ctx,
			h.FactLoader,
			intent.ScopeID,
			intent.GenerationID,
			[]string{factKindRepository, factKindFile, factKindParsedFile},
		)
		timing.factLoadDuration = time.Since(factLoadStarted)
		if err != nil {
			return Result{}, fmt.Errorf("load facts for infrastructure platform materialization: %w", err)
		}
		extractStarted := time.Now()
		rows := ExtractInfrastructurePlatformRows(facts)
		timing.infrastructureExtract = time.Since(extractStarted)
		infraRows = len(rows)
		if len(rows) > 0 {
			infraStarted := time.Now()
			infraResult, err := h.InfrastructureMaterializer.Materialize(ctx, rows)
			timing.infrastructureGraphWrite = time.Since(infraStarted)
			if err != nil {
				return Result{}, fmt.Errorf("materialize infrastructure platforms: %w", err)
			}
			infraWrites = infraResult.PlatformEdgesWritten
			canonicalWrites += infraResult.PlatformEdgesWritten
		}
	}

	crossRepoWrites := 0
	workloadReplayCount := 0
	// When CrossRepoResolver is provided, resolve cross-repo dependency edges
	// from persisted evidence facts after platform materialization completes.
	if h.CrossRepoResolver != nil {
		crossRepoStarted := time.Now()
		resolvedCrossRepoWrites, err := h.CrossRepoResolver.Resolve(ctx, intent.ScopeID, intent.GenerationID)
		timing.crossRepoResolutionDuration = time.Since(crossRepoStarted)
		if err != nil {
			return Result{}, fmt.Errorf("cross-repo relationship resolution: %w", err)
		}
		crossRepoWrites = resolvedCrossRepoWrites
		canonicalWrites += crossRepoWrites
		if crossRepoWrites > 0 && h.WorkloadMaterializationReplayer != nil {
			replayStarted := time.Now()
			replayEntityKey := workloadMaterializationReplayEntityKey(intent)
			for _, scopeID := range workloadMaterializationReplayScopes(intent) {
				if _, err := h.WorkloadMaterializationReplayer.ReplayWorkloadMaterialization(
					ctx,
					scopeID,
					intent.GenerationID,
					replayEntityKey,
				); err != nil {
					return Result{}, fmt.Errorf("replay workload materialization: %w", err)
				}
				workloadReplayCount++
			}
			timing.workloadReplayDuration = time.Since(replayStarted)
		}
	}

	evidenceSummary := strings.TrimSpace(writeResult.EvidenceSummary)
	if evidenceSummary == "" {
		evidenceSummary = fmt.Sprintf(
			"materialized %d platform key(s) across %d scope(s)",
			len(request.EntityKeys),
			len(request.RelatedScopeIDs),
		)
	}
	phaseStarted := time.Now()
	if err := publishIntentGraphPhase(
		ctx,
		h.PhasePublisher,
		intent,
		GraphProjectionKeyspaceServiceUID,
		GraphProjectionPhaseDeploymentMapping,
		time.Now().UTC(),
	); err != nil {
		return Result{}, err
	}
	timing.phasePublishDuration = time.Since(phaseStarted)
	timing.totalDuration = time.Since(totalStarted)
	logPlatformMaterializationCompleted(
		ctx,
		intent,
		request,
		canonicalWrites,
		infraRows,
		infraWrites,
		crossRepoWrites,
		workloadReplayCount,
		timing,
	)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainDeploymentMapping,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: evidenceSummary,
		CanonicalWrites: canonicalWrites,
	}, nil
}

func logPlatformMaterializationCompleted(
	ctx context.Context,
	intent Intent,
	request PlatformMaterializationWrite,
	canonicalWrites int,
	infraRows int,
	infraWrites int,
	crossRepoWrites int,
	workloadReplayCount int,
	timing platformMaterializationTiming,
) {
	slog.InfoContext(ctx, "deployment mapping materialization completed",
		slog.String("scope_id", intent.ScopeID),
		slog.String("generation_id", intent.GenerationID),
		slog.String("domain", string(DomainDeploymentMapping)),
		slog.Int("entity_key_count", len(request.EntityKeys)),
		slog.Int("related_scope_count", len(request.RelatedScopeIDs)),
		slog.Int("canonical_write_count", canonicalWrites),
		slog.Int("infrastructure_row_count", infraRows),
		slog.Int("infrastructure_write_count", infraWrites),
		slog.Int("cross_repo_write_count", crossRepoWrites),
		slog.Int("workload_replay_count", workloadReplayCount),
		slog.Float64("platform_write_duration_seconds", timing.platformWriteDuration.Seconds()),
		slog.Float64("fact_load_duration_seconds", timing.factLoadDuration.Seconds()),
		slog.Float64("infrastructure_extract_duration_seconds", timing.infrastructureExtract.Seconds()),
		slog.Float64("infrastructure_graph_write_duration_seconds", timing.infrastructureGraphWrite.Seconds()),
		slog.Float64("cross_repo_resolution_duration_seconds", timing.crossRepoResolutionDuration.Seconds()),
		slog.Float64("workload_replay_duration_seconds", timing.workloadReplayDuration.Seconds()),
		slog.Float64("phase_publish_duration_seconds", timing.phasePublishDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}

func platformMaterializationWriteFromIntent(intent Intent) (PlatformMaterializationWrite, error) {
	entityKeys := uniqueSortedStrings(intent.EntityKeys)
	if len(entityKeys) == 0 {
		return PlatformMaterializationWrite{}, fmt.Errorf(
			"platform materialization intent %q must include at least one entity key",
			intent.IntentID,
		)
	}

	relatedScopeIDs := uniqueSortedStrings(append(intent.RelatedScopeIDs, intent.ScopeID))
	if len(relatedScopeIDs) == 0 {
		return PlatformMaterializationWrite{}, fmt.Errorf(
			"platform materialization intent %q must include at least one related scope id",
			intent.IntentID,
		)
	}

	return PlatformMaterializationWrite{
		IntentID:        intent.IntentID,
		ScopeID:         intent.ScopeID,
		GenerationID:    intent.GenerationID,
		SourceSystem:    intent.SourceSystem,
		Cause:           intent.Cause,
		EntityKeys:      entityKeys,
		RelatedScopeIDs: relatedScopeIDs,
	}, nil
}

func workloadMaterializationReplayEntityKey(intent Intent) string {
	for _, entityKey := range intent.EntityKeys {
		entityKey = strings.TrimSpace(entityKey)
		if strings.HasPrefix(strings.ToLower(entityKey), "repo:") {
			return entityKey
		}
	}
	for _, entityKey := range intent.EntityKeys {
		entityKey = strings.TrimSpace(entityKey)
		if entityKey == "" || isNonRepositoryReplayKey(entityKey) {
			continue
		}
		if alias := normalizedEntityKey(entityKey); alias != "" {
			return "repo:" + alias
		}
	}
	return "repo:" + strings.TrimSpace(intent.ScopeID)
}

func workloadMaterializationReplayScopes(intent Intent) []string {
	return uniqueSortedStrings(append(intent.RelatedScopeIDs, intent.ScopeID))
}

func isNonRepositoryReplayKey(entityKey string) bool {
	lower := strings.ToLower(strings.TrimSpace(entityKey))
	return strings.HasPrefix(lower, "platform:") ||
		strings.HasPrefix(lower, "aws:") ||
		strings.HasPrefix(lower, "tfstate:") ||
		strings.HasPrefix(lower, "cloud:")
}
