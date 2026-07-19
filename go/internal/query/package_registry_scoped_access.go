// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// packageRegistryVisibilityPublic is the closed-enum "public" value emitted
// by the package-registry collector's parseVisibility
// (go/internal/collector/packageregistry/metadata_parser_helpers.go).
// Anything else -- "private", "unknown", or a missing/empty value for a
// package that does not exist -- is treated as not-provably-public and falls
// to the correlation-grant probe.
const packageRegistryVisibilityPublic = "public"

// packageRegistryAnchorGate is the outcome of resolving one anchored
// package's visibility and grant status for a scoped caller.
type packageRegistryAnchorGate struct {
	// proceed reports whether the handler may continue with its normal
	// anchored read. False means the handler MUST write the same empty page
	// shape it would write for a nonexistent package, without any further
	// store or graph read (no existence oracle: a private/ungranted package
	// and a nonexistent package are indistinguishable to the caller).
	proceed bool
	// redactSourcePath is true when the row(s) served under this gate came
	// from the public-visibility branch and must have source_path blanked.
	// A "public" registry row can still carry a source_path pointing at an
	// unrelated repository's manifest that the caller has no grant to see
	// (mirrors #5137's per-row redaction of source_key/source_display/
	// lease_owner in status_operations.go). Rows served through a proven
	// correlation grant (the caller's own repository owns/consumes/publishes
	// this private package) are NOT redacted.
	redactSourcePath bool
}

// resolvePackageRegistryAnchorGate decides whether a scoped caller may read
// rows anchored on packageID.
//
// Callers MUST short-circuit on access.empty() before calling this (the
// #5137 double guard: an empty grant returns a bounded empty page without
// any store or graph read). Shared/admin/local callers (access.scoped() ==
// false) always proceed with no redaction -- this function is a no-op gate
// for them.
//
// For a non-empty scoped grant: resolve the anchor package's visibility via
// an indexed uid (or name+ecosystem) lookup. visibility == "public" ->
// proceed, redacting source_path. Otherwise (private, unknown, or the
// package does not exist) -> run the bounded LIMIT-1 correlation probe that
// reuses the exact grant predicate the already-shipped scoped correlations
// route exposes (package_registry_correlations.go,
// listPackageRegistryCorrelationsQuery, including the
// candidate_repository_ids ?| branch). >=1 row -> proceed without
// redaction (the caller already proved a grant-anchored relationship to
// this package). 0 rows -> do not proceed.
func resolvePackageRegistryAnchorGate(
	ctx context.Context,
	span trace.Span,
	graph GraphQuery,
	correlations PackageRegistryCorrelationStore,
	packageID string,
	access repositoryAccessFilter,
) (packageRegistryAnchorGate, error) {
	if !access.scoped() {
		return packageRegistryAnchorGate{proceed: true}, nil
	}
	if packageID == "" {
		return packageRegistryAnchorGate{}, fmt.Errorf("package registry anchor gate requires a resolved package_id")
	}
	visibility, err := packageRegistryAnchorVisibility(ctx, graph, packageID)
	if err != nil {
		return packageRegistryAnchorGate{}, err
	}
	return packageRegistryGateForVisibility(ctx, span, correlations, packageID, visibility, access)
}

// packageRegistryGateForVisibility applies the visibility/grant decision for
// an anchor whose visibility is ALREADY known (for example the packages-by-
// name branch, which resolves package_id and visibility in the same lookup
// and would otherwise re-fetch visibility a second time). access MUST be
// scoped and non-empty; callers with an unscoped or empty access filter
// should not reach this helper (resolvePackageRegistryAnchorGate handles the
// unscoped no-op case).
func packageRegistryGateForVisibility(
	ctx context.Context,
	span trace.Span,
	correlations PackageRegistryCorrelationStore,
	packageID string,
	visibility string,
	access repositoryAccessFilter,
) (packageRegistryAnchorGate, error) {
	if visibility == packageRegistryVisibilityPublic {
		span.SetAttributes(attribute.Bool("pkgreg.scoped_visibility_forced", true))
		return packageRegistryAnchorGate{proceed: true, redactSourcePath: true}, nil
	}
	if correlations == nil {
		span.SetAttributes(attribute.String("pkgreg.correlation_grant", "unavailable"))
		return packageRegistryAnchorGate{}, nil
	}
	rows, err := correlations.ListPackageRegistryCorrelations(ctx, PackageRegistryCorrelationFilter{
		PackageID:            packageID,
		Limit:                1,
		AllowedRepositoryIDs: access.grantedRepositoryIDs(),
		AllowedScopeIDs:      access.grantedScopeIDs(),
	})
	if err != nil {
		return packageRegistryAnchorGate{}, err
	}
	granted := len(rows) > 0
	if granted {
		span.SetAttributes(attribute.String("pkgreg.correlation_grant", "hit"))
	} else {
		span.SetAttributes(attribute.String("pkgreg.correlation_grant", "miss"))
	}
	return packageRegistryAnchorGate{proceed: granted}, nil
}

