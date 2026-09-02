// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

const freshnessChangedSinceRoute = "GET /api/v0/freshness/changed-since"

// ChangedSinceReader computes one bounded changed-since delta summary that diffs
// a prior generation's fact set against the current active generation's fact
// set. It is implemented by the Postgres status store and consumed here so the
// handler does not depend on a concrete database driver.
type ChangedSinceReader interface {
	ComputeChangedSinceDelta(context.Context, status.ChangedSinceFilter) (status.ChangedSinceSummary, error)
}

func (h *FreshnessHandler) listChangedSince(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryFreshnessChangedSince,
		freshnessChangedSinceRoute,
		freshnessChangedSinceCapability,
	)
	defer span.End()

	if QueryParam(r, "scope_id") != "" && QueryParam(r, "repository") != "" {
		err := fmt.Errorf("scope_id and repository are mutually exclusive")
		span.RecordError(err)
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if capabilityUnsupported(h.profile(), freshnessChangedSinceCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"changed-since summaries are not supported in this profile",
			ErrorCodeUnsupportedCapability,
			freshnessChangedSinceCapability,
			h.profile(),
			requiredProfile(freshnessChangedSinceCapability),
		)
		return
	}

	filter, ok := h.parseChangedSinceFilter(w, r)
	if !ok {
		return
	}

	if h.ChangedSince == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"changed-since reader is not configured",
			ErrorCodeBackendUnavailable,
			freshnessChangedSinceCapability,
			h.profile(),
			requiredProfile(freshnessChangedSinceCapability),
		)
		return
	}

	// #5167 F-6: this route names ONE repository or scope selector, taken
	// straight from the query string, so the tenant question is not which rows
	// the caller may see but whether it may name this selector at all. The
	// grant is checked BEFORE the read: refusing afterwards would already have
	// pulled another tenant's delta into the process, and onto whatever logs
	// and spans that path emits.
	if !changedSinceSelectorWithinGrant(r.Context(), filter) {
		WriteError(w, http.StatusForbidden, "scope_id or repository is outside this token's grant")
		return
	}

	summary, err := h.ChangedSince.ComputeChangedSinceDelta(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("compute changed-since delta: %v", err))
		return
	}

	// An empty resolved scope means the named scope/repository matched nothing.
	if summary.ScopeID == "" {
		WriteContractError(
			w,
			r,
			http.StatusNotFound,
			changedSinceScopeNotFoundMessage(filter),
			ErrorCodeScopeNotFound,
			freshnessChangedSinceCapability,
			h.profile(),
			requiredProfile(freshnessChangedSinceCapability),
		)
		return
	}

	// The scope resolved but the since reference matched no prior generation.
	if summary.SinceGenerationID == "" && !summary.Unavailable {
		WriteContractError(
			w,
			r,
			http.StatusNotFound,
			changedSinceGenerationNotFoundMessage(filter),
			ErrorCodeNotFound,
			freshnessChangedSinceCapability,
			h.profile(),
			requiredProfile(freshnessChangedSinceCapability),
		)
		return
	}

	span.SetAttributes(changedSinceSpanAttributes(summary)...)

	body := map[string]any{
		"scope_id":                     summary.ScopeID,
		"scope_kind":                   summary.ScopeKind,
		"since_generation_id":          summary.SinceGenerationID,
		"current_active_generation_id": summary.CurrentActiveGenerationID,
		"sample_limit":                 summary.SampleLimit,
		"categories":                   summary.Categories,
		"unavailable":                  summary.Unavailable,
	}
	if summary.UnavailableReason != "" {
		body["unavailable_reason"] = summary.UnavailableReason
	}
	if summary.Repository != "" {
		body["repository"] = summary.Repository
	}
	if summary.SinceObservedAt != "" {
		body["since_observed_at"] = summary.SinceObservedAt
	}
	if summary.CurrentObservedAt != "" {
		body["current_observed_at"] = summary.CurrentObservedAt
	}

	WriteSuccess(w, r, http.StatusOK, body, h.changedSinceTruthEnvelope(summary))
}

func (h *FreshnessHandler) parseChangedSinceFilter(w http.ResponseWriter, r *http.Request) (status.ChangedSinceFilter, bool) {
	filter := status.ChangedSinceFilter{
		ScopeID:           QueryParam(r, "scope_id"),
		Repository:        QueryParam(r, "repository"),
		SinceGenerationID: QueryParam(r, "since_generation_id"),
	}

	if !filter.HasScopeSelector() {
		WriteError(w, http.StatusBadRequest, "scope_id or repository is required")
		return status.ChangedSinceFilter{}, false
	}

	if raw := QueryParam(r, "since_observed_at"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "since_observed_at must be an RFC3339 timestamp")
			return status.ChangedSinceFilter{}, false
		}
		filter.SinceObservedAt = parsed
	}

	if !filter.HasSinceReference() {
		WriteError(w, http.StatusBadRequest, "since_generation_id or since_observed_at is required")
		return status.ChangedSinceFilter{}, false
	}

	limit, ok := h.parseChangedSinceLimit(w, r)
	if !ok {
		return status.ChangedSinceFilter{}, false
	}
	filter.SampleLimit = limit

	return filter.Normalize(), true
}

