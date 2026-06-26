// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const documentationEvidenceSource = "reducer/documentation"

// DocumentationEdgeMaterializationHandler projects DOCUMENTS edges from exact
// documentation entity mentions to the code entities or workloads they resolve
// to (issue #2231). It owns identity-only DocumentationSection nodes; section
// bodies stay in the Postgres content/fact store (design 430).
type DocumentationEdgeMaterializationHandler struct {
	FactLoader           FactLoader
	EdgeWriter           SharedProjectionEdgeWriter
	PriorGenerationCheck PriorGenerationCheck
}

// Handle executes the documentation edge materialization path.
func (h DocumentationEdgeMaterializationHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainDocumentationMaterialization {
		return Result{}, fmt.Errorf(
			"documentation materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("documentation materialization fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("documentation materialization edge writer is required")
	}

	slog.InfoContext(
		ctx, "documentation materialization started",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.Domain(string(intent.Domain)),
	)

	envelopes, err := loadDocumentationMaterializationFacts(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for documentation materialization: %w", err)
	}

	deltaScope := buildDocumentationDeltaScope(envelopes, intent.ScopeID)
	rows := ExtractDocumentationEdgeRows(envelopes, intent.ScopeID)

	skipRetract, err := h.shouldSkipDocumentationRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	if !skipRetract {
		if err := h.EdgeWriter.RetractEdges(
			ctx,
			DomainDocumentationEdges,
			buildDocumentationRetractRows([]string{intent.ScopeID}, deltaScope),
			documentationEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical documentation edges: %w", err)
		}
	}

	writeRows := buildDocumentationIntentRows(rows, intent.ScopeID)
	if len(writeRows) > 0 {
		if err := h.EdgeWriter.WriteEdges(
			ctx,
			DomainDocumentationEdges,
			writeRows,
			documentationEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical documentation edges: %w", err)
		}
	}

	slog.InfoContext(
		ctx, "documentation materialization completed",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		slog.Int("edge_count", len(writeRows)),
	)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainDocumentationMaterialization,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf("materialized %d canonical documentation edges", len(writeRows)),
		CanonicalWrites: len(writeRows),
	}, nil
}

func (h DocumentationEdgeMaterializationHandler) shouldSkipDocumentationRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for documentation retract: %w", err)
	}
	return !hasPrior, nil
}

// ExtractDocumentationEdgeRows builds DOCUMENTS edge rows from documentation
// entity mention facts. Only a mention whose ResolutionStatus is exact and that
// carries exactly one candidate ref produces an edge; ambiguous, unmatched, and
// multi-candidate mentions do not, preserving correlation truth. A candidate
// whose kind is service is skipped because no Service graph node exists.
func ExtractDocumentationEdgeRows(envelopes []facts.Envelope, scopeID string) []map[string]any {
	rows := make([]map[string]any, 0)
	seen := make(map[string]struct{})
	for _, env := range envelopes {
		if env.FactKind != facts.DocumentationEntityMentionFactKind || env.IsTombstone {
			continue
		}
		if payloadStr(env.Payload, "resolution_status") != facts.DocumentationMentionResolutionExact {
			continue
		}
		refs := mapSlice(env.Payload["candidate_refs"])
		if len(refs) != 1 {
			continue
		}
		targetID := strings.TrimSpace(anyToString(refs[0]["id"]))
		targetKind := strings.TrimSpace(anyToString(refs[0]["kind"]))
		documentID := payloadStr(env.Payload, "document_id")
		sectionID := payloadStr(env.Payload, "section_id")
		if targetID == "" || documentID == "" || sectionID == "" {
			continue
		}
		if targetKind == "service" {
			continue
		}

		sectionUID := documentationSectionNodeUID(documentID, sectionID)
		key := sectionUID + "->" + targetID
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		rows = append(rows, map[string]any{
			"section_uid":      sectionUID,
			"scope_id":         scopeID,
			"document_id":      documentID,
			"section_id":       sectionID,
			"target_entity_id": targetID,
			"target_kind":      targetKind,
			"mention_kind":     payloadStr(env.Payload, "mention_kind"),
			"action":           IntentActionUpsert,
		})
	}
	return rows
}

// documentationSectionNodeUID is the stable identity for a documentation section
// graph node. It is derived from the logical (document, section) pair so a
// re-projected section MERGEs the same node across generations and content
// revisions.
func documentationSectionNodeUID(documentID string, sectionID string) string {
	return "docsection:" + documentID + "|" + sectionID
}

func buildDocumentationIntentRows(rows []map[string]any, scopeID string) []SharedProjectionIntentRow {
	intents := make([]SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		intents = append(intents, SharedProjectionIntentRow{
			ProjectionDomain: DomainDocumentationEdges,
			PartitionKey:     anyToString(row["section_uid"]) + "->" + anyToString(row["target_entity_id"]),
			RepositoryID:     scopeID,
			Payload:          copyPayload(row),
		})
	}
	return intents
}