// packageRegistryAnchorVisibility resolves one Package node's visibility by
// its indexed uid. An empty result (package does not exist) returns "" so
// the caller falls to the correlation probe, which will also miss (0 rows)
// for a nonexistent package -- producing the same not-found-shaped empty
// page as a genuinely absent package (no existence oracle).
func packageRegistryAnchorVisibility(ctx context.Context, graph GraphQuery, packageID string) (string, error) {
	if graph == nil {
		return "", fmt.Errorf("package registry graph is required")
	}
	rows, err := graph.Run(ctx, packageRegistryAnchorVisibilityCypher, map[string]any{"package_id": packageID})
	if err != nil {
		return "", fmt.Errorf("resolve package registry anchor visibility: %w", err)
	}
	if len(rows) == 0 {
		return "", nil
	}
	return StringVal(rows[0], "visibility"), nil
}

// packageRegistryNameAnchorPackageIDAndVisibility resolves the package_id and
// visibility for the packages-by-name branch (ecosystem+name, no package_id
// given), reusing the same {ecosystem, normalized_name} anchor the unscoped
// list already matches on. An empty package_id return means no package
// matched.
func packageRegistryNameAnchorPackageIDAndVisibility(
	ctx context.Context,
	graph GraphQuery,
	ecosystem, name string,
) (string, string, error) {
	if graph == nil {
		return "", "", fmt.Errorf("package registry graph is required")
	}
	rows, err := graph.Run(ctx, packageRegistryNameAnchorVisibilityCypher, map[string]any{
		"ecosystem": ecosystem,
		"name":      name,
	})
	if err != nil {
		return "", "", fmt.Errorf("resolve package registry name anchor: %w", err)
	}
	if len(rows) == 0 {
		return "", "", nil
	}
	return StringVal(rows[0], "package_id"), StringVal(rows[0], "visibility"), nil
}

// packageRegistryVersionAnchorPackageID resolves a PackageVersion's owning
// package id by its indexed uid, for the dependencies-by-version_id path.
// An empty return means the version does not exist.
func packageRegistryVersionAnchorPackageID(ctx context.Context, graph GraphQuery, versionID string) (string, error) {
	if graph == nil {
		return "", fmt.Errorf("package registry graph is required")
	}
	rows, err := graph.Run(ctx, packageRegistryVersionAnchorPackageIDCypher, map[string]any{"version_id": versionID})
	if err != nil {
		return "", fmt.Errorf("resolve package registry version anchor: %w", err)
	}
	if len(rows) == 0 {
		return "", nil
	}
	return StringVal(rows[0], "package_id"), nil
}

// writeEmptyPackageRegistryPackagesPage writes the same empty-result body
// shape listPackages produces for a genuinely nonexistent/empty anchor, used
// by the empty-grant short-circuit and the no-grant (private/unknown, no
// correlation) gate outcome. No existence oracle: this is byte-identical to
// what a caller sees for a package_id that simply does not exist.
func writeEmptyPackageRegistryPackagesPage(w http.ResponseWriter, r *http.Request, h *PackageRegistryHandler, limit int) {
	body := map[string]any{
		"packages":        []PackageRegistryPackageResult{},
		"identity_issues": []PackageRegistryIdentityIssue{},
		"count":           0,
		"limit":           limit,
		"truncated":       false,
	}
	attachCollectorListReadiness(r.Context(), body, h.CollectorReadiness, scope.CollectorPackageRegistry, 0, false)
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		packageRegistryPackagesCapability,
		TruthBasisAuthoritativeGraph,
		"resolved from package registry package identity graph nodes",
	))
}

