// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/package/registry"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const liveBundlesEcosystem = "npm-live-5167-bundles"

// liveBundlesPackage is one seeded :Package fixture row.
type liveBundlesPackage struct {
	uid, name, visibility string
	versions              int
}

// liveBundlesFixture returns 30 packages named pkg-00..pkg-29 in a scrambled
// insertion order. Visibility cycles public / private / absent so every
// scoped-caller filter branch has rows to drop, and the version count cycles
// 0/1/2 so a zero-version package sits inside every page.
func liveBundlesFixture() []liveBundlesPackage {
	all := make([]liveBundlesPackage, 0, 30)
	for i := 0; i < 30; i++ {
		visibility := []string{"public", "private", ""}[i%3]
		all = append(all, liveBundlesPackage{
			uid:        fmt.Sprintf("pkg:live5167b:%02d", i),
			name:       fmt.Sprintf("pkg-%02d", i),
			visibility: visibility,
			versions:   i % 3,
		})
	}
	// Scramble insertion order (stride 7 is coprime with 30) so unordered
	// storage order cannot accidentally equal (name, uid) order.
	scrambled := make([]liveBundlesPackage, 0, len(all))
	for i := 0; i < len(all); i++ {
		scrambled = append(scrambled, all[(i*7)%len(all)])
	}
	return scrambled
}

// liveBundlesRecordingReader wraps the live reader and records the row count
// each statement returned, so a test can assert the anchor read returned
// exactly limit+1 rows (the bounded page) rather than the whole catalog.
type liveBundlesRecordingReader struct {
	inner *liveNornicDBReader
	rows  []int
	// write runs one seed statement through a write session.
	write func(cypher string, params map[string]any)
}

func (r *liveBundlesRecordingReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	rows, err := r.inner.Run(ctx, cypher, params)
	r.rows = append(r.rows, len(rows))
	return rows, err
}

func (r *liveBundlesRecordingReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	return r.inner.RunSingle(ctx, cypher, params)
}

