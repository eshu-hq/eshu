// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dependency

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	dependenciesMaxLimit     = 200
	dependenciesDefaultLimit = 50
	dependenciesReadTimeout  = 10 * time.Second

	dependencyDirectionForward = "forward"
	dependencyDirectionReverse = "reverse"
)

// Handler exposes a bounded, graph-backed package dependency
// inventory: a forward view ("what does package X depend on") and a reverse
// view ("who depends on package X"). It reads the package-native dependency
// chain through the authoritative graph and never returns repository ownership
// truth, which remains a reducer correlation concern.
type Handler struct {
	Neo4j       querycontract.GraphQuery
	Profile     querycontract.QueryProfile
	Instruments *telemetry.Instruments
}

// Row is one dependency edge in the inventory. For the forward view
// the related package is the dependency target; for the reverse view it is the
// dependent package that declared the dependency. Identity fields are absent
// when the source graph did not materialize a stable identity for that node.
type Row struct {
	Direction        string `json:"direction"`
	AnchorPackageID  string `json:"anchor_package_id,omitempty"`
	AnchorPackage    string `json:"anchor_package,omitempty"`
	AnchorEcosystem  string `json:"anchor_ecosystem,omitempty"`
	DeclaringVersion string `json:"declaring_version,omitempty"`
	RelatedPackageID string `json:"related_package_id,omitempty"`
	RelatedPackage   string `json:"related_package,omitempty"`
	RelatedEcosystem string `json:"related_ecosystem,omitempty"`
	DependencyRange  string `json:"dependency_range,omitempty"`
	DependencyType   string `json:"dependency_type,omitempty"`
	Optional         bool   `json:"optional"`
	EdgeID           string `json:"edge_id,omitempty"`
}

// Mount registers the dependency inventory route.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/dependencies", h.listDependencies)
}

func (h *Handler) profile() querycontract.QueryProfile {
	if h == nil || h.Profile == "" {
		return querycontract.ProfileProduction
	}
	return h.Profile
}

func (h *Handler) listDependencies(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryDependencies,
		"GET /api/v0/dependencies",
		Capability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), Capability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"dependency inventory requires authoritative graph mode",
			querycontract.ErrorCodeUnsupportedCapability,
			Capability,
			h.profile(),
			querycontract.RequiredProfile(Capability),
		)
		return
	}

	direction, ok := dependencyDirection(w, r)
	if !ok {
		return
	}
	limit, ok := dependenciesLimit(w, r)
	if !ok {
		return
	}
	pkg := querycontract.QueryParam(r, "package")
	ecosystem := querycontract.QueryParam(r, "ecosystem")
	if direction == dependencyDirectionReverse && pkg == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "package is required for direction=reverse")
		return
	}
	afterName := querycontract.QueryParam(r, "after_name")
	afterEdge := querycontract.QueryParam(r, "after_edge")
	if (afterName == "") != (afterEdge == "") {
		querycontract.WriteError(w, http.StatusBadRequest, "after_name and after_edge must be provided together")
		return
	}
	span.SetAttributes(attribute.String("eshu.dependency_direction", direction))

	if h.Neo4j == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"dependency inventory requires the authoritative graph",
			querycontract.ErrorCodeBackendUnavailable,
			Capability,
			h.profile(),
			querycontract.RequiredProfile(Capability),
		)
		return
	}

	cypher, params := dependenciesCypher(direction, ecosystem, pkg, afterName, afterEdge, limit+1)
	queryCtx, cancel := context.WithTimeout(r.Context(), dependenciesReadTimeout)
	defer cancel()

	startedAt := time.Now()
	rows, err := h.Neo4j.Run(queryCtx, cypher, params)
	h.recordDuration(queryCtx, direction, startedAt)
	if err != nil {
		h.recordError(queryCtx, direction)
		if querycontract.WriteGraphReadError(w, r, err, Capability) {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]Row, 0, len(rows))
	var lastCursorName, lastEdgeID string
	for _, row := range rows {
		results = append(results, Row{
			Direction:        querycontract.StringVal(row, "direction"),
			AnchorPackageID:  querycontract.StringVal(row, "anchor_package_id"),
			AnchorPackage:    querycontract.StringVal(row, "anchor_package"),
			AnchorEcosystem:  querycontract.StringVal(row, "anchor_ecosystem"),
			DeclaringVersion: querycontract.StringVal(row, "declaring_version"),
			RelatedPackageID: querycontract.StringVal(row, "related_package_id"),
			RelatedPackage:   querycontract.StringVal(row, "related_package"),
			RelatedEcosystem: querycontract.StringVal(row, "related_ecosystem"),
			DependencyRange:  querycontract.StringVal(row, "dependency_range"),
			DependencyType:   querycontract.StringVal(row, "dependency_type"),
			Optional:         querycontract.BoolVal(row, "optional"),
			EdgeID:           querycontract.StringVal(row, "edge_id"),
		})
		lastCursorName = querycontract.StringVal(row, "cursor_name")
		lastEdgeID = querycontract.StringVal(row, "edge_id")
	}

	body := map[string]any{
		"dependencies": results,
		"direction":    direction,
		"count":        len(results),
		"limit":        limit,
		"truncated":    truncated,
	}
	if truncated && lastEdgeID != "" {
		body["next_cursor"] = map[string]string{
			"after_name": lastCursorName,
			"after_edge": lastEdgeID,
		}
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		Capability,
		querycontract.TruthBasisAuthoritativeGraph,
		"resolved from the package-native dependency graph; repository and service ownership remain reducer correlation concerns and are not asserted here",
	))
}

// dependencyDirection resolves the bounded direction parameter, defaulting to
// forward when absent and rejecting any other value.
func dependencyDirection(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := strings.ToLower(querycontract.QueryParam(r, "direction"))
	switch raw {
	case "":
		return dependencyDirectionForward, true
	case dependencyDirectionForward, dependencyDirectionReverse:
		return raw, true
	default:
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("direction must be %q or %q", dependencyDirectionForward, dependencyDirectionReverse))
		return "", false
	}
}

// dependenciesLimit resolves the bounded limit parameter, defaulting to 50 when
// absent and rejecting values outside 1..200.
func dependenciesLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return dependenciesDefaultLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > dependenciesMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", dependenciesMaxLimit))
		return 0, false
	}
	return limit, true
}

func (h *Handler) recordDuration(ctx context.Context, direction string, startedAt time.Time) {
	if h == nil || h.Instruments == nil || h.Instruments.DependencyListDuration == nil {
		return
	}
	h.Instruments.DependencyListDuration.Record(
		ctx,
		time.Since(startedAt).Seconds(),
		metric.WithAttributes(attribute.String("direction", direction)),
	)
}

func (h *Handler) recordError(ctx context.Context, direction string) {
	if h == nil || h.Instruments == nil || h.Instruments.DependencyListErrors == nil {
		return
	}
	h.Instruments.DependencyListErrors.Add(
		ctx,
		1,
		metric.WithAttributes(attribute.String("direction", direction)),
	)
}
