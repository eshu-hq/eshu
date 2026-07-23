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

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	packageRegistryPackagesCapability         = "package_registry.packages.list"
	packageRegistryVersionsCapability         = "package_registry.versions.list"
	packageRegistryDependenciesCapability     = "package_registry.dependencies.list"
	packageRegistryCorrelationsCapability     = "package_registry.correlations.list"
	packageRegistryDependencyChainsCapability = "package_registry.dependency_chains.list"
	packageRegistryMaxLimit                   = 200
	packageRegistryDependencyReadTimeout      = 10 * time.Second
)

// PackageRegistryHandler exposes graph-backed package registry identity reads.
type PackageRegistryHandler struct {
	Neo4j        GraphQuery
	Content      ContentStore
	Correlations PackageRegistryCorrelationStore
	Aggregates   PackageRegistryAggregateStore
	// CollectorReadiness answers the configured-collector probe for the gated
	// package-registry list tools so an empty page reports not_configured when
	// the package_registry collector is disabled. It is optional: a nil store
	// leaves the collector_readiness envelope off the response.
	CollectorReadiness CollectorListReadinessStore
	Profile            QueryProfile
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
	PURL             string `json:"purl,omitempty"`
	BOMRef           string `json:"bom_ref,omitempty"`
	PackageManager   string `json:"package_manager,omitempty"`
	SourcePath       string `json:"source_path,omitempty"`
	SourceSpecificID string `json:"source_specific_id,omitempty"`
	Visibility       string `json:"visibility,omitempty"`
	SourceConfidence string `json:"source_confidence,omitempty"`
	VersionCount     int    `json:"version_count"`
}

// PackageRegistryVersionResult is one package version identity materialized
// from registry facts.
type PackageRegistryVersionResult struct {
	VersionID      string `json:"version_id"`
	PackageID      string `json:"package_id"`
	Version        string `json:"version,omitempty"`
	PURL           string `json:"purl,omitempty"`
	BOMRef         string `json:"bom_ref,omitempty"`
	PackageManager string `json:"package_manager,omitempty"`
	PublishedAt    string `json:"published_at,omitempty"`
	IsYanked       bool   `json:"is_yanked"`
	IsUnlisted     bool   `json:"is_unlisted"`
	IsDeprecated   bool   `json:"is_deprecated"`
	IsRetracted    bool   `json:"is_retracted"`
}

// PackageRegistryDependencyResult is one package-native dependency edge
// reported by package registry metadata and materialized through the graph.
type PackageRegistryDependencyResult struct {
	DependencyID         string   `json:"dependency_id"`
	SourcePackageID      string   `json:"source_package_id"`
	SourceVersionID      string   `json:"source_version_id"`
	Version              string   `json:"version,omitempty"`
	DependencyPackageID  string   `json:"dependency_package_id"`
	DependencyEcosystem  string   `json:"dependency_ecosystem,omitempty"`
	DependencyRegistry   string   `json:"dependency_registry,omitempty"`
	DependencyNamespace  string   `json:"dependency_namespace,omitempty"`
	DependencyNormalized string   `json:"dependency_normalized,omitempty"`
	DependencyPURL       string   `json:"dependency_purl,omitempty"`
	DependencyBOMRef     string   `json:"dependency_bom_ref,omitempty"`
	DependencyManager    string   `json:"dependency_manager,omitempty"`
	DependencyRange      string   `json:"dependency_range,omitempty"`
	DependencyType       string   `json:"dependency_type,omitempty"`
	TargetFramework      string   `json:"target_framework,omitempty"`
	Marker               string   `json:"marker,omitempty"`
	Optional             bool     `json:"optional"`
	Excluded             bool     `json:"excluded"`
	SourceConfidence     string   `json:"source_confidence,omitempty"`
	CollectorKind        string   `json:"collector_kind,omitempty"`
	CollectorInstanceID  string   `json:"collector_instance_id,omitempty"`
	CorrelationAnchors   []string `json:"correlation_anchors,omitempty"`
}

