package query

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	packageRegistryPackagesCapability = "package_registry.packages.list"
	packageRegistryVersionsCapability = "package_registry.versions.list"
	packageRegistryMaxLimit           = 200
)

// PackageRegistryHandler exposes graph-backed package registry identity reads.
type PackageRegistryHandler struct {
	Neo4j   GraphQuery
	Profile QueryProfile
}

// PackageRegistryPackageResult is one package identity materialized from
// registry facts. Repository ownership is intentionally absent until reducer
// correlation admits it.
type PackageRegistryPackageResult struct {
	PackageID        string `json:"package_id"`
	Ecosystem        string `json:"ecosystem,omitempty"`
	Registry         string `json:"registry,omitempty"`
	Namespace        string `json:"namespace,omitempty"`
	NormalizedName   string `json:"normalized_name,omitempty"`
	Visibility       string `json:"visibility,omitempty"`
	SourceConfidence string `json:"source_confidence,omitempty"`
	VersionCount     int    `json:"version_count"`
}

// PackageRegistryVersionResult is one package version identity materialized
// from registry facts.
type PackageRegistryVersionResult struct {
	VersionID    string `json:"version_id"`
	PackageID    string `json:"package_id"`
	Version      string `json:"version,omitempty"`
	PublishedAt  string `json:"published_at,omitempty"`
	IsYanked     bool   `json:"is_yanked"`
	IsUnlisted   bool   `json:"is_unlisted"`
	IsDeprecated bool   `json:"is_deprecated"`
	IsRetracted  bool   `json:"is_retracted"`
}

// Mount registers package registry query routes.
func (h *PackageRegistryHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/package-registry/packages", h.listPackages)
	mux.HandleFunc("GET /api/v0/package-registry/versions", h.listVersions)
}

func (h *PackageRegistryHandler) profile() QueryProfile {
	if h == nil || h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

func (h *PackageRegistryHandler) listPackages(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryPackageRegistryPackages,
		"GET /api/v0/package-registry/packages",
		packageRegistryPackagesCapability,
	)
	defer span.End()

	if h.unsupported(w, r, packageRegistryPackagesCapability) {
		return
	}
	limit, ok := requiredPackageRegistryLimit(w, r)
	if !ok {
		return
	}
	packageID := QueryParam(r, "package_id")
	ecosystem := QueryParam(r, "ecosystem")
	name := QueryParam(r, "name")
	if packageID == "" && name != "" && ecosystem == "" {
		WriteError(w, http.StatusBadRequest, "ecosystem is required when name is provided")
		return
	}
	if packageID == "" && ecosystem == "" {
		WriteError(w, http.StatusBadRequest, "package_id or ecosystem is required")
		return
	}
	if h.Neo4j == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"package registry queries require the authoritative graph",
			ErrorCodeBackendUnavailable,
			packageRegistryPackagesCapability,
			h.profile(),
			requiredProfile(packageRegistryPackagesCapability),
		)
		return
	}

	cypher, params := packageRegistryPackagesCypher(packageID, ecosystem, name, limit+1)
	rows, err := h.Neo4j.Run(r.Context(), cypher, params)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]PackageRegistryPackageResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, PackageRegistryPackageResult{
			PackageID:        StringVal(row, "package_id"),
			Ecosystem:        StringVal(row, "ecosystem"),
			Registry:         StringVal(row, "registry"),
			Namespace:        StringVal(row, "namespace"),
			NormalizedName:   StringVal(row, "normalized_name"),
			Visibility:       StringVal(row, "visibility"),
			SourceConfidence: StringVal(row, "source_confidence"),
			VersionCount:     IntVal(row, "version_count"),
		})
	}
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"packages":  results,
		"count":     len(results),
		"limit":     limit,
		"truncated": truncated,
	}, BuildTruthEnvelope(
		h.profile(),
		packageRegistryPackagesCapability,
		TruthBasisAuthoritativeGraph,
		"resolved from package registry package identity graph nodes",
	))
}

