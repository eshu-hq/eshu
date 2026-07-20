// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestLivePackageRegistryListPackagesReturnsZeroVersionPackages is the
// backend-required proof that /api/v0/package-registry/packages no longer
// silently drops every zero-version package.
//
// On the pinned NornicDB build, OPTIONAL MATCH (p)-[:HAS_VERSION]->(v)
// followed by RETURN p.uid, count(v) does not group by every non-aggregate
// RETURN key (p.uid), as openCypher requires. Instead every row whose
// optional side is null collapses into a single implicit bucket, so only ONE
// package survives per page and every zero-version package silently
// vanishes from the response — indistinguishable from "not found" for an
// exact package_id lookup. See docs/public/reference/nornicdb-pitfalls.md.
//
// Run: ESHU_PKG_REGISTRY_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:7687 \
//
//	go test ./internal/query -run TestLivePackageRegistryListPackagesReturnsZeroVersionPackages -count=1 -v
func TestLivePackageRegistryListPackagesReturnsZeroVersionPackages(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_PKG_REGISTRY_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_PKG_REGISTRY_PROVE_LIVE=1 to run the live package-registry version-count proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required (e.g. bolt://localhost:7687)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()

	write := func(cypher string, params map[string]any) {
		s := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: "nornic"})
		defer func() { _ = s.Close(ctx) }()
		if _, err := s.Run(ctx, cypher, params); err != nil {
			t.Fatalf("seed write failed: %v\ncypher=%s", err, cypher)
		}
	}
	reader := NewNeo4jReader(driver, "nornic")

	const ecosystem = "npm-live-5167-count-fix"
	const (
		pkgNoVersionsA = "pkg:live5167:no-versions-a"
		pkgNoVersionsB = "pkg:live5167:no-versions-b"
		pkgTwoVersions = "pkg:live5167:two-versions"
	)
	// Two single-clause MATCH+DELETE statements, not one OPTIONAL-MATCH+DELETE
	// pipeline: per docs/public/reference/nornicdb-pitfalls.md, an
	// OPTIONAL MATCH ... DELETE pipeline on this backend returns the filtered
	// row but silently fails to apply the trailing DELETE.
	cleanup := func() {
		write(`MATCH (p:Package {ecosystem: $ecosystem})-[:HAS_VERSION]->(v:PackageVersion) DETACH DELETE v`,
			map[string]any{"ecosystem": ecosystem})
		write(`MATCH (p:Package {ecosystem: $ecosystem}) DETACH DELETE p`,
			map[string]any{"ecosystem": ecosystem})
	}
	cleanup()
	defer cleanup()

	write(`CREATE (a:Package {uid: $a, ecosystem: $ecosystem, normalized_name: 'a'})
	       CREATE (b:Package {uid: $b, ecosystem: $ecosystem, normalized_name: 'b'})
	       CREATE (p:Package {uid: $c, ecosystem: $ecosystem, normalized_name: 'c'})
	       CREATE (v1:PackageVersion {uid: $c1})
	       CREATE (v2:PackageVersion {uid: $c2})
	       CREATE (p)-[:HAS_VERSION]->(v1)
	       CREATE (p)-[:HAS_VERSION]->(v2)`,
		map[string]any{
			"a": pkgNoVersionsA, "b": pkgNoVersionsB, "c": pkgTwoVersions,
			"ecosystem": ecosystem,
			"c1":        pkgTwoVersions + ":1.0.0",
			"c2":        pkgTwoVersions + ":2.0.0",
		})

	// Capture the OLD broken shape directly for evidence: OPTIONAL MATCH +
	// count(v) in one statement. Expected (openCypher-correct): 3 rows, two
	// with vc=0. Observed on the pinned NornicDB build: 1 row.
	oldRows, err := reader.Run(ctx,
		`MATCH (p:Package {ecosystem: $ecosystem}) OPTIONAL MATCH (p)-[:HAS_VERSION]->(v:PackageVersion) RETURN p.uid AS id, count(v) AS vc ORDER BY p.uid`,
		map[string]any{"ecosystem": ecosystem})
	if err != nil {
		t.Fatalf("old-shape probe query failed: %v", err)
	}
	t.Logf("OLD OPTIONAL-MATCH+count(v) shape returned %d row(s) (want 3 — broken when < 3): %#v", len(oldRows), oldRows)
	if len(oldRows) >= 3 {
		t.Logf("NOTE: pinned NornicDB build no longer reproduces the group-collapse bug for this shape; the scoped-query fix remains correct and is still the shipped contract")
	}

	handler := &PackageRegistryHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	post := func(target string) map[string]any {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Accept", EnvelopeMIMEType)
		rec := httptest.NewRecorder()
		handler.listPackages(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", target, rec.Code, rec.Body.String())
		}
		var env ResponseEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("%s decode envelope: %v", target, err)
		}
		data, _ := env.Data.(map[string]any)
		return data
	}

	data := post("/api/v0/package-registry/packages?ecosystem=" + ecosystem + "&limit=50")
	t.Logf("NEW shape response: %#v", data)
	rawPackages, _ := data["packages"].([]any)
	if got, want := len(rawPackages), 3; got != want {
		t.Fatalf("count = %d, want %d (both zero-version packages were silently dropped by the OLD shape); packages = %#v", got, want, rawPackages)
	}
	versionCountByID := make(map[string]int, len(rawPackages))
	for _, raw := range rawPackages {
		row, _ := raw.(map[string]any)
		versionCountByID[StringVal(row, "package_id")] = IntVal(row, "version_count")
	}
	for _, id := range []string{pkgNoVersionsA, pkgNoVersionsB} {
		vc, ok := versionCountByID[id]
		if !ok {
			t.Fatalf("package %s missing from response (the exact bug: zero-version packages vanish); packages = %#v", id, rawPackages)
		}
		if vc != 0 {
			t.Errorf("package %s version_count = %d, want 0", id, vc)
		}
	}
	vc, ok := versionCountByID[pkgTwoVersions]
	if !ok {
		t.Fatalf("package %s missing from response; packages = %#v", pkgTwoVersions, rawPackages)
	}
	if vc != 2 {
		t.Errorf("package %s version_count = %d, want 2", pkgTwoVersions, vc)
	}

	// Exact-identity lookup: a zero-version package must be resolvable by
	// package_id, not just present in an ecosystem scan. The old bug made a
	// bare packageID lookup on a zero-version package return EMPTY, which is
	// indistinguishable from "package does not exist".
	byIDData := post("/api/v0/package-registry/packages?package_id=" + pkgNoVersionsA + "&limit=10")
	byIDRaw, _ := byIDData["packages"].([]any)
	if len(byIDRaw) != 1 {
		t.Fatalf("exact package_id lookup for zero-version package returned %d rows, want 1: %#v", len(byIDRaw), byIDRaw)
	}
	byIDRow, _ := byIDRaw[0].(map[string]any)
	if got, want := StringVal(byIDRow, "package_id"), pkgNoVersionsA; got != want {
		t.Fatalf("by-id lookup package_id = %q, want %q", got, want)
	}
	if got := IntVal(byIDRow, "version_count"); got != 0 {
		t.Errorf("by-id lookup version_count = %d, want 0", got)
	}
}

