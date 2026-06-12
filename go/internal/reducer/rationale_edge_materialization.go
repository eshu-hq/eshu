package reducer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const rationaleEvidenceSource = "reducer/rationale"

// RationaleEdgeMaterializationHandler projects EXPLAINS edges from intent-comment
// rationale (WHY/HACK/NOTE/TODO/FIXME) to the code entities they precede (issue
// #2230). It owns identity-only Rationale nodes; comment text stays in the
// Postgres content/fact store (design 430).
type RationaleEdgeMaterializationHandler struct {
	FactLoader           FactLoader
	EdgeWriter           SharedProjectionEdgeWriter
	PriorGenerationCheck PriorGenerationCheck
}

// Handle executes the rationale edge materialization path.
func (h RationaleEdgeMaterializationHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainRationaleMaterialization {
		return Result{}, fmt.Errorf(
			"rationale materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("rationale materialization fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("rationale materialization edge writer is required")
	}

	slog.InfoContext(ctx, "rationale materialization started",
		slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
		slog.String(telemetry.LogKeyDomain, string(intent.Domain)),
	)

	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{factKindContentEntity},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for rationale materialization: %w", err)
	}

	repoIDs, rows := ExtractRationaleEdgeRows(envelopes)
	if len(repoIDs) == 0 {
		return Result{
			IntentID:        intent.IntentID,
			Domain:          DomainRationaleMaterialization,
			Status:          ResultStatusSucceeded,
			EvidenceSummary: "no repositories available for rationale materialization",
		}, nil
	}

	skipRetract, err := h.shouldSkipRationaleRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	if !skipRetract {
		if err := h.EdgeWriter.RetractEdges(
			ctx,
			DomainRationaleEdges,
			buildRationaleRetractRows(repoIDs),
			rationaleEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical rationale edges: %w", err)
		}
	}

	writeRows := buildRationaleIntentRows(rows)
	if len(writeRows) > 0 {
		if err := h.EdgeWriter.WriteEdges(
			ctx,
			DomainRationaleEdges,
			writeRows,
			rationaleEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical rationale edges: %w", err)
		}
	}

	slog.InfoContext(ctx, "rationale materialization completed",
		slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
		slog.Int("edge_count", len(writeRows)),
		slog.Int("repo_count", len(repoIDs)),
	)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainRationaleMaterialization,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf("materialized %d canonical rationale edges across %d repositories", len(writeRows), len(repoIDs)),
		CanonicalWrites: len(writeRows),
	}, nil
}

func (h RationaleEdgeMaterializationHandler) shouldSkipRationaleRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for rationale retract: %w", err)
	}
	return !hasPrior, nil
}

// ExtractRationaleEdgeRows builds EXPLAINS edge rows from content entity facts
// that carry parser-emitted rationale_comments metadata. Each distinct
// (entity, comment kind, comment text) yields one identity-stable Rationale node
// and one EXPLAINS edge to the entity.
func ExtractRationaleEdgeRows(envelopes []facts.Envelope) ([]string, []map[string]any) {
	repoSet := make(map[string]struct{})
	rows := make([]map[string]any, 0)
	seen := make(map[string]struct{})
	for _, env := range envelopes {
		if env.FactKind != factKindContentEntity || env.IsTombstone {
			continue
		}
		entityID := semanticPayloadString(env.Payload, "entity_id")
		repoID := semanticPayloadString(env.Payload, "repo_id")
		if entityID == "" || repoID == "" {
			continue
		}
		for _, comment := range rationalePayloadComments(env.Payload) {
			kind := strings.TrimSpace(anyToString(comment["kind"]))
			text := strings.TrimSpace(anyToString(comment["text"]))
			if kind == "" || text == "" {
				continue
			}
			excerptHash := rationaleExcerptHash(text)
			rationaleUID := "rationale:" + entityID + ":" + kind + ":" + excerptHash
			if _, dup := seen[rationaleUID]; dup {
				continue
			}
			seen[rationaleUID] = struct{}{}
			repoSet[repoID] = struct{}{}
			rows = append(rows, map[string]any{
				"rationale_uid":    rationaleUID,
				"target_entity_id": entityID,
				"repo_id":          repoID,
				"comment_kind":     kind,
				"excerpt_hash":     excerptHash,
				"action":           IntentActionUpsert,
			})
		}
	}

	repoIDs := make([]string, 0, len(repoSet))
	for repoID := range repoSet {
		repoIDs = append(repoIDs, repoID)
	}
	return repoIDs, rows
}

// rationalePayloadComments reads the parser-emitted rationale_comments metadata
// that flows through the content-entity snapshot, mirroring how inheritance
// reads bases.
func rationalePayloadComments(payload map[string]any) []map[string]any {
	if comments := mapSlice(payload["rationale_comments"]); len(comments) > 0 {
		return comments
	}
	return mapSlice(payloadMap(payload, "entity_metadata")["rationale_comments"])
}

func rationaleExcerptHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

func buildRationaleIntentRows(rows []map[string]any) []SharedProjectionIntentRow {
	intents := make([]SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		intents = append(intents, SharedProjectionIntentRow{
			ProjectionDomain: DomainRationaleEdges,
			PartitionKey:     anyToString(row["rationale_uid"]) + "->" + anyToString(row["target_entity_id"]),
			RepositoryID:     anyToString(row["repo_id"]),
			Payload:          copyPayload(row),
		})
	}
	return intents
}

func buildRationaleRetractRows(repoIDs []string) []SharedProjectionIntentRow {
	rows := make([]SharedProjectionIntentRow, 0, len(repoIDs))
	for _, repoID := range repoIDs {
		rows = append(rows, SharedProjectionIntentRow{RepositoryID: repoID})
	}
	return rows
}
