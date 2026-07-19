// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// keyedPackageRegistryCorrelationStore answers ListPackageRegistryCorrelations
// per package_id from a fixed grant/deny map, for tests that need different
// candidates to resolve differently through the correlation-grant probe.
// Handles both the scalar PackageID anchor (single-candidate gate,
// packageRegistryGateForVisibility) and the batched PackageIDs anchor
// (packageRegistryGateForVisibilityBatch), returning one row per requested id
// that is present in the granted map -- mirroring how the real Postgres
// store's IN-clause filter behaves for a batch.
type keyedPackageRegistryCorrelationStore struct {
	grantedPackageIDs map[string]bool
	calls             int
}

func (s *keyedPackageRegistryCorrelationStore) ListPackageRegistryCorrelations(
	_ context.Context,
	filter PackageRegistryCorrelationFilter,
) ([]PackageRegistryCorrelationRow, error) {
	s.calls++
	requested := filter.PackageIDs
	if filter.PackageID != "" {
		requested = append(requested, filter.PackageID)
	}
	var rows []PackageRegistryCorrelationRow
	for _, id := range requested {
		if s.grantedPackageIDs[id] {
			rows = append(rows, PackageRegistryCorrelationRow{PackageID: id})
		}
	}
	return rows, nil
}

// TestPackageRegistryPackagesByNamePreservesEveryGrantedCandidate is the
// regression test for the F-6/W5 review finding: normalized_name is not a
// unique package identity within an ecosystem (distinct registries or
// namespaces can share it), so the packages-by-name scoped branch must not
// collapse the {ecosystem, normalized_name} anchor to a single resolved
// package_id. Before the fix, packageRegistryPackagesGate resolved only the
// FIRST matching row from packageRegistryNameAnchorVisibilityCypher and
// re-anchored the real read on that one uid, silently dropping every other
// package sharing the name -- public or privately grant-accessible alike,
// with the survivor depending on arbitrary backend row order.
//
// Three candidates share {ecosystem: "npm", normalized_name: "collide"}:
//   - pkg-public: visibility=public, must always be visible (redacted
//     source_path).
//   - pkg-granted: visibility=private, tenant A holds a correlation grant for
//     it, must be visible (unredacted source_path).
//   - pkg-denied: visibility=private, tenant A holds no grant for it, must be
//     excluded entirely.
func TestPackageRegistryPackagesByNamePreservesEveryGrantedCandidate(t *testing.T) {
	t.Parallel()

	const (
		pkgPublic  = "pkg:npm:collide-public"
		pkgGranted = "pkg:npm:collide-granted"
		pkgDenied  = "pkg:npm:collide-denied"
	)

	graph := &packageRegistryScopedGraphFake{
		t: t,
		nameAnchorByKey: map[string][]map[string]any{
			"npm\x00collide": {
				{"package_id": pkgPublic, "visibility": "public"},
				{"package_id": pkgGranted, "visibility": "private"},
				{"package_id": pkgDenied, "visibility": "private"},
			},
		},
		rows: []map[string]any{
			{
				"package_id": pkgPublic, "ecosystem": "npm", "normalized_name": "collide",
				"source_path": "public/package.json", "visibility": "public",
			},
			{
				"package_id": pkgGranted, "ecosystem": "npm", "normalized_name": "collide",
				"source_path": "granted/package.json", "visibility": "private",
			},
			{
				"package_id": pkgDenied, "ecosystem": "npm", "normalized_name": "collide",
				"source_path": "denied/package.json", "visibility": "private",
			},
		},
	}
	correlations := &keyedPackageRegistryCorrelationStore{grantedPackageIDs: map[string]bool{pkgGranted: true}}
	handler := &PackageRegistryHandler{Neo4j: graph, Correlations: correlations, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages?ecosystem=npm&name=collide&limit=10", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), tenantAScopedAuthContext()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var body struct {
		Packages []PackageRegistryPackageResult `json:"packages"`
		Count    int                            `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, rec.Body.String())
	}
	byID := make(map[string]PackageRegistryPackageResult, len(body.Packages))
	for _, pkg := range body.Packages {
		byID[pkg.PackageID] = pkg
	}
	if body.Count != 2 {
		t.Fatalf("count = %d, want 2 (public + granted-private, denied-private excluded); packages = %#v", body.Count, body.Packages)
	}
	if _, ok := byID[pkgDenied]; ok {
		t.Fatalf("ungranted candidate %s leaked into the response: %#v", pkgDenied, body.Packages)
	}
	public, ok := byID[pkgPublic]
	if !ok {
		t.Fatalf("public candidate %s missing from response (the exact bug: collapsing to one candidate drops siblings): %#v", pkgPublic, body.Packages)
	}
	if public.SourcePath != "" {
		t.Fatalf("public candidate source_path = %q, want redacted", public.SourcePath)
	}
	granted, ok := byID[pkgGranted]
	if !ok {
		t.Fatalf("grant-accessible private candidate %s missing from response: %#v", pkgGranted, body.Packages)
	}
	if granted.SourcePath != "granted/package.json" {
		t.Fatalf("grant-accessible candidate source_path = %q, want unredacted", granted.SourcePath)
	}
	// Round-trip proof: packageRegistryGateForVisibilityBatch issues exactly
	// ONE correlation-store call for the 2 private candidates (pkgGranted,
	// pkgDenied; pkgPublic never reaches the correlation store at all) --
	// not one call per private candidate. Before batching, this would have
	// been 2 calls.
	if correlations.calls != 1 {
		t.Fatalf("correlation store calls = %d, want 1 (batched, not per-candidate)", correlations.calls)
	}
}

// TestPackageRegistryPackagesByNameRedactsIdentityIssueMetadata is the
// regression test for the review's identity-issue fail-closed finding: a row
// matching the {ecosystem, normalized_name} anchor that fails identity
// extraction (packageRegistryPackageResultFromRow returns an issue because
// the row carries no package_id -- malformed graph data) cannot be looked up
// in the name+ecosystem branch's per-candidate nameAnchorRedactByID map, so
// its grant status is unverifiable. Every metadata field it carries --
// source_path, registry, namespace, purl, bom_ref, package_manager,
// source_specific_id, visibility, source_confidence, version_count -- MUST
// be blanked, not only source_path, since any of them could describe a
// private/unknown package the caller has no grant for. Before the fix (both
// a20c7419c's source_path redaction and redactPackageRegistryIdentityIssueMetadata's
// broader redaction), every one of these fields shipped unredacted.
func TestPackageRegistryPackagesByNameRedactsIdentityIssueMetadata(t *testing.T) {
	t.Parallel()

	const pkgGood = "pkg:npm:collide-good"

	graph := &packageRegistryScopedGraphFake{
		t: t,
		nameAnchorByKey: map[string][]map[string]any{
			// Only the well-formed candidate is visible to the anchor lookup
			// (a real graph would never expose a node with no uid through
			// packageRegistryNameAnchorVisibilityCypher's RETURN p.uid); the
			// malformed row below models a data-quality defect in the SEPARATE
			// main read, exactly the "graph data missing uid" shape the anchor
			// gate cannot see coming and therefore cannot pre-filter.
			"npm\x00collide": {{"package_id": pkgGood, "visibility": "public"}},
		},
		rows: []map[string]any{
			{
				"package_id": pkgGood, "ecosystem": "npm", "normalized_name": "collide",
				"source_path": "good/package.json", "visibility": "public",
			},
			{
				// No "package_id": packageRegistryPackageResultFromRow returns
				// an identity issue for this row instead of a result.
				"ecosystem": "npm", "normalized_name": "collide",
				"registry": "private-registry.example", "namespace": "acme-internal",
				"purl": "pkg:npm/acme-internal/collide", "bom_ref": "bom-ref-1",
				"package_manager": "npm", "source_path": "private/package.json",
				"source_specific_id": "internal-id-42", "visibility": "private",
				"source_confidence": "high", "version_count": int64(7),
			},
		},
	}
	handler := &PackageRegistryHandler{Neo4j: graph, Correlations: &fatalOnCallPackageRegistryCorrelationStore{t: t}, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages?ecosystem=npm&name=collide&limit=10", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), tenantAScopedAuthContext()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var body struct {
		Packages       []PackageRegistryPackageResult `json:"packages"`
		IdentityIssues []PackageRegistryIdentityIssue `json:"identity_issues"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, rec.Body.String())
	}
	if len(body.Packages) != 1 || body.Packages[0].PackageID != pkgGood {
		t.Fatalf("packages = %#v, want exactly [%s]", body.Packages, pkgGood)
	}
	if len(body.IdentityIssues) != 1 {
		t.Fatalf("identity_issues = %#v, want exactly 1", body.IdentityIssues)
	}
	issue := body.IdentityIssues[0]
	if issue.SourcePath != "" {
		t.Errorf("identity issue source_path = %q, want redacted", issue.SourcePath)
	}
	if issue.Registry != "" {
		t.Errorf("identity issue registry = %q, want redacted", issue.Registry)
	}
	if issue.Namespace != "" {
		t.Errorf("identity issue namespace = %q, want redacted", issue.Namespace)
	}
	if issue.PURL != "" {
		t.Errorf("identity issue purl = %q, want redacted", issue.PURL)
	}
	if issue.BOMRef != "" {
		t.Errorf("identity issue bom_ref = %q, want redacted", issue.BOMRef)
	}
	if issue.PackageManager != "" {
		t.Errorf("identity issue package_manager = %q, want redacted", issue.PackageManager)
	}
	if issue.SourceSpecificID != "" {
		t.Errorf("identity issue source_specific_id = %q, want redacted", issue.SourceSpecificID)
	}
	if issue.Visibility != "" {
		t.Errorf("identity issue visibility = %q, want redacted", issue.Visibility)
	}
	if issue.SourceConfidence != "" {
		t.Errorf("identity issue source_confidence = %q, want redacted", issue.SourceConfidence)
	}
	if issue.VersionCount != 0 {
		t.Errorf("identity issue version_count = %d, want redacted (0)", issue.VersionCount)
	}
	// Ecosystem and normalized_name are NOT redacted: the caller already
	// supplied both as request query parameters, so echoing them back is not
	// a new disclosure.
	if issue.Ecosystem != "npm" || issue.NormalizedName != "collide" {
		t.Errorf("identity issue ecosystem/normalized_name = %q/%q, want npm/collide (caller-supplied, not a new disclosure)", issue.Ecosystem, issue.NormalizedName)
	}
}

