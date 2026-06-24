// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	documentationFactKindAliasSemanticObservation              = "semantic_observation"
	documentationFactKindAliasDocumentationObservation         = "documentation_observation"
	documentationFactKindAliasSemanticDocumentationObservation = "semantic_documentation_observation"
)

type documentationFactFilter struct {
	FactKind             string
	ScopeID              string
	GenerationID         string
	Repository           string
	TargetKind           string
	TargetID             string
	ServiceID            string
	SourceID             string
	DocumentID           string
	SectionID            string
	Query                string
	UpdatedSince         *time.Time
	Limit                int
	Cursor               string
	Offset               int
	AllowedScopeIDs      []string
	AllowedRepositoryIDs []string
}

type documentationFactListReadModel struct {
	Facts      []map[string]any
	NextCursor string
}

func (h *DocumentationHandler) listFacts(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryDocumentationFacts,
		"GET /api/v0/documentation/facts",
		documentationFactsCapability,
	)
	defer span.End()

	if h.unsupported(w, r, documentationFactsCapability) {
		return
	}
	updatedSince, ok := documentationUpdatedSince(w, r)
	if !ok {
		return
	}
	page, ok := documentationPagination(w, r)
	if !ok {
		return
	}
	filter, ok := documentationFactRequestFilter(w, r, page, updatedSince)
	if !ok {
		return
	}
	filter, ok = documentationFactFilterWithRepositoryAccess(r.Context(), filter)
	if !ok {
		WriteSuccess(w, r, http.StatusOK, documentationFactsResponse(documentationFactListReadModel{}, page), BuildTruthEnvelope(
			h.profile(),
			documentationFactsCapability,
			TruthBasisSemanticFacts,
			"resolved from durable collected documentation facts",
		))
		return
	}
	store, ok := h.documentationStore(w, r)
	if !ok {
		return
	}
	readModel, err := store.documentationFacts(r.Context(), filter)
	if err != nil {
		writeDocumentationInternalError(w, r)
		return
	}
	WriteSuccess(w, r, http.StatusOK, documentationFactsResponse(readModel, page), BuildTruthEnvelope(
		h.profile(),
		documentationFactsCapability,
		TruthBasisSemanticFacts,
		"resolved from durable collected documentation facts",
	))
}

func documentationFactRequestFilter(
	w http.ResponseWriter,
	r *http.Request,
	page documentationPage,
	updatedSince *time.Time,
) (documentationFactFilter, bool) {
	factKind, ok := normalizeDocumentationFactKind(QueryParam(r, "fact_kind"))
	if !ok {
		writeDocumentationError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, "unsupported documentation fact_kind", "")
		return documentationFactFilter{}, false
	}
	filter := documentationFactFilter{
		FactKind:     factKind,
		ScopeID:      QueryParam(r, "scope_id"),
		GenerationID: QueryParam(r, "generation_id"),
		Repository:   QueryParam(r, "repo"),
		TargetKind:   QueryParam(r, "target_kind"),
		TargetID:     QueryParam(r, "target_id"),
		ServiceID:    QueryParam(r, "service_id"),
		SourceID:     QueryParam(r, "source_id"),
		DocumentID:   QueryParam(r, "document_id"),
		SectionID:    QueryParam(r, "section_id"),
		Query:        QueryParam(r, "q"),
		UpdatedSince: updatedSince,
		Limit:        page.limit,
		Cursor:       page.cursor,
		Offset:       page.offset,
	}
	if !filter.hasScopeOrAnchor() {
		writeDocumentationError(
			w,
			r,
			http.StatusBadRequest,
			ErrorCodeInvalidArgument,
			"documentation facts require scope_id, repo, target_id, service_id, source_id, document_id, or section_id",
			"",
		)
		return documentationFactFilter{}, false
	}
	return filter, true
}

func (f documentationFactFilter) hasScopeOrAnchor() bool {
	if f.FactKind == facts.DocumentationSourceFactKind {
		return true
	}
	return strings.TrimSpace(f.ScopeID) != "" ||
		strings.TrimSpace(f.Repository) != "" ||
		strings.TrimSpace(f.TargetID) != "" ||
		strings.TrimSpace(f.ServiceID) != "" ||
		strings.TrimSpace(f.SourceID) != "" ||
		strings.TrimSpace(f.DocumentID) != "" ||
		strings.TrimSpace(f.SectionID) != ""
}

func normalizeDocumentationFactKind(raw string) (string, bool) {
	switch strings.TrimSpace(raw) {
	case "":
		return "", true
	case "source", facts.DocumentationSourceFactKind:
		return facts.DocumentationSourceFactKind, true
	case "document", facts.DocumentationDocumentFactKind:
		return facts.DocumentationDocumentFactKind, true
	case "section", facts.DocumentationSectionFactKind:
		return facts.DocumentationSectionFactKind, true
	case "link", facts.DocumentationLinkFactKind:
		return facts.DocumentationLinkFactKind, true
	case "entity_mention", facts.DocumentationEntityMentionFactKind:
		return facts.DocumentationEntityMentionFactKind, true
	case "claim_candidate", facts.DocumentationClaimCandidateFactKind:
		return facts.DocumentationClaimCandidateFactKind, true
	case documentationFactKindAliasSemanticObservation,
		documentationFactKindAliasDocumentationObservation,
		documentationFactKindAliasSemanticDocumentationObservation,
		facts.SemanticDocumentationObservationFactKind:
		return facts.SemanticDocumentationObservationFactKind, true
	default:
		return "", false
	}
}

func documentationFactsResponse(readModel documentationFactListReadModel, page documentationPage) map[string]any {
	facts := readModel.Facts
	if facts == nil {
		facts = []map[string]any{}
	}
	nextCursor := strings.TrimSpace(readModel.NextCursor)
	missingEvidence := len(facts) == 0
	body := map[string]any{
		"facts":            facts,
		"count":            len(facts),
		"limit":            page.limit,
		"truncated":        nextCursor != "",
		"missing_evidence": missingEvidence,
		"states":           documentationFactListStates(missingEvidence),
	}
	if nextCursor != "" {
		body["next_cursor"] = nextCursor
	}
	return body
}

func documentationFactListStates(missingEvidence bool) []string {
	if missingEvidence {
		return []string{"no_documentation_facts"}
	}
	return []string{}
}
