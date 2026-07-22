// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const submodulePinEvidenceSource = "reducer/submodule"

// SubmodulePinEdgeMaterializationHandler projects
// Repository-[:PINS_SUBMODULE]->Repository edges from directly-emitted
// submodule.pin facts (issue #5420 Phase 3). It mirrors
// CodeownersOwnershipEdgeMaterializationHandler: submodule.pin is a
// direct-emitted fact (not a parser entity), so the reducer consumes it and
// rides the shared-projection intent-queue path rather than the
// canonical-projector entity path. Unlike codeowners' new CodeownerTeam
// label, both PINS_SUBMODULE endpoints are existing Repository nodes, so no
// new node label or uniqueness constraint is needed.
type SubmodulePinEdgeMaterializationHandler struct {
	FactLoader           FactLoader
	EdgeWriter           SharedProjectionEdgeWriter
	PriorGenerationCheck PriorGenerationCheck
	// Instruments records the eshu_dp_reducer_input_invalid_facts_total counter
	// for a submodule.pin fact quarantined by the typed decode seam. A nil
	// Instruments is a no-op: the counter is skipped but the quarantine still
	// surfaces through Result.SubSignals and the structured error log.
	Instruments *telemetry.Instruments
}

// Handle executes the submodule pin edge materialization path.
func (h SubmodulePinEdgeMaterializationHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainSubmodulePin {
		return Result{}, fmt.Errorf(
			"submodule pin materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("submodule pin materialization fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("submodule pin materialization edge writer is required")
	}

	slog.InfoContext(
		ctx, "submodule pin materialization started",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.Domain(string(intent.Domain)),
	)

	envelopes, err := loadSubmodulePinMaterializationFacts(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for submodule pin materialization: %w", err)
	}

	deltaScope := buildSubmodulePinDeltaScope(envelopes)
	rows, quarantined, err := ExtractSubmodulePinEdgeRowsWithQuarantine(envelopes, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("extract submodule pin edge rows: %w", err)
	}
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainSubmodulePin, intent.ScopeID, intent.GenerationID, quarantined)

	repositoryIDs := submodulePinRepositoryIDs(rows, deltaScope)

	skipRetract, err := h.shouldSkipSubmodulePinRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	if !skipRetract {
		retractRows := buildSubmodulePinRetractRows(repositoryIDs, deltaScope)
		if len(retractRows) > 0 {
			if err := h.EdgeWriter.RetractEdges(
				ctx, DomainSubmodulePinEdges, retractRows, submodulePinEvidenceSource,
			); err != nil {
				return Result{}, fmt.Errorf("retract canonical submodule pin edges: %w", err)
			}
		}
	}

	writeRows := buildSubmodulePinIntentRows(rows)
	if len(writeRows) > 0 {
		if err := h.EdgeWriter.WriteEdges(
			ctx,
			DomainSubmodulePinEdges,
			writeRows,
			submodulePinEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical submodule pin edges: %w", err)
		}
	}

	slog.InfoContext(
		ctx, "submodule pin materialization completed",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		slog.Int("edge_count", len(writeRows)),
	)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainSubmodulePin,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf("materialized %d canonical submodule pin edges", len(writeRows)),
		CanonicalWrites: len(writeRows),
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
	}, nil
}

func (h SubmodulePinEdgeMaterializationHandler) shouldSkipSubmodulePinRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for submodule pin retract: %w", err)
	}
	return !hasPrior, nil
}

// submodulePinRepositoryIDs collects every repository the retract must cover:
// every repo a written row targets (keyed on parent_repo_id — the source side
// of PINS_SUBMODULE), plus every repo the delta scope names (so a repo whose
// ".gitmodules" was deleted — producing zero rows — still gets its stale
// edges swept).
func submodulePinRepositoryIDs(rows []map[string]any, deltaScope submodulePinDeltaScope) []string {
	seen := make(map[string]struct{})
	var repositoryIDs []string
	add := func(repositoryID string) {
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID == "" {
			return
		}
		if _, ok := seen[repositoryID]; ok {
			return
		}
		seen[repositoryID] = struct{}{}
		repositoryIDs = append(repositoryIDs, repositoryID)
	}
	for _, row := range rows {
		add(anyToString(row["parent_repo_id"]))
	}
	for _, repositoryID := range deltaScope.repositoryIDs {
		add(repositoryID)
	}
	return repositoryIDs
}

