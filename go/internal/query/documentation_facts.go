// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	documentationFactKindAliasSemanticObservation              = "semantic_observation"
	documentationFactKindAliasDocumentationObservation         = "documentation_observation"
	documentationFactKindAliasSemanticDocumentationObservation = "semantic_documentation_observation"
)

// The documentation-fact filter and read model are aliases onto
// querycontract, so this package's call sites keep their unexported spelling
// while a ContentStore double outside package query can still name them
// (#6060).
type (
	documentationFactFilter        = querycontract.DocumentationFactFilter
	documentationFactListReadModel = querycontract.DocumentationFactListReadModel
)

// Documentation fact reads bind to a generation (#7128). The default read
// serves the scope's active generation: superseded generations stop being
// current truth the moment a newer one activates and retention prunes them, so
// serving them under an exact/fresh envelope returns retention-dependent
// results. A caller that names generation_id keeps the exact read it always had
// and gets it labelled instead.
const (
	// documentationFactBindingActiveScope is a scope_id read bound to that
	// scope's active generation through a scalar subquery.
	documentationFactBindingActiveScope = "active_scope"
	// documentationFactBindingActiveJoin is an anchor-only read bound to each
	// scope's active generation through one INNER JOIN on ingestion_scopes.
	documentationFactBindingActiveJoin = "active_join"
	// documentationFactBindingActiveProbe is an anchor-only read with no
	// payload filter and no scoped token, bound through a per-row primary-key
	// probe of ingestion_scopes so its ordered scan keeps its early stop.
	documentationFactBindingActiveProbe = "active_probe"
	// documentationFactBindingExplicit is a read of the generation the caller
	// named.
	documentationFactBindingExplicit = "explicit"

	// documentationGenerationBindingAttr is the bounded span attribute carrying
	// the binding form on the query.documentation_facts handler span.
	documentationGenerationBindingAttr = "eshu.documentation.generation_binding"
)

// documentationFactBindingForm is the ONE place that chooses how a facts read
// is bound to a generation. The SQL builder, the read model, and the handler
// telemetry all call it and nothing else picks a form, so they can never
// disagree.
//
//   - a named generation_id keeps its exact read (explicit);
//   - a scope_id read binds through a scalar subquery on that scope (active_scope);
//   - an anchor-only read binds through one INNER JOIN (active_join), except
//   - one with no payload, target, or text filter and no scoped token, which is
//     the fact_kind=source page: an ordered scan of the documentation_source
//     partial index stops after LIMIT rows, and a join (or EXISTS, which the
//     planner flattens into the same semi-join) would replace that early stop
//     with a sort of every source row, so it probes the scope by primary key
//     per candidate row instead (active_probe). A scoped token needs the scope
//     payload for its authorization predicates, so it takes the join.
func documentationFactBindingForm(filter documentationFactFilter) string {
	switch {
	case strings.TrimSpace(filter.GenerationID) != "":
		return documentationFactBindingExplicit
	case strings.TrimSpace(filter.ScopeID) != "":
		return documentationFactBindingActiveScope
	case len(querycontract.DocumentationTargetRefsFromFactFilter(filter)) == 0 &&
		strings.TrimSpace(filter.SourceID) == "" &&
		strings.TrimSpace(filter.DocumentID) == "" &&
		strings.TrimSpace(filter.SectionID) == "" &&
		strings.TrimSpace(filter.Query) == "" &&
		!documentationAuthorizationApplies(filter.AllowedRepositoryIDs, filter.AllowedScopeIDs):
		return documentationFactBindingActiveProbe
	default:
		return documentationFactBindingActiveJoin
	}
}

// documentationFactBindingMode maps a binding form to the response mode.
func documentationFactBindingMode(form string) string {
	if form == documentationFactBindingExplicit {
		return querycontract.DocumentationFactBindingExplicit
	}
	return querycontract.DocumentationFactBindingActive
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
	span.SetAttributes(attribute.String(documentationGenerationBindingAttr, documentationFactBindingForm(filter)))
	filter, ok = documentationFactFilterWithRepositoryAccess(r.Context(), filter)
	if !ok {
		empty := documentationFactListReadModel{}
		WriteSuccess(w, r, http.StatusOK, documentationFactsResponse(empty, page, filter), documentationFactsTruth(h.profile(), empty))
		return
	}
	store, ok := h.documentationStore(w, r)
	if !ok {
		return
	}
	readModel, err := store.DocumentationFacts(r.Context(), filter)
	if err != nil {
		writeDocumentationInternalError(w, r)
		return
	}
	WriteSuccess(w, r, http.StatusOK, documentationFactsResponse(readModel, page, filter), documentationFactsTruth(h.profile(), readModel))
}

// documentationFactsTruth builds the truth envelope for one facts page. The
// store proves the generation lifecycle behind the page, so a page read from a
// superseded, pending, or unknown generation, or from a scope with no active
// generation, is not reported as fresh (#7128).
func documentationFactsTruth(profile QueryProfile, readModel documentationFactListReadModel) *TruthEnvelope {
	truth := BuildTruthEnvelope(
		profile,
		documentationFactsCapability,
		TruthBasisSemanticFacts,
		"resolved from durable collected documentation facts",
	)
	freshness := readModel.Freshness
	if freshness.State == "" || freshness.State == querycontract.FreshnessFresh {
		return truth
	}
	truth.Freshness.State = freshness.State
	truth.Freshness.Detail = freshness.Detail
	if freshness.Cause != "" {
		WithFreshnessCause(truth, freshness.Cause)
	}
	return truth
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
	if !documentationFactFilterHasScopeOrAnchor(filter) {
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

// documentationFactFilterHasScopeOrAnchor reports whether the filter names
// something to anchor a listing to. It is a function rather than a method
// because documentationFactFilter is now an alias onto querycontract, and Go
// only allows methods in the package that declares the type. Keeping it here
// keeps the internal/facts dependency out of querycontract, which stays
// dependency-neutral.
func documentationFactFilterHasScopeOrAnchor(f documentationFactFilter) bool {
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

func documentationFactsResponse(
	readModel documentationFactListReadModel,
	page documentationPage,
	filter documentationFactFilter,
) map[string]any {
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
		"states":           documentationFactListStates(missingEvidence, readModel.EmptyReason),
		"generation_binding": map[string]any{
			"mode":          documentationFactBindingMode(documentationFactBindingForm(filter)),
			"generation_id": readModel.Binding.GenerationID,
			"is_active":     readModel.Binding.IsActive,
		},
	}
	if nextCursor != "" {
		body["next_cursor"] = nextCursor
	}
	return body
}

// documentationFactListStates lists why a page is empty. The two scope states
// are appended only when the store proved them from the scope's own row.
func documentationFactListStates(missingEvidence bool, emptyReason string) []string {
	if !missingEvidence {
		return []string{}
	}
	states := []string{"no_documentation_facts"}
	switch emptyReason {
	case querycontract.DocumentationFactEmptyScopeNotFound, querycontract.DocumentationFactEmptyNoActiveGeneration:
		states = append(states, emptyReason)
	}
	return states
}