// openLiveBundlesReader connects to the env-selected NornicDB, seeds the
// fixture, and returns the recording reader. Cleanup removes the fixture.
func openLiveBundlesReader(t *testing.T) *liveBundlesRecordingReader {
	t.Helper()
	if strings.TrimSpace(os.Getenv("ESHU_PKG_REGISTRY_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_PKG_REGISTRY_PROVE_LIVE=1 to run the live bundle-search proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required (e.g. bolt://localhost:17994)")
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	write := func(cypher string, params map[string]any) {
		// A fresh context per write: t.Cleanup runs after this helper returns.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: "nornic"})
		defer func() { _ = s.Close(ctx) }()
		if _, err := s.Run(ctx, cypher, params); err != nil {
			t.Fatalf("seed write failed: %v\ncypher=%s", err, cypher)
		}
	}
	clean := func() {
		write(`MATCH (v:PackageVersion) WHERE v.uid STARTS WITH 'ver:live5167b:' DETACH DELETE v`, nil)
		write(`MATCH (p:Package {ecosystem: $e}) DETACH DELETE p`, map[string]any{"e": liveBundlesEcosystem})
	}
	clean()
	t.Cleanup(clean)
	for _, pkg := range liveBundlesFixture() {
		props := map[string]any{"uid": pkg.uid, "name": pkg.name, "e": liveBundlesEcosystem}
		set := `SET p.normalized_name = $name, p.ecosystem = $e, p.registry = 'live', p.namespace = '', p.purl = 'pkg:npm/' + $name`
		if pkg.visibility != "" {
			set += `, p.visibility = $visibility`
			props["visibility"] = pkg.visibility
		}
		write(`MERGE (p:Package {uid: $uid}) `+set, props)
		for v := 0; v < pkg.versions; v++ {
			write(`MATCH (p:Package {uid: $uid}) CREATE (p)-[:HAS_VERSION]->(:PackageVersion {uid: $vuid, package_id: $uid, version: $version})`,
				map[string]any{"uid": pkg.uid, "vuid": fmt.Sprintf("ver:live5167b:%s:%d", pkg.uid, v), "version": fmt.Sprintf("1.0.%d", v)})
		}
	}
	return &liveBundlesRecordingReader{inner: newLiveNornicDBReader(driver, "nornic"), write: write}
}

// liveBundlesSearch drives the shipped handler and decodes the bundles page.
func liveBundlesSearch(t *testing.T, reader GraphQuery, body string, auth *AuthContext) (bundles []map[string]any, truncated bool) {
	t.Helper()
	h := &CodeHandler{Neo4j: reader}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/bundles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	if auth != nil {
		req = req.WithContext(ContextWithAuthContext(req.Context(), *auth))
	}
	w := httptest.NewRecorder()
	h.handleSearchBundles(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var envelope struct {
		Data struct {
			Bundles   []map[string]any `json:"bundles"`
			Truncated bool             `json:"truncated"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	return envelope.Data.Bundles, envelope.Data.Truncated
}

// liveBundlesExpected returns the fixture rows the caller may see, in the
// (ecosystem, name, uid) order the API contract promises.
func liveBundlesExpected(publicOnly bool) []liveBundlesPackage {
	var want []liveBundlesPackage
	for _, pkg := range liveBundlesFixture() {
		if publicOnly && pkg.visibility != "public" {
			continue
		}
		want = append(want, pkg)
	}
	sort.Slice(want, func(i, j int) bool {
		if want[i].name != want[j].name {
			return want[i].name < want[j].name
		}
		return want[i].uid < want[j].uid
	})
	return want
}

// TestLiveSearchBundlesHonoursOrderByAndLimit is the #5167 regression for the
// ordering/LIMIT bug: on the pinned NornicDB build the old statement silently
// ignored ORDER BY and LIMIT after `WITH p, count(v)`, so LIMIT 6 returned the
// whole catalog and the handler's rows[:limit] cut an arbitrary page.
//
//	Run: ESHU_PKG_REGISTRY_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17994 \
//		go test ./internal/query/codequery -run TestLiveSearchBundles -count=1 -v
func TestLiveSearchBundlesHonoursOrderByAndLimit(t *testing.T) {
	reader := openLiveBundlesReader(t)
	const limit = 5
	bundles, truncated := liveBundlesSearch(t, reader, fmt.Sprintf(`{"ecosystem": %q, "limit": %d}`, liveBundlesEcosystem, limit), nil)

	if len(reader.rows) == 0 || reader.rows[0] != limit+1 {
		t.Errorf("anchor statement returned %v rows, want exactly limit+1 = %d (the catalog holds 30)", reader.rows, limit+1)
	}
	if !truncated {
		t.Errorf("truncated = false, want true (30 packages, limit %d)", limit)
	}
	want := liveBundlesExpected(false)[:limit]
	if len(bundles) != limit {
		t.Fatalf("len(bundles) = %d, want %d", len(bundles), limit)
	}
	for i, row := range bundles {
		if got := StringVal(row, "package_id"); got != want[i].uid {
			t.Errorf("bundles[%d].package_id = %q, want %q (name order)", i, got, want[i].uid)
		}
		if got := IntVal(row, "version_count"); got != want[i].versions {
			t.Errorf("bundles[%d] %s version_count = %d, want %d", i, want[i].uid, got, want[i].versions)
		}
	}
}

// TestLiveSearchBundlesScopedCallerSeesOnlyPublicPackages proves the scoped
// caller's grant boundary against real rows: private and visibility-absent
// packages never appear, the page is still ordered and bounded, and version
// counts stay exact.
func TestLiveSearchBundlesScopedCallerSeesOnlyPublicPackages(t *testing.T) {
	reader := openLiveBundlesReader(t)
	auth := testutil.CodeGrantScopedAuthContext([]string{"repo://tenant-a/svc"})
	public := liveBundlesExpected(true)

	// Unbounded-enough page: every public row, none of the others.
	bundles, truncated := liveBundlesSearch(t, reader, fmt.Sprintf(`{"ecosystem": %q, "limit": 200}`, liveBundlesEcosystem), &auth)
	if truncated {
		t.Fatalf("truncated = true, want false for a 200-row page over %d public rows", len(public))
	}
	if len(bundles) != len(public) {
		t.Fatalf("scoped caller saw %d packages, want exactly the %d public ones", len(bundles), len(public))
	}
	for i, row := range bundles {
		if got := StringVal(row, "package_id"); got != public[i].uid {
			t.Errorf("bundles[%d].package_id = %q, want %q; a non-public package leaked or order broke", i, got, public[i].uid)
		}
		if got := IntVal(row, "version_count"); got != public[i].versions {
			t.Errorf("bundles[%d] %s version_count = %d, want %d", i, public[i].uid, got, public[i].versions)
		}
	}

	// A query term that would match private/absent rows must still hide them.
	bundles, _ = liveBundlesSearch(t, reader, `{"query": "PKG-", "limit": 200}`, &auth)
	for _, row := range bundles {
		id := StringVal(row, "package_id")
		if strings.HasPrefix(id, "pkg:live5167b:") && !containsUID(public, id) {
			t.Errorf("scoped query search leaked non-public package %s", id)
		}
	}

	// A bounded scoped page keeps the limit+1 contract on the public subset.
	reader.rows = nil
	bundles, truncated = liveBundlesSearch(t, reader, fmt.Sprintf(`{"ecosystem": %q, "limit": 4}`, liveBundlesEcosystem), &auth)
	if reader.rows[0] != 5 || !truncated || len(bundles) != 4 {
		t.Fatalf("scoped limit 4: anchor rows = %v, truncated = %v, len = %d; want 5 rows, true, 4", reader.rows, truncated, len(bundles))
	}
}

func containsUID(rows []liveBundlesPackage, uid string) bool {
	for _, row := range rows {
		if row.uid == uid {
			return true
		}
	}
	return false
}

// TestLiveSearchBundlesEveryPredicateFiltersRows proves each WHERE predicate
// by live row membership, not statement text: the pinned NornicDB silently
// ignores a syntactically invalid WHERE and returns every row, so a predicate
// that is merely present proves nothing.
func TestLiveSearchBundlesEveryPredicateFiltersRows(t *testing.T) {
	reader := openLiveBundlesReader(t)
	auth := testutil.CodeGrantScopedAuthContext([]string{"repo://tenant-a/svc"})
	ids := func(rows []map[string]any) []string {
		out := make([]string, 0, len(rows))
		for _, row := range rows {
			if id := StringVal(row, "package_id"); strings.HasPrefix(id, "pkg:live5167b:") {
				out = append(out, id)
			}
		}
		return out
	}
	cases := []struct {
		name string
		body string
		auth *AuthContext
		want int
	}{
		{"ecosystem only", fmt.Sprintf(`{"ecosystem": %q, "limit": 200}`, liveBundlesEcosystem), nil, 30},
		{"other ecosystem", `{"ecosystem": "npm-live-5167-absent", "limit": 200}`, nil, 0},
		{"query narrows: pkg-0 prefix", fmt.Sprintf(`{"query": "PKG-0", "ecosystem": %q, "limit": 200}`, liveBundlesEcosystem), nil, 10},
		{"query matches purl", fmt.Sprintf(`{"query": "pkg:npm/pkg-1", "ecosystem": %q, "limit": 200}`, liveBundlesEcosystem), nil, 10},
		{"query matches nothing", fmt.Sprintf(`{"query": "zzz-none", "ecosystem": %q, "limit": 200}`, liveBundlesEcosystem), nil, 0},
		{"unique_only keeps every row", fmt.Sprintf(`{"ecosystem": %q, "unique_only": true, "limit": 200}`, liveBundlesEcosystem), nil, 30},
		{"scoped + query keeps public only", fmt.Sprintf(`{"query": "pkg-0", "ecosystem": %q, "limit": 200}`, liveBundlesEcosystem), &auth, 4},
		{"scoped + other ecosystem", `{"ecosystem": "npm-live-5167-absent", "limit": 200}`, &auth, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bundles, _ := liveBundlesSearch(t, reader, tc.body, tc.auth)
			if got := ids(bundles); len(got) != tc.want {
				t.Fatalf("rows = %d %v, want %d", len(got), got, tc.want)
			}
		})
	}
}

// TestLiveSearchBundlesVersionCountEqualsEdgeCount proves the index-backed
// count statement (PackageVersion.package_id) agrees with a HAS_VERSION edge
// count on every fixture package once both writer phases have committed, and
// pins the one window where they differ: a version node written by the node
// phase whose deferred HAS_VERSION edge is not written yet counts by property
// but not by edge (package_registry_edge_writer.go).
func TestLiveSearchBundlesVersionCountEqualsEdgeCount(t *testing.T) {
	reader := openLiveBundlesReader(t)
	ctx := context.Background()
	fixture := liveBundlesFixture()
	ids := make([]string, 0, len(fixture))
	for _, pkg := range fixture {
		ids = append(ids, pkg.uid)
	}
	edgeCounts := func() map[string]int {
		rows, err := reader.inner.Run(ctx, `UNWIND $ids AS id
MATCH (p:Package {uid: id})-[r:HAS_VERSION]->(v:PackageVersion)
RETURN p.uid AS package_id, count(r) AS version_count`, map[string]any{"ids": ids})
		if err != nil {
			t.Fatalf("edge count read: %v", err)
		}
		out := map[string]int{}
		for _, row := range rows {
			out[StringVal(row, "package_id")] = IntVal(row, "version_count")
		}
		return out
	}
	propCounts, err := registry.VersionCountsByPackageID(ctx, reader, ids)
	if err != nil {
		t.Fatalf("VersionCountsByPackageID: %v", err)
	}
	edges := edgeCounts()
	for _, pkg := range fixture {
		if propCounts[pkg.uid] != edges[pkg.uid] || propCounts[pkg.uid] != pkg.versions {
			t.Errorf("%s: property count %d, edge count %d, seeded %d; want all equal", pkg.uid, propCounts[pkg.uid], edges[pkg.uid], pkg.versions)
		}
	}
	// Node-phase-only window: a version with package_id but no edge yet.
	reader.write(`CREATE (:PackageVersion {uid: 'ver:live5167b:no-edge', package_id: $id, version: '9.9.9'})`, map[string]any{"id": ids[0]})
	propCounts, _ = registry.VersionCountsByPackageID(ctx, reader, ids[:1])
	if got, want := propCounts[ids[0]], edgeCounts()[ids[0]]+1; got != want {
		t.Errorf("no-edge window: property count %d, want edge count + 1 = %d", got, want)
	}
}
