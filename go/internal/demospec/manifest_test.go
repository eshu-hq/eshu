// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package demospec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestDemoFirstAnswers is the failing-test-first referential-integrity oracle
// for issue #4741. It loads specs/demo-first-answers.v1.yaml and fails when
// any referenced artifact (cassette family, fixture repo, playbook ID, or
// MCP/CLI/HTTP query-shape key) does not exist, so the manifest can never
// silently drift from the corpus and read surfaces it claims to pin down.
func TestDemoFirstAnswers(t *testing.T) {
	root := moduleRoot(t)
	manifestPath := filepath.Join(root, "specs", ManifestFileName)

	m, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest(%s): %v", manifestPath, err)
	}

	if got, want := len(m.Questions), requiredQuestionCount; got != want {
		t.Fatalf("manifest declares %d questions, want exactly %d", got, want)
	}

	playbookIDs := loadPlaybookIDs()
	snapshot := loadGoldenSnapshot(t, root)
	correlationIDs := snapshot.requiredCorrelationIDs()

	var demonstrated []string
	for _, q := range m.Questions {
		t.Run(q.ID, func(t *testing.T) {
			assertArtifactsExist(t, root, q)
			assertSurfaceResolves(t, q, playbookIDs, snapshot)
			assertExecuteResolves(t, q, snapshot)
		})
		demonstrated = append(demonstrated, q.ExpectedAnswer.DemonstratesCorrelations...)
	}

	t.Run("correlation_coverage", func(t *testing.T) {
		if len(demonstrated) == 0 {
			t.Fatal("no question demonstrates any correlation; the manifest must exercise at least one required_correlations id")
		}
		for _, id := range demonstrated {
			if _, ok := correlationIDs[id]; !ok {
				t.Errorf("question demonstrates correlation %q, which is not in testdata/golden/e2e-20repo-snapshot.json graph.required_correlations", id)
			}
		}
	})
}

// assertArtifactsExist fails the test if any cassette family or fixture repo
// a question depends on does not exist on disk.
func assertArtifactsExist(t *testing.T, root string, q Question) {
	t.Helper()
	for _, family := range q.Artifacts.Cassettes {
		cassettePath := filepath.Join(root, "testdata", "cassettes", family, "supply-chain-demo.json")
		if _, err := os.Stat(cassettePath); err != nil {
			t.Errorf("question %s: missing cassette artifact %s: %v", q.ID, cassettePath, err)
		}
	}
	for _, repo := range q.Artifacts.Repos {
		repoPath := filepath.Join(root, "tests", "fixtures", "ecosystems", repo)
		if info, err := os.Stat(repoPath); err != nil {
			t.Errorf("question %s: missing fixture repo %s: %v", q.ID, repoPath, err)
		} else if !info.IsDir() {
			t.Errorf("question %s: fixture repo path %s is not a directory", q.ID, repoPath)
		}
	}
}

// assertSurfaceResolves fails the test if a question's surface ref does not
// resolve to a real playbook ID (kind=playbook) or a real key in the golden
// snapshot's query_shapes registry (kind=mcp/cli/http).
func assertSurfaceResolves(t *testing.T, q Question, playbookIDs map[string]struct{}, snapshot goldenSnapshot) {
	t.Helper()
	switch q.Surface.Kind {
	case SurfaceKindPlaybook:
		if _, ok := playbookIDs[q.Surface.Ref]; !ok {
			t.Errorf("question %s: surface ref %q is not a playbook id in query.PlaybookCatalog()", q.ID, q.Surface.Ref)
		}
	case SurfaceKindMCP:
		shape, ok := snapshot.QueryShapes.MCP[q.Surface.Ref]
		if !ok {
			t.Errorf("question %s: surface ref %q is not a key in golden snapshot query_shapes.mcp", q.ID, q.Surface.Ref)
			return
		}
		assertArgumentKeysProven(t, q, shape)
	case SurfaceKindCLI:
		if _, ok := snapshot.QueryShapes.CLI[q.Surface.Ref]; !ok {
			t.Errorf("question %s: surface ref %q is not a key in golden snapshot query_shapes.cli", q.ID, q.Surface.Ref)
		}
	case SurfaceKindHTTP:
		if !httpRouteResolves(q.Surface.Ref, snapshot.QueryShapes.HTTP) {
			t.Errorf("question %s: surface ref %q does not match method+path prefix of any key in golden snapshot query_shapes.http", q.ID, q.Surface.Ref)
		}
	default:
		t.Errorf("question %s: unhandled surface kind %q", q.ID, q.Surface.Kind)
	}
}

// assertExecuteResolves fails the test when a question's surface.execute target
// (the concrete callable the demo-answers golden-gate phase runs to fetch the
// live answer) does not resolve to a real MCP tool or HTTP route in the golden
// snapshot. A playbook question must carry one (the loader enforces presence);
// mcp/http questions may omit it because their own surface is executable.
func assertExecuteResolves(t *testing.T, q Question, snapshot goldenSnapshot) {
	t.Helper()
	ex := q.Surface.Execute
	if ex == nil {
		return
	}
	switch ex.Kind {
	case SurfaceKindMCP:
		if _, ok := snapshot.QueryShapes.MCP[ex.Ref]; !ok {
			t.Errorf("question %s: surface.execute ref %q is not a key in golden snapshot query_shapes.mcp", q.ID, ex.Ref)
		}
	case SurfaceKindHTTP:
		if !httpRouteResolves(ex.Ref, snapshot.QueryShapes.HTTP) {
			t.Errorf("question %s: surface.execute ref %q does not match method+path prefix of any golden snapshot query_shapes.http route", q.ID, ex.Ref)
		}
	default:
		t.Errorf("question %s: surface.execute has unhandled kind %q (want mcp or http)", q.ID, ex.Kind)
	}
}

