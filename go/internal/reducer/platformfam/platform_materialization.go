// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package platformfam

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	log "github.com/eshu-hq/eshu/go/pkg/log"
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

// PlatformGraphLocker coordinates writes that can touch the same Platform.id.
// Implementations should lock keys in a deterministic order and release the
// locks when fn returns so unrelated platform IDs can still project in
// parallel.
type PlatformGraphLocker interface {
	WithPlatformLocks(ctx context.Context, platformIDs []string, fn func(context.Context) error) error
}

// CrossRepoRelationshipResolver resolves cross-repo dependency edges from
// persisted evidence facts for one scope generation and reports how many
// canonical edges it wrote. The reducer root's CrossRepoRelationshipHandler is
// the production implementation; the family names the behaviour it needs rather
// than the root type, because a family subpackage must never import the root.
type CrossRepoRelationshipResolver interface {
	Resolve(ctx context.Context, scopeID, generationID string) (int, error)
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
	CrossRepoResolver               CrossRepoRelationshipResolver
	WorkloadMaterializationReplayer WorkloadMaterializationReplayer
	PhasePublisher                  gpphase.PhasePublisher
}

// platformMaterializationTiming records success-path stage timings for the
// deployment_mapping reducer domain without affecting reducer ordering.
type platformMaterializationTiming struct {
	platformWriteDuration       time.Duration
	crossRepoResolutionDuration time.Duration
	workloadReplayDuration      time.Duration
	phasePublishDuration        time.Duration
	totalDuration               time.Duration
}