// TestLivePackageRegistryScopedEcosystemBrowseReturnsZeroVersionPackages is
// the scoped-caller mirror of
// TestLivePackageRegistryListPackagesReturnsZeroVersionPackages: it proves
// packageRegistryPackagesScopedEcosystemCypher (the F-6/W5b tenant-scoped
// ecosystem-browse branch) does not reintroduce the OPTIONAL MATCH +
// count(v) row-collapse bug that packageRegistryPackagesCypher had. A
// public, zero-version package must survive a scoped caller's
// ecosystem-only browse with version_count: 0, not vanish from the page.
//
// Run: ESHU_PKG_REGISTRY_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:7687 \
//
//	go test ./internal/query -run TestLivePackageRegistryScopedEcosystemBrowseReturnsZeroVersionPackages -count=1 -v
func TestLivePackageRegistryScopedEcosystemBrowseReturnsZeroVersionPackages(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_PKG_REGISTRY_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_PKG_REGISTRY_PROVE_LIVE=1 to run the live package-registry version-count proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required (e.g. bolt://localhost:7687)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()

	write := func(cypher string, params map[string]any) {
		s := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: "nornic"})
		defer func() { _ = s.Close(ctx) }()
		if _, err := s.Run(ctx, cypher, params); err != nil {
			t.Fatalf("seed write failed: %v\ncypher=%s", err, cypher)
		}
	}
	reader := NewNeo4jReader(driver, "nornic")

	const ecosystem = "npm-live-5167-scoped-count-fix"
	const (
		pkgPublicNoVersions  = "pkg:live5167:scoped-no-versions"
		pkgPublicTwoVersions = "pkg:live5167:scoped-two-versions"
		pkgPrivateNoVersions = "pkg:live5167:scoped-private-no-versions"
	)
	cleanup := func() {
		write(`MATCH (p:Package {ecosystem: $ecosystem})-[:HAS_VERSION]->(v:PackageVersion) DETACH DELETE v`,
			map[string]any{"ecosystem": ecosystem})
		write(`MATCH (p:Package {ecosystem: $ecosystem}) DETACH DELETE p`,
			map[string]any{"ecosystem": ecosystem})
	}
	cleanup()
	defer cleanup()

	write(`CREATE (a:Package {uid: $a, ecosystem: $ecosystem, normalized_name: 'a', visibility: 'public'})
	       CREATE (b:Package {uid: $b, ecosystem: $ecosystem, normalized_name: 'b', visibility: 'public'})
	       CREATE (v1:PackageVersion {uid: $b1})
	       CREATE (v2:PackageVersion {uid: $b2})
	       CREATE (b)-[:HAS_VERSION]->(v1)
	       CREATE (b)-[:HAS_VERSION]->(v2)
	       CREATE (c:Package {uid: $c, ecosystem: $ecosystem, normalized_name: 'c', visibility: 'private'})`,
		map[string]any{
			"a": pkgPublicNoVersions, "b": pkgPublicTwoVersions, "c": pkgPrivateNoVersions,
			"ecosystem": ecosystem,
			"b1":        pkgPublicTwoVersions + ":1.0.0",
			"b2":        pkgPublicTwoVersions + ":2.0.0",
		})

	// Capture the OLD broken shape directly for evidence: the exact
	// OPTIONAL MATCH + count(v) composition
	// packageRegistryPackagesScopedEcosystemCypher used before this fix,
	// with the same combined-WHERE ecosystem+visibility predicate. Expected
	// (openCypher-correct): 2 rows (the 2 public packages), one with vc=0.
	// Observed on the pinned NornicDB build: collapses to 1 row, mirroring
	// the sibling unscoped-shape probe above.
	oldRows, err := reader.Run(ctx,
		`MATCH (p:Package) WHERE p.ecosystem = $ecosystem AND p.visibility = 'public' OPTIONAL MATCH (p)-[:HAS_VERSION]->(v:PackageVersion) RETURN p.uid AS id, count(v) AS vc ORDER BY p.uid`,
		map[string]any{"ecosystem": ecosystem})
	if err != nil {
		t.Fatalf("old-shape scoped-ecosystem probe query failed: %v", err)
	}
	t.Logf("OLD scoped-ecosystem OPTIONAL-MATCH+count(v) shape returned %d row(s) (want 2 — broken when < 2): %#v", len(oldRows), oldRows)
	if len(oldRows) >= 2 {
		t.Logf("NOTE: pinned NornicDB build no longer reproduces the group-collapse bug for this shape; the anchor-only + UNWIND-count fix remains correct and is still the shipped contract")
	}

	handler := &PackageRegistryHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages?ecosystem="+ecosystem+"&limit=50", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-live-5167",
		WorkspaceID:          "workspace-live-5167",
		AllowedScopeIDs:      []string{ecosystem},
		AllowedRepositoryIDs: []string{ecosystem},
	}))
	rec := httptest.NewRecorder()
	handler.listPackages(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var env ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	data, _ := env.Data.(map[string]any)
	rawPackages, _ := data["packages"].([]any)

	// Only the 2 PUBLIC packages must appear; the private one stays excluded
	// by the visibility='public' predicate.
	if got, want := len(rawPackages), 2; got != want {
		t.Fatalf("count = %d, want %d (a zero-version public package was dropped, or a private package leaked); packages = %#v", got, want, rawPackages)
	}
	versionCountByID := make(map[string]int, len(rawPackages))
	seenIDs := make(map[string]bool, len(rawPackages))
	for _, raw := range rawPackages {
		row, _ := raw.(map[string]any)
		id := StringVal(row, "package_id")
		seenIDs[id] = true
		versionCountByID[id] = IntVal(row, "version_count")
	}
	if seenIDs[pkgPrivateNoVersions] {
		t.Fatalf("scoped ecosystem browse leaked the private package: %#v", rawPackages)
	}
	vc, ok := versionCountByID[pkgPublicNoVersions]
	if !ok {
		t.Fatalf("public zero-version package %s missing from scoped browse (the exact bug: OPTIONAL MATCH + count(v) drops zero-match rows); packages = %#v", pkgPublicNoVersions, rawPackages)
	}
	if vc != 0 {
		t.Errorf("package %s version_count = %d, want 0", pkgPublicNoVersions, vc)
	}
	vc, ok = versionCountByID[pkgPublicTwoVersions]
	if !ok {
		t.Fatalf("public package %s missing from scoped browse; packages = %#v", pkgPublicTwoVersions, rawPackages)
	}
	if vc != 2 {
		t.Errorf("package %s version_count = %d, want 2", pkgPublicTwoVersions, vc)
	}
}
