// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// packageRegistryPackagesGateResult carries the outcome of
// packageRegistryPackagesGate back to listPackages.
type packageRegistryPackagesGateResult struct {
	// packageID is the caller-supplied package_id. It is set ONLY for the
	// package_id-anchored branch; the name+ecosystem branch never collapses
	// to a single id (see nameAnchorRedactByID) because normalized_name is
	// not a unique package identity within an ecosystem.
	packageID string
	// redactSourcePath reports whether source_path must be blanked on served
	// rows for the package_id-anchored and ecosystem-browse branches (see
	// packageRegistryAnchorGate.redactSourcePath). The name+ecosystem branch
	// uses nameAnchorRedactByID for its well-formed candidate rows instead,
	// since they can carry a per-row mix of public and
	// correlation-granted-private redaction; it still sets redactSourcePath
	// true as a fail-closed default for any row that fails identity
	// extraction entirely (no package_id to look up in the map).
	redactSourcePath bool
	// useScopedEcosystemCypher reports whether listPackages must use
	// packageRegistryPackagesScopedEcosystemCypher instead of
	// packageRegistryPackagesCypher (the ecosystem-only browse branch for a
	// scoped caller).
	useScopedEcosystemCypher bool
	// nameAnchorRedactByID is non-nil only for the name+ecosystem branch. It
	// maps every package_id the caller is allowed to see (public, or
	// correlation-granted private) to whether that row's source_path must be
	// redacted. listPackages MUST use it to filter and redact the
	// name-anchored read's full candidate set instead of trusting the
	// underlying MATCH to have already scoped the result -- the query
	// returns every package sharing the anchor regardless of visibility or
	// grant.
	nameAnchorRedactByID map[string]bool
	// nameAnchorCandidatesTruncated reports whether more than
	// packageRegistryNameAnchorCandidateLimit packages matched the
	// name+ecosystem anchor (packageRegistryNameAnchorCandidates detected the
	// limit+1 row). listPackages MUST fold this into the response's
	// truncated flag so a caller cannot mistake a capped candidate set for a
	// complete one -- the same silent-drop shape this fix closes, just past a
	// higher and rarer threshold instead of always.
	nameAnchorCandidatesTruncated bool
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
			if WriteGraphReadError(w, r, err, packageRegistryPackagesCapability) {
				return result, true
			}
			WriteError(w, http.StatusInternalServerError, err.Error())
			return result, true
		}
		if !gate.proceed {
			writeEmptyPackageRegistryPackagesPage(w, r, h, limit)
			return result, true
		}
		result.redactSourcePath = gate.redactSourcePath
	case name != "":
		// normalized_name is not a unique package identity within an
		// ecosystem (distinct registries or namespaces can share it), so
		// EVERY matching candidate must be resolved and gated individually
		// -- collapsing to a single resolved id here would silently drop
		// other legitimate public or grant-accessible packages sharing the
		// name (see packageRegistryNameAnchorCandidates's doc comment).
		candidates, candidatesTruncated, err := packageRegistryNameAnchorCandidates(r.Context(), h.Neo4j, ecosystem, name)
		if err != nil {
			if WriteGraphReadError(w, r, err, packageRegistryPackagesCapability) {
				return result, true
			}
			WriteError(w, http.StatusInternalServerError, err.Error())
			return result, true
		}
		if candidatesTruncated {
			// Operator-facing signal: more than packageRegistryNameAnchorCandidateLimit
			// packages share this {ecosystem, normalized_name} anchor. Not
			// expected in practice; if it fires, the cap may need raising.
			span.SetAttributes(attribute.Bool("pkgreg.name_anchor_candidates_truncated", true))
		}
		if len(candidates) == 0 {
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
		candidateGates, err := packageRegistryGateForVisibilityBatch(r.Context(), span, h.Correlations, candidates, access)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return result, true
		}
		redactByID := make(map[string]bool, len(candidates))
		for _, candidate := range candidates {
			if gate := candidateGates[candidate.PackageID]; gate.proceed {
				redactByID[candidate.PackageID] = gate.redactSourcePath
			}
		}
		if len(redactByID) == 0 {
			// Every candidate was denied. Write the same empty page a
			// nonexistent name produces -- do NOT set
			// nameAnchorCandidatesTruncated here even if candidatesTruncated
			// is true: a fully-denied caller must not learn "many packages
			// share this name" from a response shape it cannot otherwise
			// distinguish from "the name does not exist" (no existence
			// oracle). The truncation signal is only meaningful once at
			// least one row is actually being returned to the caller.
			writeEmptyPackageRegistryPackagesPage(w, r, h, limit)
			return result, true
		}
		result.nameAnchorRedactByID = redactByID
		result.nameAnchorCandidatesTruncated = candidatesTruncated
		// Fail-closed default for a row that fails identity extraction
		// (packageRegistryPackageResultFromRow returns an issue instead of a
		// PackageRegistryPackageResult, so it has no package_id to look up in
		// nameAnchorRedactByID and its grant status cannot be determined).
		// listPackages uses this bool, not the per-id map, for the
		// identity-issue path.
		result.redactSourcePath = true
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
		if WriteGraphReadError(w, r, err, packageRegistryVersionsCapability) {
			return true
		}
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
			if WriteGraphReadError(w, r, err, packageRegistryDependenciesCapability) {
				return true
			}
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
				if WriteGraphReadError(w, r, probeErr, packageRegistryDependenciesCapability) {
					return true
				}
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
		if WriteGraphReadError(w, r, err, packageRegistryDependenciesCapability) {
			return true
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return true
	}
	if !gate.proceed {
		writeEmptyPackageRegistryDependenciesPage(w, r, h, limit)
		return true
	}
	return false
}