func (h *FreshnessHandler) parseChangedSinceLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "sample_limit")
	if raw == "" {
		return status.DefaultChangedSinceSampleLimit, true
	}
	limit := QueryParamInt(r, "sample_limit", -1)
	if limit <= 0 || limit > status.MaxChangedSinceSampleLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("sample_limit must be between 1 and %d", status.MaxChangedSinceSampleLimit))
		return 0, false
	}
	return limit, true
}

func (h *FreshnessHandler) changedSinceTruthEnvelope(summary status.ChangedSinceSummary) *TruthEnvelope {
	envelope := BuildTruthEnvelope(
		h.profile(),
		freshnessChangedSinceCapability,
		TruthBasisSemanticFacts,
		"diffed from durable fact_records keyed by (scope_id, generation_id, stable_fact_key); changed-since is persisted fact truth, not graph-materialized correlation",
	)
	switch {
	case summary.Unavailable:
		envelope.Freshness.State = FreshnessUnavailable
		if summary.UnavailableReason == status.ChangedSinceUnavailableRetentionExpired {
			envelope.Freshness.Detail = "the prior generation was pruned by the retention policy, so the changed-since diff is no longer available"
			WithFreshnessCause(envelope, FreshnessCauseRetentionExpired)
		} else {
			envelope.Freshness.Detail = "the scope has no current active generation, so a changed-since diff cannot be computed yet"
			WithFreshnessCause(envelope, FreshnessCausePendingRepoGeneration)
		}
	case summary.Building:
		envelope.Freshness.State = FreshnessBuilding
		envelope.Freshness.Detail = "the scope has a pending generation in flight; the current active generation may change"
		WithFreshnessCause(envelope, FreshnessCausePendingRepoGeneration)
	}
	return envelope
}

func changedSinceScopeNotFoundMessage(filter status.ChangedSinceFilter) string {
	switch {
	case filter.ScopeID != "":
		return fmt.Sprintf("no scope found for scope_id %q", filter.ScopeID)
	case filter.Repository != "":
		return fmt.Sprintf("no repository scope found for repository %q", filter.Repository)
	default:
		return "no scope matched the requested changed-since selector"
	}
}

func changedSinceGenerationNotFoundMessage(filter status.ChangedSinceFilter) string {
	if filter.SinceGenerationID != "" {
		return fmt.Sprintf("no generation %q for the requested scope", filter.SinceGenerationID)
	}
	return "no generation observed at or before the requested since_observed_at for the scope"
}

func changedSinceSpanAttributes(summary status.ChangedSinceSummary) []attribute.KeyValue {
	changed := 0
	for _, category := range summary.Categories {
		changed += category.Counts.Added +
			category.Counts.Updated +
			category.Counts.Retired +
			category.Counts.Superseded
	}
	return []attribute.KeyValue{
		attribute.String(telemetry.SpanAttrChangedSinceScopeID, summary.ScopeID),
		attribute.String(telemetry.SpanAttrChangedSinceSinceGenerationID, summary.SinceGenerationID),
		attribute.String(telemetry.SpanAttrChangedSinceCurrentGenerationID, summary.CurrentActiveGenerationID),
		attribute.Int(telemetry.SpanAttrChangedSinceChangedCount, changed),
		attribute.Bool(telemetry.SpanAttrChangedSinceUnavailable, summary.Unavailable),
	}
}

// changedSinceSelectorWithinGrant reports whether the caller may name the
// selector this filter carries. A shared-key or all-scopes caller always may.
// A scoped caller may only name a repository or scope its grant lists, and an
// empty grant may name nothing -- the same fail-closed posture the operations
// reader takes rather than passing empty grants down to a store.
func changedSinceSelectorWithinGrant(ctx context.Context, filter status.ChangedSinceFilter) bool {
	return freshnessSelectorWithinGrant(ctx, filter.Repository, filter.ScopeID)
}

// freshnessSelectorWithinGrant reports whether the caller may name this
// repository/scope pair. Shared by every freshness route that takes a single
// selector from the query string rather than returning a row set to intersect.
func freshnessSelectorWithinGrant(ctx context.Context, repository, scopeID string) bool {
	access := repositoryAccessFilterFromContext(ctx)
	if !access.Scoped() {
		return true
	}
	if access.Empty() {
		return false
	}
	if repo := strings.TrimSpace(repository); repo != "" {
		if !containsAuthString(access.GrantedRepositoryIDs(), repo) {
			return false
		}
	}
	if scope := strings.TrimSpace(scopeID); scope != "" {
		if !containsAuthString(access.GrantedScopeIDs(), scope) {
			return false
		}
	}
	return true
}
