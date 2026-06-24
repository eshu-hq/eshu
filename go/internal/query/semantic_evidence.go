// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	semanticDocumentationObservationsCapability = "semantic_evidence.documentation_observations.list"
	semanticCodeHintsCapability                 = "semantic_evidence.code_hints.list"
)

// SemanticEvidenceHandler exposes opt-in semantic observation and code-hint reads.
type SemanticEvidenceHandler struct {
	Content any
	Profile QueryProfile
}

type semanticEvidenceStore interface {
	semanticEvidence(context.Context, semanticEvidenceFilter) (semanticEvidenceListReadModel, error)
}

type semanticEvidenceFilter struct {
	FactKind             string
	FactID               string
	ScopeID              string
	GenerationID         string
	Repository           string
	TargetKind           string
	TargetID             string
	ServiceID            string
	SourceClass          string
	SourceID             string
	DocumentID           string
	SectionID            string
	RelativePath         string
	EntityID             string
	ProviderProfileID    string
	ProviderKind         string
	PromptVersion        string
	RedactionVersion     string
	ExtractionMode       string
	PolicyState          string
	RedactionState       string
	FreshnessState       string
	AdmissionState       string
	CorroborationState   string
	ObservationType      string
	HintType             string
	RelationshipKind     string
	Query                string
	UpdatedSince         *time.Time
	AllowedScopeIDs      []string
	AllowedRepositoryIDs []string
	Limit                int
	Cursor               string
	Offset               int
}

type semanticEvidenceListReadModel struct {
	Rows       []map[string]any
	NextCursor string
}

// Mount registers semantic evidence routes.
func (h *SemanticEvidenceHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/semantic/documentation-observations", h.listDocumentationObservations)
	mux.HandleFunc("GET /api/v0/semantic/code-hints", h.listCodeHints)
}

func (h *SemanticEvidenceHandler) profile() QueryProfile {
	if h == nil || h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

func (h *SemanticEvidenceHandler) listDocumentationObservations(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, semanticDocumentationObservationsCapability, facts.SemanticDocumentationObservationFactKind, "observations")
}

func (h *SemanticEvidenceHandler) listCodeHints(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, semanticCodeHintsCapability, facts.SemanticCodeHintFactKind, "code_hints")
}

func (h *SemanticEvidenceHandler) list(
	w http.ResponseWriter,
	r *http.Request,
	capability string,
	factKind string,
	responseKey string,
) {
	route := "/api/v0/semantic/documentation-observations"
	if factKind == facts.SemanticCodeHintFactKind {
		route = "/api/v0/semantic/code-hints"
	}
	r, span := startQueryHandlerSpan(r, telemetry.SpanQuerySemanticEvidence, "GET "+route, capability)
	defer span.End()

	if capabilityUnsupported(h.profile(), capability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"semantic evidence routes require durable semantic facts",
			ErrorCodeUnsupportedCapability,
			capability,
			h.profile(),
			requiredProfile(capability),
		)
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
	filter := semanticEvidenceRequestFilter(r, factKind, page, updatedSince)
	if !filter.hasScopeOrFilter() {
		writeSemanticEvidenceError(
			w,
			r,
			http.StatusBadRequest,
			ErrorCodeInvalidArgument,
			"semantic evidence reads require scope_id, repo, provider_profile_id, source, subject, status, freshness, or fact_id filter",
			capability,
		)
		return
	}
	var hasAuthorizedRows bool
	filter, hasAuthorizedRows = semanticEvidenceFilterWithRepositoryAccess(r.Context(), filter)
	if !hasAuthorizedRows {
		WriteSuccess(w, r, http.StatusOK, semanticEvidenceResponse(responseKey, semanticEvidenceListReadModel{}, page), BuildTruthEnvelope(
			h.profile(),
			capability,
			TruthBasisSemanticFacts,
			"resolved from durable semantic evidence facts",
		))
		return
	}
	store, ok := h.semanticStore()
	if !ok {
		writeSemanticEvidenceError(
			w,
			r,
			http.StatusNotImplemented,
			ErrorCodeReadModelUnavailable,
			"semantic evidence routes require the Postgres fact read model",
			capability,
		)
		return
	}
	readModel, err := store.semanticEvidence(r.Context(), filter)
	if err != nil {
		writeSemanticEvidenceError(
			w,
			r,
			http.StatusInternalServerError,
			ErrorCodeInternalError,
			"semantic evidence request failed",
			capability,
		)
		return
	}
	WriteSuccess(w, r, http.StatusOK, semanticEvidenceResponse(responseKey, readModel, page), BuildTruthEnvelope(
		h.profile(),
		capability,
		TruthBasisSemanticFacts,
		"resolved from durable semantic evidence facts",
	))
}

func semanticEvidenceFilterWithRepositoryAccess(
	ctx context.Context,
	filter semanticEvidenceFilter,
) (semanticEvidenceFilter, bool) {
	access := repositoryAccessFilterFromContext(ctx)
	if !access.scoped() {
		return filter, true
	}
	if access.empty() {
		return filter, false
	}
	filter.AllowedRepositoryIDs = append([]string(nil), access.allowedRepositoryIDs...)
	filter.AllowedScopeIDs = append([]string(nil), access.allowedScopeIDs...)
	return filter, true
}