// crowdingPackageRegistryCorrelationStore simulates one candidate's
// correlation rows filling the batched read's shared LIMIT window (a real
// listPackageRegistryCorrelationsQuery orders by fact_id across the WHOLE
// matched set, not per package_id), so a co-candidate's own grant is invisible
// in that one batched response. It answers a batch call (PackageIDs set) with
// exactly packageRegistryMaxLimit synthetic rows all attributed to
// crowderPackageID, and answers a scalar call (PackageID set, the
// packageRegistryGateForVisibilityBatch individual-fallback shape) from
// grantedPackageIDs, proving the fallback path actually recovers a candidate
// the crowded batch made ambiguous.
type crowdingPackageRegistryCorrelationStore struct {
	crowderPackageID  string
	grantedPackageIDs map[string]bool
	batchCalls        int
	scalarCalls       int
}

func (s *crowdingPackageRegistryCorrelationStore) ListPackageRegistryCorrelations(
	_ context.Context,
	filter PackageRegistryCorrelationFilter,
) ([]PackageRegistryCorrelationRow, error) {
	if len(filter.PackageIDs) > 0 {
		s.batchCalls++
		rows := make([]PackageRegistryCorrelationRow, packageRegistryMaxLimit)
		for i := range rows {
			rows[i] = PackageRegistryCorrelationRow{PackageID: s.crowderPackageID}
		}
		return rows, nil
	}
	s.scalarCalls++
	if s.grantedPackageIDs[filter.PackageID] {
		return []PackageRegistryCorrelationRow{{PackageID: filter.PackageID}}, nil
	}
	return nil, nil
}