// Mount registers package registry query routes.
func (h *PackageRegistryHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/package-registry/packages", h.listPackages)
	mux.HandleFunc("GET /api/v0/package-registry/versions", h.listVersions)
	mux.HandleFunc("GET /api/v0/package-registry/dependencies", h.listDependencies)
	mux.HandleFunc("GET /api/v0/package-registry/correlations", h.listCorrelations)
	mux.HandleFunc("GET /api/v0/package-registry/dependency-chains", h.listDependencyChains)
	h.packageRegistryAggregateRoutes(mux)
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
	gate, handled := packageRegistryPackagesGate(w, r, h, span, packageID, ecosystem, name, limit)
	if handled {
		return
	}
	packageID = gate.packageID
	redactSourcePath := gate.redactSourcePath
	// nameAnchorRedactByID is set only for the scoped name+ecosystem branch:
	// normalized_name is not a unique package identity within an ecosystem,
	// so the read below returns EVERY package sharing the anchor and this
	// map (built by gating each candidate individually) decides, per row,
	// whether it is allowed and whether its source_path must be redacted.
	nameAnchorRedactByID := gate.nameAnchorRedactByID

	var cypher string
	var params map[string]any
	if gate.useScopedEcosystemCypher {
		cypher, params = packageRegistryPackagesScopedEcosystemCypher(ecosystem, limit+1)
	} else {
		cypher, params = packageRegistryPackagesCypher(packageID, ecosystem, name, limit+1)
	}
	rows, err := h.Neo4j.Run(r.Context(), cypher, params)
	if err != nil {
		if WriteGraphReadError(w, r, err, packageRegistryPackagesCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	results := make([]PackageRegistryPackageResult, 0, len(rows))
	identityIssues := make([]PackageRegistryIdentityIssue, 0)
	// nameAnchorCandidatesTruncated forces truncated=true even when the
	// returned page fits under limit: it means the name+ecosystem anchor
	// itself was capped (packageRegistryNameAnchorCandidateLimit), so this
	// response cannot be presented as a complete candidate set.
	truncated := len(rows) > limit || gate.nameAnchorCandidatesTruncated
	for _, row := range rows {
		result, issue := packageRegistryPackageResultFromRow(row)
		if issue != nil {
			if nameAnchorRedactByID != nil {
				// The name+ecosystem branch cannot look up this row's grant
				// status (packageRegistryPackageResultFromRow already failed
				// to extract a package_id, so there is no key for
				// nameAnchorRedactByID): fail closed on every metadata field
				// this issue carries, not just source_path. A malformed row
				// sharing the requested name could belong to a
				// private/unknown package the caller has no grant for, and
				// registry/namespace/purl/package_manager/source_specific_id/
				// source_confidence/version_count are as much a metadata leak
				// as source_path is.
				redactPackageRegistryIdentityIssueMetadata(issue)
			} else if redactSourcePath {
				issue.SourcePath = ""
			}
			identityIssues = append(identityIssues, *issue)
			continue
		}
		rowRedact := redactSourcePath
		if nameAnchorRedactByID != nil {
			redact, allowed := nameAnchorRedactByID[result.PackageID]
			if !allowed {
				// A name+ecosystem sibling the caller has no grant for
				// (private/unknown, no correlation proof). Drop it silently
				// -- same treatment as an ungranted package anywhere else in
				// this handler; the caller only ever sees rows it is allowed
				// to see.
				continue
			}
			rowRedact = redact
		}
		if len(results) == limit {
			truncated = true
			continue
		}
		if rowRedact {
			result.SourcePath = ""
		}
		results = append(results, result)
	}
	if err := h.attachPackageVersionCounts(r.Context(), results); err != nil {
		if WriteGraphReadError(w, r, err, packageRegistryPackagesCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	body := map[string]any{
		"packages":        results,
		"identity_issues": identityIssues,
		"count":           len(results),
		"limit":           limit,
		"truncated":       truncated,
	}
	attachCollectorListReadiness(r.Context(), body, h.CollectorReadiness, scope.CollectorPackageRegistry, len(results), truncated)
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
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
	if packageRegistryVersionsGate(w, r, h, span, packageID, limit) {
		return
	}

	rows, err := h.Neo4j.Run(r.Context(), packageRegistryVersionsCypher(), map[string]any{
		"package_id": packageID,
		"limit":      limit + 1,
	})
	if err != nil {
		if WriteGraphReadError(w, r, err, packageRegistryVersionsCapability) {
			return
		}
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
			VersionID:      StringVal(row, "version_id"),
			PackageID:      StringVal(row, "package_id"),
			Version:        StringVal(row, "version"),
			PURL:           StringVal(row, "purl"),
			BOMRef:         StringVal(row, "bom_ref"),
			PackageManager: StringVal(row, "package_manager"),
			PublishedAt:    packageRegistryTimestampVal(row, "published_at"),
			IsYanked:       BoolVal(row, "is_yanked"),
			IsUnlisted:     BoolVal(row, "is_unlisted"),
			IsDeprecated:   BoolVal(row, "is_deprecated"),
			IsRetracted:    BoolVal(row, "is_retracted"),
		})
	}
	body := map[string]any{
		"versions":  results,
		"count":     len(results),
		"limit":     limit,
		"truncated": truncated,
	}
	attachCollectorListReadiness(r.Context(), body, h.CollectorReadiness, scope.CollectorPackageRegistry, len(results), truncated)
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		packageRegistryVersionsCapability,
		TruthBasisAuthoritativeGraph,
		"resolved from package registry package-version identity graph nodes",
	))
}

// attachPackageVersionCounts resolves HAS_VERSION counts for one page of
// packages as a separate, scoped query and zero-fills any package uid absent
// from the result. This is deliberately not folded back into
// packageRegistryPackagesCypher's OPTIONAL MATCH + count(v): on the pinned
// NornicDB backend that composition silently collapses every zero-version
// package out of the result set instead of returning it with version_count
// 0 (see docs/public/reference/nornicdb-pitfalls.md). Skips the round trip
// entirely when the page is empty.
func (h *PackageRegistryHandler) attachPackageVersionCounts(
	ctx context.Context,
	results []PackageRegistryPackageResult,
) error {
	if len(results) == 0 {
		return nil
	}
	packageIDs := make([]string, len(results))
	for i, result := range results {
		packageIDs[i] = result.PackageID
	}
	cypher, params := packageRegistryVersionCountsCypher(packageIDs)
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return err
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[StringVal(row, "package_id")] = IntVal(row, "version_count")
	}
	for i := range results {
		results[i].VersionCount = counts[results[i].PackageID]
	}
	return nil
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
