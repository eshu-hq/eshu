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

// packageRegistryNonexistentAnchorSentinel is a package_id used ONLY to
// equalize store round-trips when a name+ecosystem or version_id anchor does
// not resolve to a package. Without it, a non-resolving anchor would
// short-circuit with fewer store calls than an existing-but-gated
// (private/unknown, no grant) anchor, letting a caller distinguish "package
// exists" from "package does not exist" by round-trip count or latency -- a
// timing/existence oracle that the empty-body symmetry alone does not close.
// The gate runs the SAME visibility-lookup + correlation-probe sequence
// against this sentinel that a resolving anchor would, then DISCARDS the
// result and writes the empty page unconditionally. Because the result is
// discarded, the sentinel only needs to be a non-empty, valid-text value the
// stores accept; it is never served, never becomes result.packageID, and a
// package or correlation fact that happened to carry this id would still not
// be returned. It contains characters no collector-normalized package
// identity emits.
const packageRegistryNonexistentAnchorSentinel = "eshu::package-registry::nonexistent-anchor::timing-equalizer"

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

// packageRegistryGateForVisibilityBatch is the batched sibling of
// packageRegistryGateForVisibility for the name+ecosystem branch, where up to
// packageRegistryNameAnchorCandidateLimit candidates must each be gated: it
// resolves every public candidate immediately (no store read) and issues ONE
// correlation query for every private/unknown candidate instead of one query
// per candidate.
//
// A single shared-LIMIT batched read is NOT simply correct here:
// listPackageRegistryCorrelationsQuery orders by fact_id across the WHOLE
// matched set and applies one LIMIT, so if one candidate has many
// grant-visible correlation rows within the caller's own granted
// repositories/scopes, its rows can fill the page before a co-candidate's
// only row is reached -- silently reproducing this same PR's class of bug
// (a real candidate treated as ungranted) at the correlation layer instead
// of the anchor layer. This function closes that gap: it batches at
// packageRegistryMaxLimit (comfortably above the realistic per-candidate
// row count within one caller's own grant) and, if the response filled that
// page (proving at least one candidate's rows could have crowded out
// another's), individually re-verifies every candidate whose presence in
// the batched result is still unproven -- so batching is a strict
// round-trip win in the common case and never trades correctness for it in
// the adversarial one.
func packageRegistryGateForVisibilityBatch(
	ctx context.Context,
	span trace.Span,
	correlations PackageRegistryCorrelationStore,
	candidates []packageRegistryNameCandidate,
	access repositoryAccessFilter,
) (map[string]packageRegistryAnchorGate, error) {
	gates := make(map[string]packageRegistryAnchorGate, len(candidates))
	needsProbe := make([]packageRegistryNameCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Visibility == packageRegistryVisibilityPublic {
			gates[candidate.PackageID] = packageRegistryAnchorGate{proceed: true, redactSourcePath: true}
			continue
		}
		needsProbe = append(needsProbe, candidate)
	}
	if len(needsProbe) == 0 {
		span.SetAttributes(attribute.Bool("pkgreg.scoped_visibility_forced", true))
		return gates, nil
	}
	if correlations == nil {
		span.SetAttributes(attribute.String("pkgreg.correlation_grant", "unavailable"))
		for _, candidate := range needsProbe {
			gates[candidate.PackageID] = packageRegistryAnchorGate{}
		}
		return gates, nil
	}
	packageIDs := make([]string, len(needsProbe))
	for i, candidate := range needsProbe {
		packageIDs[i] = candidate.PackageID
	}
	rows, err := correlations.ListPackageRegistryCorrelations(ctx, PackageRegistryCorrelationFilter{
		PackageIDs:           packageIDs,
		Limit:                packageRegistryMaxLimit,
		AllowedRepositoryIDs: access.grantedRepositoryIDs(),
		AllowedScopeIDs:      access.grantedScopeIDs(),
	})
	if err != nil {
		return nil, err
	}
	grantedSeen := make(map[string]bool, len(rows))
	for _, row := range rows {
		grantedSeen[row.PackageID] = true
	}
	// The batch page filled: some candidate's rows may have crowded a
	// co-candidate's only row off the LIMIT window, so an absence in
	// grantedSeen is not proof of zero correlations. Below the cap, absence
	// IS proof (the query would have returned every matching row).
	ambiguous := len(rows) >= packageRegistryMaxLimit
	verified := 0
	for _, candidate := range needsProbe {
		if grantedSeen[candidate.PackageID] {
			gates[candidate.PackageID] = packageRegistryAnchorGate{proceed: true}
			continue
		}
		if !ambiguous {
			gates[candidate.PackageID] = packageRegistryAnchorGate{}
			continue
		}
		gate, err := packageRegistryGateForVisibility(ctx, span, correlations, candidate.PackageID, candidate.Visibility, access)
		if err != nil {
			return nil, err
		}
		gates[candidate.PackageID] = gate
		verified++
	}
	span.SetAttributes(
		attribute.Int("pkgreg.name_anchor_batch_candidates", len(needsProbe)),
		attribute.Bool("pkgreg.name_anchor_batch_ambiguous", ambiguous),
		attribute.Int("pkgreg.name_anchor_batch_individually_verified", verified),
	)
	return gates, nil
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

// packageRegistryNameCandidate is one Package node matching a
// {ecosystem, normalized_name} anchor.
type packageRegistryNameCandidate struct {
	PackageID  string
	Visibility string
}

// packageRegistryNameAnchorCandidates resolves EVERY package_id and
// visibility for the packages-by-name branch (ecosystem+name, no package_id
// given), reusing the same {ecosystem, normalized_name} anchor the unscoped
// list already matches on. normalized_name is not a unique package identity
// within an ecosystem -- distinct registries or namespaces can legitimately
// share it -- so callers MUST gate every returned candidate individually
// rather than collapsing to a single resolved id.
//
// A zero-length return means no package matched. truncated reports whether
// more than packageRegistryNameAnchorCandidateLimit candidates matched
// (packageRegistryNameAnchorVisibilityCypher fetches one past the limit to
// detect this, the same idiom listPackages uses for page limits); the
// caller MUST surface this rather than silently presenting a partial
// candidate set as complete.
func packageRegistryNameAnchorCandidates(
	ctx context.Context,
	graph GraphQuery,
	ecosystem, name string,
) (candidates []packageRegistryNameCandidate, truncated bool, err error) {
	if graph == nil {
		return nil, false, fmt.Errorf("package registry graph is required")
	}
	rows, err := graph.Run(ctx, packageRegistryNameAnchorVisibilityCypher, map[string]any{
		"ecosystem": ecosystem,
		"name":      name,
	})
	if err != nil {
		return nil, false, fmt.Errorf("resolve package registry name anchor: %w", err)
	}
	if len(rows) > packageRegistryNameAnchorCandidateLimit {
		truncated = true
		rows = rows[:packageRegistryNameAnchorCandidateLimit]
	}
	candidates = make([]packageRegistryNameCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, packageRegistryNameCandidate{
			PackageID:  StringVal(row, "package_id"),
			Visibility: StringVal(row, "visibility"),
		})
	}
	return candidates, truncated, nil
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
