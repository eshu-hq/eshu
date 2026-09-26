// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

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
	filter, ok = documentationFactFilterWithRepositoryAccess(r.Context(), filter)
	if !ok {
		// A token with no grants sees what an ungranted scope or generation
		// gets from the store, so its denial is indistinguishable from an
		// unknown id (#7128).
		state := documentationFactUngrantedPageState(filter)
		empty := documentationFactListReadModel{
			Binding:     state.Binding,
			Freshness:   state.Freshness,
			EmptyReason: state.EmptyReason,
		}
		WriteSuccess(w, r, http.StatusOK, documentationFactsResponse(empty, page, filter), documentationFactsTruth(h.profile(), empty))
		return
	}
	// Recorded from the filter the store builds its SQL from: the access filter
	// above can move a read from the probe to the join (#7128).
	span.SetAttributes(attribute.String(documentationGenerationBindingAttr, documentationFactBindingForm(filter)))
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

// documentationFactPageState labels the generation a page was read from. A
// non-empty active read adds no statement: every returned row already belongs
// to the bound generation. An empty scope read runs one primary-key lookup on
// ingestion_scopes, and an explicit read runs one on scope_generations, so the
// label is proven rather than assumed.
//
// Both lookups honor the caller's grants (#7128 review F1). For a scoped token
// a scope or generation outside its grants is not found, so its label is the
// label of an id that does not exist and discloses nothing about it. The one
// exception is an explicit read that returned rows: those rows passed the full
// facts authorization predicate and carry their own scope and generation ids,
// so the caller may learn the lifecycle of the generation it is reading.
func (cr *ContentReader) documentationFactPageState(
	ctx context.Context,
	span trace.Span,
	filter documentationFactFilter,
	factRows []map[string]any,
) (querycontract.DocumentationFactPageState, error) {
	scopeID := strings.TrimSpace(filter.ScopeID)
	switch documentationFactBindingForm(filter) {
	case documentationFactBindingExplicit:
		generationID := strings.TrimSpace(filter.GenerationID)
		query, args := buildDocumentationFactGenerationLabelSQL(filter, len(factRows) > 0)
		var foundScope, status string
		err := cr.db.QueryRowContext(ctx, query, args...).Scan(&foundScope, &status)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			span.RecordError(err)
			return querycontract.DocumentationFactPageState{}, fmt.Errorf("query documentation facts generation state: %w", err)
		}
		return querycontract.DocumentationFactExplicitGenerationState(
			generationID, err == nil, foundScope, scopeID, status), nil
	case documentationFactBindingActiveScope:
		if len(factRows) > 0 {
			generationID, _ := factRows[0]["generation_id"].(string)
			return querycontract.DocumentationFactPageState{
				Binding: querycontract.DocumentationFactGenerationBinding{GenerationID: generationID, IsActive: true},
			}, nil
		}
		query, args := buildDocumentationFactScopeLabelSQL(filter)
		var status string
		var active sql.NullString
		err := cr.db.QueryRowContext(ctx, query, args...).Scan(&status, &active)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			span.RecordError(err)
			return querycontract.DocumentationFactPageState{}, fmt.Errorf("query documentation facts scope state: %w", err)
		}
		state := querycontract.DocumentationFactEmptyScopeState(err == nil, status, strings.TrimSpace(active.String))
		span.AddEvent("documentation.empty_page", trace.WithAttributes(
			attribute.String("reason", state.EmptyReason),
			attribute.Bool("scoped_grant", documentationAuthorizationApplies(filter.AllowedRepositoryIDs, filter.AllowedScopeIDs)),
		))
		return state, nil
	default:
		// Every row of an anchor-only read is active by construction.
		return querycontract.DocumentationFactPageState{
			Binding: querycontract.DocumentationFactGenerationBinding{IsActive: true},
		}, nil
	}
}

// documentationFactUngrantedPageState is the page state of a read that sees
// nothing: the label a scope or generation outside the caller's grants gets
// from documentationFactPageState. A token with no grants at all gets it
// without a statement, so it cannot tell its denial from an unknown id.
func documentationFactUngrantedPageState(filter documentationFactFilter) querycontract.DocumentationFactPageState {
	switch documentationFactBindingForm(filter) {
	case documentationFactBindingExplicit:
		return querycontract.DocumentationFactExplicitGenerationState(
			strings.TrimSpace(filter.GenerationID), false, "", strings.TrimSpace(filter.ScopeID), "")
	case documentationFactBindingActiveScope:
		return querycontract.DocumentationFactEmptyScopeState(false, "", "")
	default:
		return querycontract.DocumentationFactPageState{
			Binding: querycontract.DocumentationFactGenerationBinding{IsActive: true},
		}
	}
}

// buildDocumentationFactScopeLabelSQL reads the ingestion_scopes row that
// explains an empty scope page, restricted to the caller's scope grants.
func buildDocumentationFactScopeLabelSQL(filter documentationFactFilter) (string, []any) {
	args := []any{strings.TrimSpace(filter.ScopeID)}
	clauses := []string{"ingestion_scopes.scope_id = $1"}
	clauses, args = appendDocumentationScopeGrantClause(
		clauses, args, "ingestion_scopes.scope_id", "ingestion_scopes",
		filter.AllowedRepositoryIDs, filter.AllowedScopeIDs,
	)
	return "SELECT ingestion_scopes.status, ingestion_scopes.active_generation_id\nFROM ingestion_scopes\nWHERE " +
		strings.Join(clauses, " AND "), args
}

// buildDocumentationFactGenerationLabelSQL reads the scope_generations row of
// a caller-named generation. It is restricted to the caller's scope grants
// unless the page returned rows, which already passed the facts authorization
// predicate.
func buildDocumentationFactGenerationLabelSQL(filter documentationFactFilter, rowsReturned bool) (string, []any) {
	args := []any{strings.TrimSpace(filter.GenerationID)}
	clauses := []string{"scope_generations.generation_id = $1"}
	join := ""
	if !rowsReturned && documentationAuthorizationApplies(filter.AllowedRepositoryIDs, filter.AllowedScopeIDs) {
		join = "\nLEFT JOIN ingestion_scopes ON ingestion_scopes.scope_id = scope_generations.scope_id"
		clauses, args = appendDocumentationScopeGrantClause(
			clauses, args, "scope_generations.scope_id", "ingestion_scopes",
			filter.AllowedRepositoryIDs, filter.AllowedScopeIDs,
		)
	}
	return "SELECT scope_generations.scope_id, scope_generations.status\nFROM scope_generations" + join +
		"\nWHERE " + strings.Join(clauses, " AND "), args
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
