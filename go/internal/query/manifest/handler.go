// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package manifest

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

const (
	manifestDependenciesMaxLimit     = 200
	manifestDependenciesDefaultLimit = 50
	manifestDependenciesReadTimeout  = 10 * time.Second
)

// Handler exposes the bounded repository-scoped declared manifest
// dependency inventory: package name, declared version or range, manifest
// file, ecosystem, and the dev-or-runtime signal when the fact says so. It
// reads the active-generation git content-entity facts through the same
// delta-overlay snapshot the reducer uses, and never asserts resolved
// versions or cross-repository ownership.
type Handler struct {
	Store       ManifestDependencyStore
	Profile     querycontract.QueryProfile
	Instruments *telemetry.Instruments
}

// Mount registers the repository manifest dependency route.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/repository/manifest-dependencies", h.listManifestDependencies)
}

func (h *Handler) profile() querycontract.QueryProfile {
	if h == nil || h.Profile == "" {
		return querycontract.ProfileProduction
	}
	return h.Profile
}

func (h *Handler) listManifestDependencies(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryManifestDependencies,
		"GET /api/v0/repository/manifest-dependencies",
		Capability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), Capability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"repository manifest dependencies require the authoritative store profile",
			querycontract.ErrorCodeUnsupportedCapability,
			Capability,
			h.profile(),
			querycontract.RequiredProfile(Capability),
		)
		return
	}

	limit, ok := manifestDependenciesLimit(w, r)
	if !ok {
		return
	}
	repositoryID := querycontract.QueryParam(r, "repository_id")
	if repositoryID == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "repository_id is required")
		return
	}
	afterObservedAt, afterFactID, ok := manifestDependenciesCursor(w, r)
	if !ok {
		return
	}
	includeDev, ok := manifestDependenciesIncludeDev(w, r)
	if !ok {
		return
	}

	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() || (access.Scoped() && !access.AllowsRepositoryID(repositoryID)) {
		h.writeEmptyManifestDependenciesPage(w, r, repositoryID, limit)
		return
	}

	if h.Store == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"repository manifest dependencies require the fact store",
			querycontract.ErrorCodeBackendUnavailable,
			Capability,
			h.profile(),
			querycontract.RequiredProfile(Capability),
		)
		return
	}

	queryCtx, cancel := context.WithTimeout(r.Context(), manifestDependenciesReadTimeout)
	defer cancel()

	startedAt := time.Now()
	page, refused, err := h.Store.ListRepositoryManifestDependencyFacts(
		queryCtx, repositoryID, afterObservedAt, afterFactID, limit+1, includeDev)
	h.recordDuration(queryCtx, startedAt)
	if err != nil {
		h.recordError(queryCtx)
		if refused {
			span.SetAttributes(attribute.Bool("eshu.manifest_dependencies_refused", true))
			querycontract.WriteContractError(
				w,
				r,
				http.StatusServiceUnavailable,
				manifestDependenciesBaseRefusedMessage,
				querycontract.ErrorCodeBackendUnavailable,
				Capability,
				h.profile(),
				querycontract.RequiredProfile(Capability),
			)
			return
		}
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, manifestDependenciesReadFailedMessage)
		return
	}

	truncated := len(page.Rows) > limit
	rows := page.Rows
	if truncated {
		rows = rows[:limit]
	}
	span.SetAttributes(
		attribute.String("eshu.repository_id", repositoryID),
		attribute.Int("eshu.manifest_dependencies_count", len(rows)),
		attribute.Bool("eshu.manifest_dependencies_truncated", truncated),
	)

	body := map[string]any{
		"repository_id":            repositoryID,
		"dependencies":             rows,
		"count":                    len(rows),
		"limit":                    limit,
		"truncated":                truncated,
		"include_dev_dependencies": includeDev,
		"freshness":                page.Freshness,
	}
	if truncated && len(rows) > 0 {
		last := rows[len(rows)-1]
		body["next_cursor"] = map[string]string{
			"after_observed_at": last.ObservedAt,
			"after_fact_id":     last.FactID,
		}
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		Capability,
		querycontract.TruthBasisSemanticFacts,
		"declared manifest dependencies from active-generation git content-entity facts; ranges are declared, not resolved, and no ownership beyond the scope anchor is asserted",
	))
}

// writeEmptyManifestDependenciesPage answers a scoped caller with no grant,
// or with no grant for the requested repository, the same empty shape a
// granted caller of an empty repository receives — without touching the
// store, so a private or nonexistent repository is indistinguishable.
func (h *Handler) writeEmptyManifestDependenciesPage(w http.ResponseWriter, r *http.Request, repositoryID string, limit int) {
	body := map[string]any{
		"repository_id": repositoryID,
		"dependencies":  []ManifestDependencyRow{},
		"count":         0,
		"limit":         limit,
		"truncated":     false,
		"freshness": ManifestDependencyFreshness{
			FreshnessState: string(querycontract.FreshnessUnavailable),
		},
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		Capability,
		querycontract.TruthBasisNoBackendRead,
		querycontract.ReasonEmptyGrantNoBackendRead,
	))
}

// manifestDependenciesLimit resolves the bounded limit parameter, defaulting
// to 50 when absent and rejecting values outside 1..200.
func manifestDependenciesLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return manifestDependenciesDefaultLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > manifestDependenciesMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", manifestDependenciesMaxLimit))
		return 0, false
	}
	return limit, true
}

// manifestDependenciesCursor resolves the (observed_at, fact_id) keyset
// cursor, accepting the pair together or neither. The observed_at component
// is RFC 3339, matching the next_cursor the handler emits.
func manifestDependenciesCursor(w http.ResponseWriter, r *http.Request) (*time.Time, string, bool) {
	afterObservedAt := querycontract.QueryParam(r, "after_observed_at")
	afterFactID := querycontract.QueryParam(r, "after_fact_id")
	if (afterObservedAt == "") != (afterFactID == "") {
		querycontract.WriteError(w, http.StatusBadRequest, "after_observed_at and after_fact_id must be provided together")
		return nil, "", false
	}
	if afterObservedAt == "" {
		return nil, "", true
	}
	observedAt, err := time.Parse(time.RFC3339Nano, afterObservedAt)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, "after_observed_at must be RFC 3339")
		return nil, "", false
	}
	observedAt = observedAt.UTC()
	return &observedAt, afterFactID, true
}

// manifestDependenciesIncludeDev resolves the dev-inclusion opt-in flag,
// defaulting to false (runtime dependencies only). An unparseable value is a
// 400, never a silent default.
func manifestDependenciesIncludeDev(w http.ResponseWriter, r *http.Request) (bool, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("include_dev_dependencies"))
	if raw == "" {
		return false, true
	}
	includeDev, err := strconv.ParseBool(raw)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, "include_dev_dependencies must be true or false")
		return false, false
	}
	return includeDev, true
}

func (h *Handler) recordDuration(ctx context.Context, startedAt time.Time) {
	if h == nil || h.Instruments == nil || h.Instruments.ManifestDependencyListDuration == nil {
		return
	}
	h.Instruments.ManifestDependencyListDuration.Record(
		ctx,
		time.Since(startedAt).Seconds(),
	)
}

func (h *Handler) recordError(ctx context.Context) {
	if h == nil || h.Instruments == nil || h.Instruments.ManifestDependencyListErrors == nil {
		return
	}
	h.Instruments.ManifestDependencyListErrors.Add(ctx, 1)
}
