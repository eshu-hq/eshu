package query

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	factSchemaVersionListCapability   = "fact_schema_version.list"
	factSchemaVersionDetailCapability = "fact_schema_version.detail"
	factSchemaVersionSchemaVersion    = "eshu.fact_schema_version.v1"
	factSchemaVersionDefaultLimit     = 200
	factSchemaVersionMaxLimit         = 500
	factSchemaVersionTruthReason      = "resolved from the static core fact-schema-version registry; advisory and does not move code"
)

// FactSchemaVersionHandler exposes the core fact-schema-version compatibility
// registry over the query API so API, MCP, and CLI clients can read which schema
// version a core consumer supports for each fact kind and classify a collector's
// emitted version. The data is the static in-binary registry from
// go/internal/facts; the handler reads no runtime, graph, or registry state.
type FactSchemaVersionHandler struct {
	Profile QueryProfile
}

// FactSchemaVersionEntry is one core fact kind and the schema version a core
// consumer currently supports for it.
type FactSchemaVersionEntry struct {
	FactKind      string `json:"fact_kind"`
	SchemaVersion string `json:"schema_version"`
}

// FactSchemaVersionListResponse is the bounded list of core fact-kind schema
// versions.
type FactSchemaVersionListResponse struct {
	SchemaVersion      string                   `json:"schema_version"`
	Status             string                   `json:"status"`
	FactSchemaVersions []FactSchemaVersionEntry `json:"fact_schema_versions"`
	Count              int                      `json:"count"`
	TotalCount         int                      `json:"total_count"`
	Limit              int                      `json:"limit"`
	Truncated          bool                     `json:"truncated"`
}

// FactSchemaVersionDetailResponse is the schema version for one core fact kind,
// plus an optional compatibility classification of a candidate version.
type FactSchemaVersionDetailResponse struct {
	SchemaVersion    string `json:"schema_version"`
	Status           string `json:"status"`
	FactKind         string `json:"fact_kind"`
	SupportedVersion string `json:"supported_version"`
	Candidate        string `json:"candidate,omitempty"`
	Compatibility    string `json:"compatibility,omitempty"`
}

// Mount registers the fact-schema-version routes.
func (h *FactSchemaVersionHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/fact-schema-versions", h.list)
	mux.HandleFunc("GET /api/v0/fact-schema-versions/{fact_kind}", h.detail)
}

func (h *FactSchemaVersionHandler) list(w http.ResponseWriter, r *http.Request) {
	limit, ok := h.limit(w, r)
	if !ok {
		return
	}
	entries := factSchemaVersionEntries()
	totalCount := len(entries)
	truncated := totalCount > limit
	if truncated {
		entries = entries[:limit]
	}
	response := FactSchemaVersionListResponse{
		SchemaVersion:      factSchemaVersionSchemaVersion,
		Status:             "available",
		FactSchemaVersions: entries,
		Count:              len(entries),
		TotalCount:         totalCount,
		Limit:              limit,
		Truncated:          truncated,
	}
	WriteSuccess(w, r, http.StatusOK, response, h.truth(factSchemaVersionListCapability))
}

func (h *FactSchemaVersionHandler) detail(w http.ResponseWriter, r *http.Request) {
	factKind := strings.TrimSpace(r.PathValue("fact_kind"))
	if factKind == "" {
		WriteContractError(
			w, r, http.StatusBadRequest,
			"fact_kind is required",
			ErrorCodeInvalidArgument,
			factSchemaVersionDetailCapability,
			h.profile(), h.profile(),
		)
		return
	}
	supported, ok := facts.SchemaVersion(factKind)
	if !ok {
		WriteContractError(
			w, r, http.StatusNotFound,
			"fact kind is not a core-owned fact kind with a registered schema version",
			ErrorCodeNotFound,
			factSchemaVersionDetailCapability,
			h.profile(), h.profile(),
		)
		return
	}
	response := FactSchemaVersionDetailResponse{
		SchemaVersion:    factSchemaVersionSchemaVersion,
		Status:           "available",
		FactKind:         factKind,
		SupportedVersion: supported,
	}
	if candidate := strings.TrimSpace(QueryParam(r, "candidate")); candidate != "" {
		response.Candidate = candidate
		response.Compatibility = string(facts.ClassifySchemaVersion(factKind, candidate))
	}
	WriteSuccess(w, r, http.StatusOK, response, h.truth(factSchemaVersionDetailCapability))
}

func (h *FactSchemaVersionHandler) limit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(QueryParam(r, "limit"))
	if raw == "" {
		return factSchemaVersionDefaultLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > factSchemaVersionMaxLimit {
		WriteContractError(
			w, r, http.StatusBadRequest,
			fmt.Sprintf("limit must be between 1 and %d", factSchemaVersionMaxLimit),
			ErrorCodeInvalidArgument,
			factSchemaVersionListCapability,
			h.profile(), h.profile(),
		)
		return 0, false
	}
	return limit, true
}

// factSchemaVersionEntries returns the core fact-kind to supported-version
// registry as a deterministically ordered slice.
func factSchemaVersionEntries() []FactSchemaVersionEntry {
	registry := facts.SupportedSchemaVersions()
	entries := make([]FactSchemaVersionEntry, 0, len(registry))
	for kind, version := range registry {
		entries = append(entries, FactSchemaVersionEntry{FactKind: kind, SchemaVersion: version})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].FactKind < entries[j].FactKind })
	return entries
}

func (h *FactSchemaVersionHandler) truth(capability string) *TruthEnvelope {
	return BuildTruthEnvelope(
		h.profile(),
		capability,
		TruthBasisRuntimeState,
		factSchemaVersionTruthReason,
	)
}

func (h *FactSchemaVersionHandler) profile() QueryProfile {
	if h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}