// TestPackageRegistryPackagesByNameBatchAmbiguityFallsBackToIndividualVerification
// proves packageRegistryGateForVisibilityBatch's crowd-out safeguard: when the
// batched correlation read fills packageRegistryMaxLimit rows (proving at
// least one candidate's own correlation rows could have crowded a
// co-candidate's only row off the page), every candidate NOT found in that
// batched response is individually re-verified rather than treated as denied.
// Without the fallback, pkgVictim (crowded out by pkgCrowder's rows, but
// genuinely grant-accessible) would be silently dropped -- the same class of
// bug this whole fix closes, reintroduced one layer down at the correlation
// read instead of the anchor read.
func TestPackageRegistryPackagesByNameBatchAmbiguityFallsBackToIndividualVerification(t *testing.T) {
	t.Parallel()

	const (
		pkgCrowder = "pkg:npm:crowder"
		pkgVictim  = "pkg:npm:victim"
	)

	graph := &packageRegistryScopedGraphFake{
		t: t,
		nameAnchorByKey: map[string][]map[string]any{
			"npm\x00collide2": {
				{"package_id": pkgCrowder, "visibility": "private"},
				{"package_id": pkgVictim, "visibility": "private"},
			},
		},
		rows: []map[string]any{
			{"package_id": pkgCrowder, "ecosystem": "npm", "normalized_name": "collide2", "source_path": "crowder/package.json", "visibility": "private"},
			{"package_id": pkgVictim, "ecosystem": "npm", "normalized_name": "collide2", "source_path": "victim/package.json", "visibility": "private"},
		},
	}
	correlations := &crowdingPackageRegistryCorrelationStore{
		crowderPackageID:  pkgCrowder,
		grantedPackageIDs: map[string]bool{pkgCrowder: true, pkgVictim: true},
	}
	handler := &PackageRegistryHandler{Neo4j: graph, Correlations: correlations, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages?ecosystem=npm&name=collide2&limit=10", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), tenantAScopedAuthContext()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var body struct {
		Packages []PackageRegistryPackageResult `json:"packages"`
		Count    int                            `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, rec.Body.String())
	}
	byID := make(map[string]PackageRegistryPackageResult, len(body.Packages))
	for _, pkg := range body.Packages {
		byID[pkg.PackageID] = pkg
	}
	if body.Count != 2 {
		t.Fatalf("count = %d, want 2 (both crowder and victim are granted); packages = %#v", body.Count, body.Packages)
	}
	if _, ok := byID[pkgCrowder]; !ok {
		t.Fatalf("pkgCrowder missing from response: %#v", body.Packages)
	}
	if _, ok := byID[pkgVictim]; !ok {
		t.Fatalf("pkgVictim (crowded out of the batch page, must be recovered by individual fallback verification) missing from response: %#v", body.Packages)
	}
	if correlations.batchCalls != 1 {
		t.Fatalf("batch correlation calls = %d, want 1", correlations.batchCalls)
	}
	if correlations.scalarCalls != 1 {
		t.Fatalf("scalar (individual fallback) correlation calls = %d, want 1 (only pkgVictim was ambiguous; pkgCrowder was already found in the batch)", correlations.scalarCalls)
	}
}