// ExtractSubmodulePinEdgeRowsWithQuarantine decodes every submodule.pin
// envelope through the sdk/go/factschema seam (decodeSubmodulePin) and builds
// one PINS_SUBMODULE edge row per fact whose submodule URL resolved to a
// known repository (ResolvedRepoID non-empty). A fact missing a required
// field (parent_repo_id, submodule_path) is quarantined per-fact via
// partitionDecodeFailures rather than dropped silently; an unsupported schema
// major is escalated to a fatal error for durable triage (see
// partitionDecodeFailures).
//
// A fact whose URL never resolved to a known repository (ResolvedRepoID nil)
// projects no edge: the parent-repository half of the join is always known,
// but the target half is not, and PINS_SUBMODULE MUST NEVER point at a
// guessed or dangling Repository id (issue #5420 Phase 3 design). PinnedSHA
// MAY be nil (a declared-but-no-gitlink submodule) — the edge still projects
// when ResolvedRepoID is known, carrying pinned_sha only when known: the pin
// relationship is real even when the exact commit is not yet observable.
//
// A repeated (parent_repo_id, submodule_path) key within one generation
// collapses into a single row, keeping the last envelope seen — mirroring the
// general last-write-wins contract for a repeated fact key within a
// generation (there is exactly one stable_fact_key per (parent, path), so a
// repeat here means the same fact re-delivered, not two independent
// declarations).
func ExtractSubmodulePinEdgeRowsWithQuarantine(
	envelopes []facts.Envelope,
	generationID string,
) ([]map[string]any, []quarantinedFact, error) {
	rows := make([]map[string]any, 0)
	var quarantined []quarantinedFact
	rowIndexByKey := make(map[string]int)
	for _, env := range envelopes {
		if env.FactKind != factKindSubmodulePin || env.IsTombstone {
			continue
		}
		pin, err := decodeSubmodulePin(env)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return nil, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}

		parentRepoID := strings.TrimSpace(pin.ParentRepoID)
		submodulePath := strings.TrimSpace(pin.SubmodulePath)
		if parentRepoID == "" || submodulePath == "" {
			continue
		}

		var resolvedRepoID string
		if pin.ResolvedRepoID != nil {
			resolvedRepoID = strings.TrimSpace(*pin.ResolvedRepoID)
		}
		if resolvedRepoID == "" {
			// Unresolved, ambiguous, or dangling submodule URL: never guess a
			// target Repository id, so no edge projects for this fact.
			continue
		}

		var pinnedSHA string
		if pin.PinnedSHA != nil {
			pinnedSHA = strings.TrimSpace(*pin.PinnedSHA)
		}

		row := map[string]any{
			"parent_repo_id":   parentRepoID,
			"resolved_repo_id": resolvedRepoID,
			"submodule_path":   submodulePath,
			"pinned_sha":       pinnedSHA,
			"generation_id":    generationID,
			"action":           IntentActionUpsert,
		}

		key := parentRepoID + "|" + submodulePath
		if idx, dup := rowIndexByKey[key]; dup {
			rows[idx] = row
			continue
		}
		rowIndexByKey[key] = len(rows)
		rows = append(rows, row)
	}
	return rows, quarantined, nil
}

func buildSubmodulePinIntentRows(rows []map[string]any) []SharedProjectionIntentRow {
	intents := make([]SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		intents = append(intents, SharedProjectionIntentRow{
			ProjectionDomain: DomainSubmodulePinEdges,
			PartitionKey:     anyToString(row["parent_repo_id"]) + "->" + anyToString(row["submodule_path"]),
			RepositoryID:     anyToString(row["parent_repo_id"]),
			Payload:          copyPayload(row),
		})
	}
	return intents
}
