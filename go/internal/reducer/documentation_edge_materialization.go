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

const documentationEvidenceSource = "reducer/documentation"

// DocumentationEdgeMaterializationHandler projects DOCUMENTS edges from exact
// documentation entity mentions to the code entities or workloads they resolve
// to (issue #2231). It owns identity-only DocumentationSection nodes; section
// bodies stay in the Postgres content/fact store (design 430).
type DocumentationEdgeMaterializationHandler struct {
	FactLoader           FactLoader
	EdgeWriter           SharedProjectionEdgeWriter
	PriorGenerationCheck PriorGenerationCheck
	// Instruments records the eshu_dp_reducer_input_invalid_facts_total
	// counter for a documentation_entity_mention fact quarantined by the
	// typed decode seam (Contract System v1 Wave 4e). A nil Instruments is a
	// no-op: the counter is skipped but the quarantine still surfaces
	// through Result.SubSignals and the structured error log.
	Instruments *telemetry.Instruments
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

	deltaScope, deltaQuarantined, err := buildDocumentationDeltaScopeWithQuarantine(envelopes, intent.ScopeID)
	if err != nil {
		return Result{}, fmt.Errorf("build documentation delta scope: %w", err)
	}
	rows, mentionQuarantined, err := ExtractDocumentationEdgeRowsWithQuarantine(envelopes, intent.ScopeID)
	if err != nil {
		return Result{}, fmt.Errorf("extract documentation edge rows: %w", err)
	}
	quarantined := append(deltaQuarantined, mentionQuarantined...)
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainDocumentationMaterialization, intent.ScopeID, intent.GenerationID, quarantined)

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
		SubSignals:      inputInvalidSubSignals(inputInvalidCount),
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
//
// It keeps its pre-typing error-free signature (no quarantine slice, no
// error) because it is the entry point the existing table tests
// (documentation_edge_materialization_test.go) exercise directly; it
// delegates to ExtractDocumentationEdgeRowsWithQuarantine and discards the
// quarantine/error results, mirroring the kubernetes_live wave's
// buildKubernetesCorrelationDecisionsWithQuarantine pattern. The reducer
// intent path (Handle) calls the quarantine-aware function directly so it
// can report quarantines.
func ExtractDocumentationEdgeRows(envelopes []facts.Envelope, scopeID string) []map[string]any {
	rows, _, _ := ExtractDocumentationEdgeRowsWithQuarantine(envelopes, scopeID)
	return rows
}

// ExtractDocumentationEdgeRowsWithQuarantine is the typed-decode counterpart
// of ExtractDocumentationEdgeRows (Contract System v1 Wave 4e): it decodes
// each documentation_entity_mention envelope through the sdk/go/factschema
// seam (decodeDocumentationEntityMention) instead of raw payloadStr/mapSlice
// map lookups. A mention fact missing its required document_id, section_id,
// or resolution_status field is quarantined per-fact via
// partitionDecodeFailures rather than the pre-typing behavior of silently
// skipping the fact via the old `if targetID == "" || documentID == "" ||
// sectionID == "" { continue }` check (a missing key and a present-but-empty
// key were indistinguishable, and neither produced any operator signal).
// Every valid mention in the same batch still projects. A non-input_invalid
// decode error (an unsupported schema major) is returned as a fatal error,
// failing the whole intent for durable triage rather than being silently
// skipped.
func ExtractDocumentationEdgeRowsWithQuarantine(envelopes []facts.Envelope, scopeID string) ([]map[string]any, []quarantinedFact, error) {
	rows := make([]map[string]any, 0)
	var quarantined []quarantinedFact
	seen := make(map[string]struct{})
	for _, env := range envelopes {
		if env.FactKind != facts.DocumentationEntityMentionFactKind || env.IsTombstone {
			continue
		}
		mention, err := decodeDocumentationEntityMention(env)
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
		if mention.ResolutionStatus != facts.DocumentationMentionResolutionExact {
			continue
		}
		if len(mention.CandidateRefs) != 1 {
			continue
		}
		ref := mention.CandidateRefs[0]
		targetID := strings.TrimSpace(evidenceRefString(ref.ID))
		targetKind := strings.TrimSpace(evidenceRefString(ref.Kind))
		documentID := mention.DocumentID
		sectionID := mention.SectionID
		// document_id/section_id are REQUIRED on the typed struct, so an
		// ABSENT key already quarantined above; a PRESENT-but-empty value is
		// a valid decode (per the absent-vs-empty contract) but still
		// produces no edge here, preserving the pre-typing behavior where
		// payloadStr returned "" for either an absent or empty key and the
		// handler skipped identically either way.
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
			"mention_kind":     evidenceRefString(mention.MentionKind),
			"action":           IntentActionUpsert,
		})
	}
	return rows, quarantined, nil
}

// evidenceRefString dereferences an optional string pointer decoded from a
// documentationv1.EvidenceRef or documentationv1.EntityMention field, or
// documentationv1's other optional *string fields, returning "" for a nil
// pointer. It mirrors the pre-typing anyToString/payloadStr behavior of
// treating an absent optional value as the empty string.
func evidenceRefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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