func (h *PackageRegistryHandler) listVersions(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryPackageRegistryVersions,
		"GET /api/v0/package-registry/versions",
		packageRegistryVersionsCapability,
	)
	defer span.End()

	if h.unsupported(w, r, packageRegistryVersionsCapability) {
		return
	}
	limit, ok := requiredPackageRegistryLimit(w, r)
	if !ok {
		return
	}
	packageID := QueryParam(r, "package_id")
	if packageID == "" {
		WriteError(w, http.StatusBadRequest, "package_id is required")
		return
	}
	if h.Neo4j == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"package registry version queries require the authoritative graph",
			ErrorCodeBackendUnavailable,
			packageRegistryVersionsCapability,
			h.profile(),
			requiredProfile(packageRegistryVersionsCapability),
		)
		return
	}

	rows, err := h.Neo4j.Run(r.Context(), packageRegistryVersionsCypher(), map[string]any{
		"package_id": packageID,
		"limit":      limit + 1,
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]PackageRegistryVersionResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, PackageRegistryVersionResult{
			VersionID:    StringVal(row, "version_id"),
			PackageID:    StringVal(row, "package_id"),
			Version:      StringVal(row, "version"),
			PublishedAt:  packageRegistryTimestampVal(row, "published_at"),
			IsYanked:     BoolVal(row, "is_yanked"),
			IsUnlisted:   BoolVal(row, "is_unlisted"),
			IsDeprecated: BoolVal(row, "is_deprecated"),
			IsRetracted:  BoolVal(row, "is_retracted"),
		})
	}
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"versions":  results,
		"count":     len(results),
		"limit":     limit,
		"truncated": truncated,
	}, BuildTruthEnvelope(
		h.profile(),
		packageRegistryVersionsCapability,
		TruthBasisAuthoritativeGraph,
		"resolved from package registry package-version identity graph nodes",
	))
}

func (h *PackageRegistryHandler) unsupported(w http.ResponseWriter, r *http.Request, capability string) bool {
	if capabilityUnsupported(h.profile(), capability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"package registry queries require authoritative graph mode",
			ErrorCodeUnsupportedCapability,
			capability,
			h.profile(),
			requiredProfile(capability),
		)
		return true
	}
	return false
}

func packageRegistryTimestampVal(row map[string]any, key string) string {
	value, ok := row[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case time.Time:
		if typed.IsZero() {
			return ""
		}
		return typed.UTC().Format(time.RFC3339)
	case interface{ Time() time.Time }:
		timestamp := typed.Time()
		if timestamp.IsZero() {
			return ""
		}
		return timestamp.UTC().Format(time.RFC3339)
	default:
		return StringVal(row, key)
	}
}

func requiredPackageRegistryLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > packageRegistryMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", packageRegistryMaxLimit))
		return 0, false
	}
	return limit, true
}

func packageRegistryPackagesCypher(packageID, ecosystem, name string, limit int) (string, map[string]any) {
	params := map[string]any{"limit": limit}
	var match string
	switch {
	case packageID != "":
		match = "MATCH (p:Package {uid: $package_id})"
		params["package_id"] = packageID
	case name != "":
		match = "MATCH (p:Package {ecosystem: $ecosystem, normalized_name: $name})"
		params["ecosystem"] = ecosystem
		params["name"] = name
	default:
		match = "MATCH (p:Package {ecosystem: $ecosystem})"
		params["ecosystem"] = ecosystem
	}
	return match + `
OPTIONAL MATCH (p)-[:HAS_VERSION]->(v:PackageVersion)
WITH p, count(v) AS version_count
RETURN p.uid AS package_id,
       p.ecosystem AS ecosystem,
       p.registry AS registry,
       p.namespace AS namespace,
       p.normalized_name AS normalized_name,
       p.visibility AS visibility,
       p.source_confidence AS source_confidence,
       version_count
ORDER BY p.ecosystem, p.normalized_name, p.uid
LIMIT $limit`, params
}

func packageRegistryVersionsCypher() string {
	return `MATCH (p:Package {uid: $package_id})-[:HAS_VERSION]->(v:PackageVersion)
RETURN v.uid AS version_id,
       v.package_id AS package_id,
       v.version AS version,
       v.published_at AS published_at,
       coalesce(v.is_yanked, false) AS is_yanked,
       coalesce(v.is_unlisted, false) AS is_unlisted,
       coalesce(v.is_deprecated, false) AS is_deprecated,
       coalesce(v.is_retracted, false) AS is_retracted
ORDER BY v.version, v.uid
LIMIT $limit`
}