// assertArgumentKeysProven fails the test when an mcp question pins a call
// argument whose key the golden snapshot's query_shape for that tool does not
// declare. The referential-integrity contract is "no unproven surface"; that
// must cover the arguments too, not only the tool name — otherwise a manifest
// could pin a typo'd or unrecognized argument (e.g. `pkg_id` instead of
// `package_id`) against a real tool and still stay green. The check is
// key-level, not value-level: it allows a demo question to use a bounded
// subset of the shape's arguments (or a different value), but rejects an
// argument key the proven shape has never exercised. When the snapshot shape
// declares no example arguments, there is nothing to validate against and the
// check is skipped.
func assertArgumentKeysProven(t *testing.T, q Question, shape json.RawMessage) {
	t.Helper()
	var parsed struct {
		Arguments map[string]json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(shape, &parsed); err != nil {
		t.Errorf("question %s: cannot parse golden snapshot query_shape for %q: %v", q.ID, q.Surface.Ref, err)
		return
	}
	if len(parsed.Arguments) == 0 {
		return
	}
	known := make([]string, 0, len(parsed.Arguments))
	for k := range parsed.Arguments {
		known = append(known, k)
	}
	sort.Strings(known)
	for key := range q.Surface.Arguments {
		if _, ok := parsed.Arguments[key]; !ok {
			t.Errorf(
				"question %s: surface %q pins argument %q, which the golden snapshot query_shape for that tool does not declare (known arguments: %v); the manifest must not pin an argument the proven shape has never exercised",
				q.ID, q.Surface.Ref, key, known,
			)
		}
	}
}

// httpRouteResolves reports whether ref matches an HTTP query-shape key by
// method and path prefix, ignoring the querystring. The golden snapshot's
// http keys are literally "METHOD /path?query=..."; a manifest ref that
// carries a different (or no) querystring for the same method+path is still
// considered resolved, matching the referential-integrity contract's
// "match on method+path prefix" rule for HTTP surfaces.
func httpRouteResolves(ref string, httpShapes map[string]json.RawMessage) bool {
	refMethod, refPath := splitHTTPKey(ref)
	if refMethod == "" || refPath == "" {
		return false
	}
	for key := range httpShapes {
		method, path := splitHTTPKey(key)
		if method == refMethod && path == refPath {
			return true
		}
	}
	return false
}

// splitHTTPKey splits an HTTP query-shape key ("METHOD /path?query") into its
// method and path-without-querystring components.
func splitHTTPKey(key string) (method, path string) {
	parts := strings.SplitN(key, " ", 2)
	if len(parts) != 2 {
		return "", ""
	}
	method = parts[0]
	path = parts[1]
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	return method, path
}

// loadPlaybookIDs returns the set of playbook IDs query.PlaybookCatalog()
// declares, the proven-surface registry for kind=playbook manifest entries.
func loadPlaybookIDs() map[string]struct{} {
	ids := make(map[string]struct{})
	for _, pb := range query.PlaybookCatalog() {
		ids[pb.ID] = struct{}{}
	}
	return ids
}

// goldenSnapshot is the minimal subset of testdata/golden/e2e-20repo-snapshot.json
// this test needs: the query-shape registries the gate proves are live, and
// the required_correlations id list.
type goldenSnapshot struct {
	QueryShapes struct {
		MCP  map[string]json.RawMessage `json:"mcp"`
		CLI  map[string]json.RawMessage `json:"cli"`
		HTTP map[string]json.RawMessage `json:"http"`
	} `json:"query_shapes"`
	Graph struct {
		RequiredCorrelations []struct {
			ID string `json:"id"`
		} `json:"required_correlations"`
	} `json:"graph"`
}

// requiredCorrelationIDs returns the set of rc-NN ids the golden snapshot
// requires, so a manifest question can only claim to demonstrate a
// correlation the gate actually asserts.
func (s goldenSnapshot) requiredCorrelationIDs() map[string]struct{} {
	ids := make(map[string]struct{}, len(s.Graph.RequiredCorrelations))
	for _, rc := range s.Graph.RequiredCorrelations {
		ids[rc.ID] = struct{}{}
	}
	return ids
}

// loadGoldenSnapshot reads and parses the B-12 golden snapshot from
// testdata/golden/e2e-20repo-snapshot.json under root.
func loadGoldenSnapshot(t *testing.T, root string) goldenSnapshot {
	t.Helper()
	path := filepath.Join(root, "testdata", "golden", "e2e-20repo-snapshot.json")
	raw, err := os.ReadFile(path) // #nosec G304 -- repo-owned golden snapshot path, not external input
	if err != nil {
		t.Fatalf("read golden snapshot %s: %v", path, err)
	}
	var snapshot goldenSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("parse golden snapshot %s: %v", path, err)
	}
	return snapshot
}

// moduleRoot walks upward from this test file until it finds the repository
// root (identified by the specs/ and testdata/golden/ directories sitting
// alongside go/), so the test resolves specs/ and testdata/ paths correctly
// regardless of where `go test` is invoked from. specs/ lives outside the Go
// module (go/go.mod), so go:embed cannot reach it; this mirrors moduleRoot in
// go/internal/graph/edgetype/coverage_schema_test.go, extended one level up
// past go.mod to the repository root that contains specs/.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate repository root")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "specs", ManifestFileName)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("reached filesystem root without finding specs/" + ManifestFileName)
		}
		dir = parent
	}
}
