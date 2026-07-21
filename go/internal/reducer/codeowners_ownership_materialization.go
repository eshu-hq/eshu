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

const codeownersOwnershipEvidenceSource = "reducer/codeowners"

// CodeownersOwnershipEdgeMaterializationHandler projects
// Repository-[:DECLARES_CODEOWNER]->CodeownerTeam edges from directly-emitted
// codeowners.ownership facts (issue #5419 Phase 3). It mirrors
// DocumentationEdgeMaterializationHandler: codeowners.ownership is a
// direct-emitted fact (not a parser entity), so the reducer consumes it and
// rides the shared-projection intent-queue path rather than the
// canonical-projector entity path.
type CodeownersOwnershipEdgeMaterializationHandler struct {
	FactLoader           FactLoader
	EdgeWriter           SharedProjectionEdgeWriter
	PriorGenerationCheck PriorGenerationCheck
	// Instruments records the eshu_dp_reducer_input_invalid_facts_total counter
	// for a codeowners.ownership fact quarantined by the typed decode seam. A
	// nil Instruments is a no-op: the counter is skipped but the quarantine
	// still surfaces through Result.SubSignals and the structured error log.
	Instruments *telemetry.Instruments
}

// Handle executes the codeowners ownership edge materialization path.
func (h CodeownersOwnershipEdgeMaterializationHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainCodeownersOwnership {
		return Result{}, fmt.Errorf(
			"codeowners ownership materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("codeowners ownership materialization fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("codeowners ownership materialization edge writer is required")
	}

	slog.InfoContext(
		ctx, "codeowners ownership materialization started",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.Domain(string(intent.Domain)),
	)

	envelopes, err := loadCodeownersOwnershipMaterializationFacts(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for codeowners ownership materialization: %w", err)
	}

	deltaScope := buildCodeownersOwnershipDeltaScope(envelopes)
	rows, quarantined, err := ExtractCodeownersOwnershipEdgeRowsWithQuarantine(envelopes, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("extract codeowners ownership edge rows: %w", err)
	}
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainCodeownersOwnership, intent.ScopeID, intent.GenerationID, quarantined)

	repositoryIDs := codeownersOwnershipRepositoryIDs(rows, deltaScope)

	skipRetract, err := h.shouldSkipCodeownersOwnershipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	if !skipRetract {
		retractRows := buildCodeownersOwnershipRetractRows(repositoryIDs, deltaScope)
		if len(retractRows) > 0 {
			if err := h.EdgeWriter.RetractEdges(
				ctx,
				DomainCodeownersOwnershipEdges,
				retractRows,
				codeownersOwnershipEvidenceSource,
			); err != nil {
				return Result{}, fmt.Errorf("retract canonical codeowners ownership edges: %w", err)
			}
		}
	}

	writeRows := buildCodeownersOwnershipIntentRows(rows)
	if len(writeRows) > 0 {
		if err := h.EdgeWriter.WriteEdges(
			ctx,
			DomainCodeownersOwnershipEdges,
			writeRows,
			codeownersOwnershipEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical codeowners ownership edges: %w", err)
		}
	}

	slog.InfoContext(
		ctx, "codeowners ownership materialization completed",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		slog.Int("edge_count", len(writeRows)),
	)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainCodeownersOwnership,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf("materialized %d canonical codeowners ownership edges", len(writeRows)),
		CanonicalWrites: len(writeRows),
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
	}, nil
}

func (h CodeownersOwnershipEdgeMaterializationHandler) shouldSkipCodeownersOwnershipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for codeowners ownership retract: %w", err)
	}
	return !hasPrior, nil
}

// codeownersOwnershipRepositoryIDs collects every repository the retract must
// cover: every repo a written row targets, plus every repo the delta scope
// names (so a repo whose CODEOWNERS file was deleted — producing zero rows —
// still gets its stale edges swept).
func codeownersOwnershipRepositoryIDs(rows []map[string]any, deltaScope codeownersOwnershipDeltaScope) []string {
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
		add(anyToString(row["repo_id"]))
	}
	for _, repositoryID := range deltaScope.repositoryIDs {
		add(repositoryID)
	}
	return repositoryIDs
}

// ExtractCodeownersOwnershipEdgeRowsWithQuarantine decodes every
// codeowners.ownership envelope through the sdk/go/factschema seam
// (decodeCodeownersOwnership) and builds one DECLARES_CODEOWNER edge row per
// (pattern, owner) pair: a CODEOWNERS rule line with N owner tokens projects N
// edges, since ownership is declared per-owner, not per-line. A fact missing a
// required field (repo_id, source_path, pattern, owners, order_index) is
// quarantined per-fact via partitionDecodeFailures rather than dropped
// silently; an unsupported schema major is escalated to a fatal error for
// durable triage (see partitionDecodeFailures).
//
// A repeated (repo, path, pattern, owner) key — the same owner listed again
// on a later rule line for the same pattern — collapses into a single row,
// but that row MUST keep the highest (latest) order_index it saw, never the
// first. GitHub's CODEOWNERS resolution is last-match-wins, and the
// downstream precedence resolver picks the highest surviving ordinal as the
// effective owner, so freezing the first occurrence's ordinal would let a
// stale, superseded rule line outrank the true last match.
func ExtractCodeownersOwnershipEdgeRowsWithQuarantine(
	envelopes []facts.Envelope,
	generationID string,
) ([]map[string]any, []quarantinedFact, error) {
	rows := make([]map[string]any, 0)
	var quarantined []quarantinedFact
	rowIndexByKey := make(map[string]int)
	for _, env := range envelopes {
		if env.FactKind != factKindCodeownersOwnership || env.IsTombstone {
			continue
		}
		ownership, err := decodeCodeownersOwnership(env)
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

		repoID := strings.TrimSpace(ownership.RepoID)
		sourcePath := strings.TrimSpace(ownership.SourcePath)
		pattern := strings.TrimSpace(ownership.Pattern)
		if repoID == "" || sourcePath == "" || pattern == "" {
			continue
		}

		for _, owner := range ownership.Owners {
			ownerRef := strings.TrimSpace(owner)
			if ownerRef == "" {
				continue
			}
			key := repoID + "|" + sourcePath + "|" + pattern + "|" + ownerRef
			if idx, dup := rowIndexByKey[key]; dup {
				if existingOrderIndex, ok := rows[idx]["order_index"].(int); ok && ownership.OrderIndex > existingOrderIndex {
					rows[idx]["order_index"] = ownership.OrderIndex
				}
				continue
			}
			rowIndexByKey[key] = len(rows)

			rows = append(rows, map[string]any{
				"repo_id":       repoID,
				"owner_ref":     ownerRef,
				"pattern":       pattern,
				"source_path":   sourcePath,
				"order_index":   ownership.OrderIndex,
				"generation_id": generationID,
				"action":        IntentActionUpsert,
			})
		}
	}
	return rows, quarantined, nil
}

func buildCodeownersOwnershipIntentRows(rows []map[string]any) []SharedProjectionIntentRow {
	intents := make([]SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		intents = append(intents, SharedProjectionIntentRow{
			ProjectionDomain: DomainCodeownersOwnershipEdges,
			PartitionKey: anyToString(row["repo_id"]) + "->" + anyToString(row["source_path"]) +
				"->" + anyToString(row["pattern"]) + "->" + anyToString(row["owner_ref"]),
			RepositoryID: anyToString(row["repo_id"]),
			Payload:      copyPayload(row),
		})
	}
	return intents
}