// writeEmptyPackageRegistryVersionsPage is the listVersions counterpart of
// writeEmptyPackageRegistryPackagesPage.
func writeEmptyPackageRegistryVersionsPage(w http.ResponseWriter, r *http.Request, h *PackageRegistryHandler, limit int) {
	body := map[string]any{
		"versions":  []PackageRegistryVersionResult{},
		"count":     0,
		"limit":     limit,
		"truncated": false,
	}
	attachCollectorListReadiness(r.Context(), body, h.CollectorReadiness, scope.CollectorPackageRegistry, 0, false)
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		packageRegistryVersionsCapability,
		TruthBasisAuthoritativeGraph,
		"resolved from package registry package-version identity graph nodes",
	))
}

// writeEmptyPackageRegistryDependenciesPage is the listDependencies
// counterpart of writeEmptyPackageRegistryPackagesPage.
func writeEmptyPackageRegistryDependenciesPage(w http.ResponseWriter, r *http.Request, h *PackageRegistryHandler, limit int) {
	body := map[string]any{
		"dependencies": []PackageRegistryDependencyResult{},
		"count":        0,
		"limit":        limit,
		"truncated":    false,
	}
	attachCollectorListReadiness(r.Context(), body, h.CollectorReadiness, scope.CollectorPackageRegistry, 0, false)
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		packageRegistryDependenciesCapability,
		TruthBasisAuthoritativeGraph,
		"resolved from package-native dependency graph nodes",
	))
}

// packageRegistryAggregateVisibilityGate forces visibility='public' onto a
// scoped caller's aggregate filter, or reports that the caller explicitly
// asked for a visibility scoped callers cannot see (private/unknown), in
// which case the handler MUST write an empty envelope without calling the
// aggregate store. Unlike the list-route gates, this does NOT short-circuit
// on an empty repository/scope grant: package visibility is a package-level
// property, not tied to the caller's granted repositories, so a scoped
// caller with zero granted repositories still sees real public-package
// totals. Shared/admin/local callers (access.scoped() == false) pass
// filter through unchanged.
func packageRegistryAggregateVisibilityGate(
	ctx context.Context,
	span trace.Span,
	filter PackageRegistryAggregateFilter,
) (out PackageRegistryAggregateFilter, emptyResult bool) {
	access := repositoryAccessFilterFromContext(ctx)
	if !access.scoped() {
		return filter, false
	}
	if filter.Visibility != "" && filter.Visibility != packageRegistryVisibilityPublic {
		return filter, true
	}
	filter.Visibility = packageRegistryVisibilityPublic
	span.SetAttributes(attribute.Bool("pkgreg.scoped_visibility_forced", true))
	return filter, false
}

// packageRegistryPackagesGateResult carries the outcome of
// packageRegistryPackagesGate back to listPackages.
type packageRegistryPackagesGateResult struct {
	// packageID is the caller-supplied package_id, or the package_id
	// resolved from the name+ecosystem anchor when the caller supplied name
	// instead.
	packageID string
	// redactSourcePath reports whether source_path must be blanked on served
	// rows (see packageRegistryAnchorGate.redactSourcePath).
	redactSourcePath bool
	// useScopedEcosystemCypher reports whether listPackages must use
	// packageRegistryPackagesScopedEcosystemCypher instead of
	// packageRegistryPackagesCypher (the ecosystem-only browse branch for a
	// scoped caller).
	useScopedEcosystemCypher bool
}