func (h *SemanticEvidenceHandler) semanticStore() (semanticEvidenceStore, bool) {
	if h == nil || h.Content == nil {
		return nil, false
	}
	store, ok := h.Content.(semanticEvidenceStore)
	return store, ok
}

func semanticEvidenceRequestFilter(
	r *http.Request,
	factKind string,
	page documentationPage,
	updatedSince *time.Time,
) semanticEvidenceFilter {
	return semanticEvidenceFilter{
		FactKind:           factKind,
		FactID:             QueryParam(r, "fact_id"),
		ScopeID:            QueryParam(r, "scope_id"),
		GenerationID:       QueryParam(r, "generation_id"),
		Repository:         QueryParam(r, "repo"),
		TargetKind:         QueryParam(r, "target_kind"),
		TargetID:           QueryParam(r, "target_id"),
		ServiceID:          QueryParam(r, "service_id"),
		SourceClass:        QueryParam(r, "source_class"),
		SourceID:           QueryParam(r, "source_id"),
		DocumentID:         QueryParam(r, "document_id"),
		SectionID:          QueryParam(r, "section_id"),
		RelativePath:       QueryParam(r, "relative_path"),
		EntityID:           QueryParam(r, "entity_id"),
		ProviderProfileID:  QueryParam(r, "provider_profile_id"),
		ProviderKind:       QueryParam(r, "provider_kind"),
		PromptVersion:      QueryParam(r, "prompt_version"),
		RedactionVersion:   QueryParam(r, "redaction_version"),
		ExtractionMode:     QueryParam(r, "extraction_mode"),
		PolicyState:        QueryParam(r, "policy_state"),
		RedactionState:     QueryParam(r, "redaction_state"),
		FreshnessState:     QueryParam(r, "freshness_state"),
		AdmissionState:     QueryParam(r, "admission_state"),
		CorroborationState: QueryParam(r, "corroboration_state"),
		ObservationType:    QueryParam(r, "observation_type"),
		HintType:           QueryParam(r, "hint_type"),
		RelationshipKind:   QueryParam(r, "relationship_kind"),
		Query:              QueryParam(r, "q"),
		UpdatedSince:       updatedSince,
		Limit:              page.limit,
		Cursor:             page.cursor,
		Offset:             page.offset,
	}
}

func (f semanticEvidenceFilter) hasScopeOrFilter() bool {
	for _, value := range []string{
		f.FactID, f.ScopeID, f.GenerationID, f.Repository, f.TargetID, f.ServiceID,
		f.SourceClass, f.SourceID, f.DocumentID, f.SectionID, f.RelativePath,
		f.EntityID, f.ProviderProfileID, f.ProviderKind, f.PromptVersion,
		f.RedactionVersion, f.ExtractionMode, f.PolicyState, f.RedactionState,
		f.FreshnessState, f.AdmissionState, f.CorroborationState,
		f.ObservationType, f.HintType, f.RelationshipKind, f.Query,
	} {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return f.UpdatedSince != nil
}

func semanticEvidenceResponse(
	responseKey string,
	readModel semanticEvidenceListReadModel,
	page documentationPage,
) map[string]any {
	rows := readModel.Rows
	if rows == nil {
		rows = []map[string]any{}
	}
	body := map[string]any{
		responseKey: rows,
		"count":     len(rows),
		"limit":     page.limit,
		"truncated": strings.TrimSpace(readModel.NextCursor) != "",
	}
	if readModel.NextCursor != "" {
		body["next_cursor"] = readModel.NextCursor
	}
	return body
}

func writeSemanticEvidenceError(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	code ErrorCode,
	message string,
	capability string,
) {
	if acceptsEnvelope(r) {
		WriteJSON(w, status, ResponseEnvelope{Error: &ErrorEnvelope{
			Code:       code,
			Message:    message,
			Capability: capability,
		}})
		return
	}
	WriteJSON(w, status, map[string]any{
		"error_code": code,
		"message":    message,
		"capability": capability,
	})
}

// semanticEvidence returns sanitized semantic evidence rows from fact_records.
func (cr *ContentReader) semanticEvidence(
	ctx context.Context,
	filter semanticEvidenceFilter,
) (semanticEvidenceListReadModel, error) {
	if cr == nil || cr.db == nil {
		return semanticEvidenceListReadModel{}, nil
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_semantic_evidence"),
			attribute.String("db.sql.table", "fact_records"),
		),
	)
	defer span.End()

	query, args := buildSemanticEvidenceSQL(filter)
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return semanticEvidenceListReadModel{}, fmt.Errorf("query semantic evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	out := make([]map[string]any, 0, limit)
	for rows.Next() {
		raw, err := scanJSONPayload(rows)
		if err != nil {
			span.RecordError(err)
			return semanticEvidenceListReadModel{}, fmt.Errorf("query semantic evidence: %w", err)
		}
		out = append(out, semanticEvidencePublicRow(raw))
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return semanticEvidenceListReadModel{}, fmt.Errorf("query semantic evidence: %w", err)
	}
	nextCursor := ""
	if len(out) > limit {
		out = out[:limit]
		nextCursor = strconv.Itoa(filter.Offset + limit)
	}
	return semanticEvidenceListReadModel{Rows: out, NextCursor: nextCursor}, nil
}