// Handle executes the platform materialization reduction path.
func (h PlatformMaterializationHandler) Handle(
	ctx context.Context,
	intent reducercontract.Intent,
) (reducercontract.Result, error) {
	totalStarted := time.Now()
	var timing platformMaterializationTiming

	if intent.Domain != reducercontract.DomainDeploymentMapping {
		return reducercontract.Result{}, fmt.Errorf(
			"platform materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.Writer == nil {
		return reducercontract.Result{}, fmt.Errorf("platform materialization writer is required")
	}

	request, err := platformMaterializationWriteFromIntent(intent)
	if err != nil {
		return reducercontract.Result{}, err
	}

	platformWriteStarted := time.Now()
	writeResult, err := h.Writer.WritePlatformMaterialization(ctx, request)
	timing.platformWriteDuration = time.Since(platformWriteStarted)
	if err != nil {
		return reducercontract.Result{}, err
	}

	// PROVISIONS_PLATFORM (Repository-[:PROVISIONS_PLATFORM]->Platform) edges from
	// Terraform/terragrunt IaC are materialized by the dedicated
	// platform_infra_materialization domain (PlatformInfraMaterializationHandler),
	// not here. This handler owns the deployment_mapping canonical fact write and
	// cross-repo resolution only.
	canonicalWrites := writeResult.CanonicalWrites

	crossRepoWrites := 0
	workloadReplayCount := 0
	// When CrossRepoResolver is provided, resolve cross-repo dependency edges
	// from persisted evidence facts after platform materialization completes.
	if h.CrossRepoResolver != nil {
		crossRepoStarted := time.Now()
		resolvedCrossRepoWrites, err := h.CrossRepoResolver.Resolve(ctx, intent.ScopeID, intent.GenerationID)
		timing.crossRepoResolutionDuration = time.Since(crossRepoStarted)
		if err != nil {
			return reducercontract.Result{}, fmt.Errorf("cross-repo relationship resolution: %w", err)
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
					return reducercontract.Result{}, fmt.Errorf("replay workload materialization: %w", err)
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
	if err := publishIntentPhase(
		ctx,
		h.PhasePublisher,
		gpphase.IntentAnchor{
			ScopeID:      intent.ScopeID,
			GenerationID: intent.GenerationID,
			EntityKeys:   intent.EntityKeys,
		},
		gpphase.KeyspaceServiceUID,
		gpphase.PhaseDeploymentMapping,
		time.Now().UTC(),
	); err != nil {
		return reducercontract.Result{}, err
	}
	timing.phasePublishDuration = time.Since(phaseStarted)
	timing.totalDuration = time.Since(totalStarted)
	logPlatformMaterializationCompleted(
		ctx,
		intent,
		request,
		canonicalWrites,
		crossRepoWrites,
		workloadReplayCount,
		timing,
	)

	// input_ready reflects INPUT PRESENCE, not write count: the platform writer
	// runs unconditionally, so canonicalWrites==0 is genuine empty work, not an
	// ordering stall. A deployment_mapping intent always carries the entity keys
	// it must materialize (validated in platformMaterializationWriteFromIntent),
	// so input is present whenever the request has entity keys.
	inputReady := len(request.EntityKeys) > 0
	return reducercontract.Result{
		IntentID:        intent.IntentID,
		Domain:          reducercontract.DomainDeploymentMapping,
		Status:          reducercontract.ResultStatusSucceeded,
		EvidenceSummary: evidenceSummary,
		CanonicalWrites: canonicalWrites,
		SubDurations:    platformMaterializationSubDurations(timing),
		SubSignals:      reducercontract.MaterializationDiagnosticSignals(inputReady, canonicalWrites),
	}, nil
}

// platformMaterializationSubDurations converts the internal per-phase timing
// struct into the Result.SubDurations map so the service layer emits
// sub_duration_<key>_seconds log attributes alongside handler_duration_seconds.
// Keys follow the workload_materialization naming convention so operators can
// compare sub-phase timing across domains in the same structured-log stream.
// Non-duration diagnostic signals (input_ready, written_rows) are carried
// separately in Result.SubSignals so the _seconds suffix stays honest.
func platformMaterializationSubDurations(t platformMaterializationTiming) map[string]float64 {
	return map[string]float64{
		"platform_write":     t.platformWriteDuration.Seconds(),
		"cross_repo_resolve": t.crossRepoResolutionDuration.Seconds(),
		"workload_replay":    t.workloadReplayDuration.Seconds(),
		"phase_publish":      t.phasePublishDuration.Seconds(),
		"total":              t.totalDuration.Seconds(),
	}
}

func logPlatformMaterializationCompleted(
	ctx context.Context,
	intent reducercontract.Intent,
	request PlatformMaterializationWrite,
	canonicalWrites int,
	crossRepoWrites int,
	workloadReplayCount int,
	timing platformMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "deployment mapping materialization completed",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.Domain(string(reducercontract.DomainDeploymentMapping)),
		slog.Int("entity_key_count", len(request.EntityKeys)),
		slog.Int("related_scope_count", len(request.RelatedScopeIDs)),
		slog.Int("canonical_write_count", canonicalWrites),
		slog.Int("cross_repo_write_count", crossRepoWrites),
		slog.Int("workload_replay_count", workloadReplayCount),
		slog.Float64("platform_write_duration_seconds", timing.platformWriteDuration.Seconds()),
		slog.Float64("cross_repo_resolution_duration_seconds", timing.crossRepoResolutionDuration.Seconds()),
		slog.Float64("workload_replay_duration_seconds", timing.workloadReplayDuration.Seconds()),
		slog.Float64("phase_publish_duration_seconds", timing.phasePublishDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}

func platformMaterializationWriteFromIntent(intent reducercontract.Intent) (PlatformMaterializationWrite, error) {
	entityKeys := payloadcore.UniqueSortedStrings(intent.EntityKeys)
	if len(entityKeys) == 0 {
		return PlatformMaterializationWrite{}, fmt.Errorf(
			"platform materialization intent %q must include at least one entity key",
			intent.IntentID,
		)
	}

	relatedScopeIDs := payloadcore.UniqueSortedStrings(append(intent.RelatedScopeIDs, intent.ScopeID))
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

func workloadMaterializationReplayEntityKey(intent reducercontract.Intent) string {
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
		if alias := payloadcore.NormalizedEntityKey(entityKey); alias != "" {
			return "repo:" + alias
		}
	}
	return "repo:" + strings.TrimSpace(intent.ScopeID)
}

func workloadMaterializationReplayScopes(intent reducercontract.Intent) []string {
	return payloadcore.UniqueSortedStrings(append(intent.RelatedScopeIDs, intent.ScopeID))
}

func isNonRepositoryReplayKey(entityKey string) bool {
	lower := strings.ToLower(strings.TrimSpace(entityKey))
	return strings.HasPrefix(lower, "platform:") ||
		strings.HasPrefix(lower, "aws:") ||
		strings.HasPrefix(lower, "tfstate:") ||
		strings.HasPrefix(lower, "cloud:")
}

// publishIntentPhase publishes the readiness milestone for one intent anchor.
// A nil publisher and an anchor that cannot name a bounded slice are both
// no-ops, so a handler wired without readiness publication still runs.
//
// This lives beside its caller rather than in gpphase: that package is the leaf
// the reducer root and every family import, and its contract is plain data,
// constants, and pure validation. Building the state is pure and stays there as
// [gpphase.StateForIntent]; the write belongs to whoever holds the publisher.
func publishIntentPhase(
	ctx context.Context,
	publisher gpphase.PhasePublisher,
	anchor gpphase.IntentAnchor,
	keyspace gpphase.Keyspace,
	phase gpphase.Phase,
	observedAt time.Time,
) error {
	if publisher == nil {
		return nil
	}
	state, ok := gpphase.StateForIntent(anchor, keyspace, phase, observedAt)
	if !ok {
		return nil
	}
	if err := publisher.PublishGraphProjectionPhases(ctx, []gpphase.PhaseState{state}); err != nil {
		return fmt.Errorf("publish %s phase: %w", phase, err)
	}
	return nil
}