// packageRegistryPackagesGate performs the empty-grant short-circuit, the
// backend-availability check, and the scoped anchor/ecosystem-browse gating
// for listPackages, writing the response itself whenever the handler must
// not proceed to its normal graph read. handled reports that outcome: when
// true, listPackages must return immediately.
func packageRegistryPackagesGate(
	w http.ResponseWriter,
	r *http.Request,
	h *PackageRegistryHandler,
	span trace.Span,
	packageID, ecosystem, name string,
	limit int,
) (result packageRegistryPackagesGateResult, handled bool) {
	result.packageID = packageID
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		writeEmptyPackageRegistryPackagesPage(w, r, h, limit)
		return result, true
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
		return result, true
	}
	if !access.scoped() {
		return result, false
	}
	switch {
	case packageID != "":
		gate, err := resolvePackageRegistryAnchorGate(r.Context(), span, h.Neo4j, h.Correlations, packageID, access)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return result, true
		}
		if !gate.proceed {
			writeEmptyPackageRegistryPackagesPage(w, r, h, limit)
			return result, true
		}
		result.redactSourcePath = gate.redactSourcePath
	case name != "":
		resolvedID, visibility, err := packageRegistryNameAnchorPackageIDAndVisibility(r.Context(), h.Neo4j, ecosystem, name)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return result, true
		}
		if resolvedID == "" {
			writeEmptyPackageRegistryPackagesPage(w, r, h, limit)
			return result, true
		}
		gate, err := packageRegistryGateForVisibility(r.Context(), span, h.Correlations, resolvedID, visibility, access)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return result, true
		}
		if !gate.proceed {
			writeEmptyPackageRegistryPackagesPage(w, r, h, limit)
			return result, true
		}
		result.packageID = resolvedID
		result.redactSourcePath = gate.redactSourcePath
	default:
		// Ecosystem-only browse: no per-package anchor exists, so scoped
		// callers get the visibility='public'-filtered query shape instead
		// of a per-row gate. Correlation-augmented private-package inclusion
		// in scoped browse is deferred (see the F-6/W5b decision doc); every
		// row this branch returns is public.
		result.useScopedEcosystemCypher = true
		result.redactSourcePath = true
	}
	return result, false
}

// packageRegistryVersionsGate performs the empty-grant short-circuit, the
// backend-availability check, and the scoped anchor gating for listVersions.
// It returns true when it has already written the response and listVersions
// must return immediately.
func packageRegistryVersionsGate(
	w http.ResponseWriter,
	r *http.Request,
	h *PackageRegistryHandler,
	span trace.Span,
	packageID string,
	limit int,
) bool {
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		writeEmptyPackageRegistryVersionsPage(w, r, h, limit)
		return true
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
		return true
	}
	if !access.scoped() {
		return false
	}
	gate, err := resolvePackageRegistryAnchorGate(r.Context(), span, h.Neo4j, h.Correlations, packageID, access)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return true
	}
	if !gate.proceed {
		writeEmptyPackageRegistryVersionsPage(w, r, h, limit)
		return true
	}
	return false
}

// packageRegistryDependenciesGate performs the empty-grant short-circuit,
// the backend-availability check, and the scoped anchor gating for
// listDependencies -- resolving the version_id anchor to its owning
// package_id first when the caller supplied version_id but not package_id.
// It returns true when it has already written the response and
// listDependencies must return immediately.
func packageRegistryDependenciesGate(
	w http.ResponseWriter,
	r *http.Request,
	h *PackageRegistryHandler,
	span trace.Span,
	packageID, versionID string,
	limit int,
) bool {
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		writeEmptyPackageRegistryDependenciesPage(w, r, h, limit)
		return true
	}
	if h.Neo4j == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"package registry dependency queries require the authoritative graph",
			ErrorCodeBackendUnavailable,
			packageRegistryDependenciesCapability,
			h.profile(),
			requiredProfile(packageRegistryDependenciesCapability),
		)
		return true
	}
	if !access.scoped() {
		return false
	}
	anchorPackageID := packageID
	if anchorPackageID == "" {
		resolvedID, err := packageRegistryVersionAnchorPackageID(r.Context(), h.Neo4j, versionID)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return true
		}
		if resolvedID == "" {
			writeEmptyPackageRegistryDependenciesPage(w, r, h, limit)
			return true
		}
		anchorPackageID = resolvedID
	}
	gate, err := resolvePackageRegistryAnchorGate(r.Context(), span, h.Neo4j, h.Correlations, anchorPackageID, access)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return true
	}
	if !gate.proceed {
		writeEmptyPackageRegistryDependenciesPage(w, r, h, limit)
		return true
	}
	return false
}
