// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const rationaleEvidenceSource = "reducer/rationale"

// RationaleEdgeIntentWriter persists durable shared-projection intents for
// rationale EXPLAINS edge materialization (#2869). The promoted handler emits
// intents instead of writing edges directly so the #2755 partitioned runner
// projects them under file-scoped partition keys and the #2898 refresh fence owns
// the single per-repo retract.
type RationaleEdgeIntentWriter interface {
	UpsertIntents(ctx context.Context, rows []SharedProjectionIntentRow) error
}

// RationaleEdgeMaterializationHandler projects EXPLAINS edges from intent-comment
// rationale (WHY/HACK/NOTE/TODO/FIXME) to the code entities they precede (issue
// #2230). It owns identity-only Rationale nodes; comment text stays in the
// Postgres content/fact store (design 430). The promoted handler emits durable
// shared-projection intents under file-scoped partition keys, with one whole-scope
// refresh intent per repository owning the retract and each edge fenced behind it
// (#2869).
type RationaleEdgeMaterializationHandler struct {
	FactLoader   FactLoader
	IntentWriter RationaleEdgeIntentWriter
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
	if h.IntentWriter == nil {
		return Result{}, fmt.Errorf("rationale materialization intent writer is required")
	}

	slog.InfoContext(
		ctx, "rationale materialization started",
		slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
		slog.String(telemetry.LogKeyDomain, string(intent.Domain)),
	)

	envelopes, err := loadRationaleMaterializationFacts(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for rationale materialization: %w", err)
	}

	deltaScope := buildRationaleDeltaScope(envelopes)
	repoIDs, rows := ExtractRationaleEdgeRows(envelopes)
	repoIDs = mergeRationaleRepositoryIDs(repoIDs, deltaScope.repositoryIDs)
	contextByRepoID := buildCodeCallProjectionContexts(envelopes, intent.GenerationID)
	if len(repoIDs) == 0 || len(contextByRepoID) == 0 {
		return Result{
			IntentID:        intent.IntentID,
			Domain:          DomainRationaleMaterialization,
			Status:          ResultStatusSucceeded,
			EvidenceSummary: "no repositories available for rationale materialization",
		}, nil
	}

	createdAt := intent.EnqueuedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	intentRows := buildRationaleSharedIntentRows(rows, deltaScope, repoIDs, contextByRepoID, createdAt)
	if len(intentRows) > 0 {
		if err := h.IntentWriter.UpsertIntents(ctx, intentRows); err != nil {
			return Result{}, fmt.Errorf("write rationale intents: %w", err)
		}
	}

	slog.InfoContext(
		ctx, "rationale materialization completed",
		slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
		slog.Int("intent_count", len(intentRows)),
		slog.Int("edge_count", len(rows)),
		slog.Int("repo_count", len(repoIDs)),
	)

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainRationaleMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"emitted %d durable rationale intents across %d repositories",
			len(intentRows),
			len(repoIDs),
		),
		CanonicalWrites: len(intentRows),
	}, nil
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
		// targetPath is the repo-qualified path of the code entity the comment
		// precedes. It is the durable anchor for the file-scoped partition key and
		// the target.path delta retract (the EXPLAINS edge precedes this entity), so
		// it rides every emitted edge row as provenance (#2869).
		targetPath := semanticPayloadString(env.Payload, "path")
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
				"target_path":      targetPath,
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
