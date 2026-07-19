// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"

	"go.opentelemetry.io/otel/trace"
)

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
			// Name did not resolve. Issue the same correlation probe a
			// resolving-but-gated package would (against the sentinel anchor),
			// discard its result, and write the empty page -- so a nonexistent
			// name is indistinguishable from an existing-but-ungranted one by
			// store round-trip count and latency (no existence oracle).
			if _, probeErr := packageRegistryGateForVisibility(r.Context(), span, h.Correlations, packageRegistryNonexistentAnchorSentinel, "", access); probeErr != nil {
				WriteError(w, http.StatusInternalServerError, probeErr.Error())
				return result, true
			}
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
			// version_id did not resolve. Run the same visibility-lookup +
			// correlation-probe gate a resolving-but-gated package would
			// (against the sentinel anchor), discard its result, and write the
			// empty page -- so a nonexistent version_id is indistinguishable
			// from an existing-but-ungranted one by store round-trip count and
			// latency (no existence oracle). This keeps the total round-trips
			// (version anchor + visibility + probe) equal to the resolving
			// path.
			if _, probeErr := resolvePackageRegistryAnchorGate(r.Context(), span, h.Neo4j, h.Correlations, packageRegistryNonexistentAnchorSentinel, access); probeErr != nil {
				WriteError(w, http.StatusInternalServerError, probeErr.Error())
				return true
			}
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
