// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rationale

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// EvidenceSource is the evidence_source the promoted rationale handler keeps
// after moving onto the shared-projection runner (#2869).
const EvidenceSource = "reducer/rationale"

// IntentWriter persists durable shared-projection intents for
// rationale EXPLAINS edge materialization (#2869). The promoted handler emits
// intents instead of writing edges directly so the #2755 partitioned runner
// projects them under file-scoped partition keys and the #2898 refresh fence owns
// the single per-repo retract.
type IntentWriter interface {
	UpsertIntents(ctx context.Context, rows []sharedintent.Row) error
}

// MaterializationHandler projects EXPLAINS edges from intent-comment
// rationale (WHY/HACK/NOTE/TODO/FIXME) to the code entities they precede (issue
// #2230). It owns identity-only Rationale nodes; comment text stays in the
// Postgres content/fact store (design 430). The promoted handler emits durable
// shared-projection intents under file-scoped partition keys, with one whole-scope
// refresh intent per repository owning the retract and each edge fenced behind it
// (#2869).
type MaterializationHandler struct {
	FactLoader   factload.FactLoader
	IntentWriter IntentWriter
}

// Handle executes the rationale edge materialization path.
func (h MaterializationHandler) Handle(ctx context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	if intent.Domain != reducercontract.DomainRationaleMaterialization {
		return reducercontract.Result{}, fmt.Errorf(
			"rationale materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return reducercontract.Result{}, fmt.Errorf("rationale materialization fact loader is required")
	}
	if h.IntentWriter == nil {
		return reducercontract.Result{}, fmt.Errorf("rationale materialization intent writer is required")
	}

	slog.InfoContext(
		ctx, "rationale materialization started",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.Domain(string(intent.Domain)),
	)

	envelopes, err := loadRationaleMaterializationFacts(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("load facts for rationale materialization: %w", err)
	}

	deltaScope := BuildDeltaScope(envelopes)
	repoIDs, rows := ExtractRows(envelopes)
	repoIDs = mergeRationaleRepositoryIDs(repoIDs, deltaScope.RepositoryIDs)
	contextByRepoID := schemadecode.BuildProjectionContexts(envelopes, intent.GenerationID)
	contextRepoIDs := make([]string, 0, len(contextByRepoID))
	for repoID := range contextByRepoID {
		contextRepoIDs = append(contextRepoIDs, repoID)
	}
	// A full generation with no current rationale rows still needs one repo-wide
	// refresh to retract edges whose comments disappeared. Projection contexts
	// are the admitted repository set: they require both repo_id and
	// source_run_id, so this does not manufacture refreshes for malformed input.
	repoIDs = mergeRationaleRepositoryIDs(repoIDs, contextRepoIDs)
	if len(repoIDs) == 0 || len(contextByRepoID) == 0 {
		return reducercontract.Result{
			IntentID:        intent.IntentID,
			Domain:          reducercontract.DomainRationaleMaterialization,
			Status:          reducercontract.ResultStatusSucceeded,
			EvidenceSummary: "no repositories available for rationale materialization",
		}, nil
	}

	createdAt := intent.EnqueuedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	intentRows := BuildSharedIntentRows(rows, deltaScope, repoIDs, contextByRepoID, createdAt)
	if len(intentRows) > 0 {
		if err := h.IntentWriter.UpsertIntents(ctx, intentRows); err != nil {
			return reducercontract.Result{}, fmt.Errorf("write rationale intents: %w", err)
		}
	}

	slog.InfoContext(
		ctx, "rationale materialization completed",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		slog.Int("intent_count", len(intentRows)),
		slog.Int("edge_count", len(rows)),
		slog.Int("repo_count", len(repoIDs)),
	)

	return reducercontract.Result{
		IntentID: intent.IntentID,
		Domain:   reducercontract.DomainRationaleMaterialization,
		Status:   reducercontract.ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"emitted %d durable rationale intents across %d repositories",
			len(intentRows),
			len(repoIDs),
		),
		CanonicalWrites: len(intentRows),
	}, nil
}

// ExtractRows builds EXPLAINS edge rows from content entity facts
// that carry parser-emitted rationale_comments metadata. Each distinct
// (entity, comment kind, comment text) yields one identity-stable Rationale node
// and one EXPLAINS edge to the entity.
func ExtractRows(envelopes []facts.Envelope) ([]string, []map[string]any) {
	repoSet := make(map[string]struct{})
	rows := make([]map[string]any, 0)
	seen := make(map[string]struct{})
	for _, env := range envelopes {
		if env.FactKind != factload.FactKindContentEntity || env.IsTombstone {
			continue
		}
		entityID := payloadcore.SemanticPayloadString(env.Payload, "entity_id")
		repoID := payloadcore.SemanticPayloadString(env.Payload, "repo_id")
		if entityID == "" || repoID == "" {
			continue
		}
		// targetPath is the repo-relative path emitted on the content_entity fact.
		// It is the durable anchor for the file-scoped intent partition key; the
		// separate repository delta scope qualifies changed paths for target.path
		// retraction. It rides every edge row as provenance (#2869).
		//
		// Read "relative_path", which is the key contentEntityFactEnvelope actually
		// emits (git_content_fact_envelopes.go), and which every sibling extractor
		// reads -- semantic_entity_materialization, sql_relationship_embedded_query,
		// and sql_relationship_materialization. This read was "path", a key no
		// content-entity fact carries, so targetPath was empty for every rationale
		// edge in production and the file-scoped anchor hashed into
		// rationaleFilePartitionKey was blank. The bug survived because the only
		// fixtures exercising it supplied "path" -- the key the extractor wanted
		// rather than the one the collector sends (#5998).
		targetPath := payloadcore.SemanticPayloadString(env.Payload, "relative_path")
		for _, comment := range rationalePayloadComments(env.Payload) {
			kind := strings.TrimSpace(payloadcore.AnyToString(comment["kind"]))
			text := strings.TrimSpace(payloadcore.AnyToString(comment["text"]))
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
				"action":           reducercontract.IntentActionUpsert,
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
	if comments := payloadcore.MapSlice(payload["rationale_comments"]); len(comments) > 0 {
		return comments
	}
	return payloadcore.MapSlice(payloadcore.PayloadMap(payload, "entity_metadata")["rationale_comments"])
}

func rationaleExcerptHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}
