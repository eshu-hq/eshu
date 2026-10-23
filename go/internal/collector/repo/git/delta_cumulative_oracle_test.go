// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// oracleDashboard is a Grafana dashboard resource the YAML parser turns into
// observability facts keyed by generation (collector/repo/git/observability).
const oracleDashboard = `apiVersion: grafana.integreatly.org/v1beta1
kind: GrafanaDashboard
metadata:
  name: checkout-latency
  namespace: observability
spec:
  folder: checkout
  json: |
    {"uid": "checkout-latency-%s", "title": "Checkout %s", "panels": []}
`

// oracleRepo is the #8780 AC2 matrix on a real repository:
//
//   - F: a, b, c, d (Go files with one function each), the dashboard, and
//     ten untouched Go fillers.
//   - c1 (the active delta d1): modify a, add e, delete d.
//   - C (the cumulative delta d2): revert a to F's bytes, delete e, modify b,
//     add f, re-add d with F's bytes, change the dashboard.
type oracleRepo struct {
	path               string
	full, active, head string
}

func goFile(name, body string) string {
	return "package p\n\nfunc " + name + "() {}\n" + body
}

func newOracleRepo(t *testing.T) oracleRepo {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_AUTHOR_NAME", "eshu")
	t.Setenv("GIT_AUTHOR_EMAIL", "eshu@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "eshu")
	t.Setenv("GIT_COMMITTER_EMAIL", "eshu@example.com")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	r := oracleRepo{path: filepath.Join(root, "repo")}
	runGit(t, root, "init", "-q", "-b", "main", r.path)
	write := func(name, content string) { writeFixtureFile(t, filepath.Join(r.path, name), content) }
	commit := func(message string) string {
		runGit(t, r.path, "add", "-A")
		runGit(t, r.path, "commit", "-qm", message)
		return runGit(t, r.path, "rev-parse", "HEAD")
	}
	for _, name := range []string{"a", "b", "c", "d"} {
		write(name+".go", goFile(name, ""))
	}
	writeFixtureFile(t, filepath.Join(r.path, "dashboard.yaml"), fmt.Sprintf(oracleDashboard, "f", "F"))
	for i := range 10 {
		write(fmt.Sprintf("g%d.go", i), goFile(fmt.Sprintf("g%d", i), ""))
	}
	r.full = commit("F")

	write("a.go", goFile("aChanged", ""))
	write("e.go", goFile("e", ""))
	runGit(t, r.path, "rm", "-q", "d.go")
	r.active = commit("c1")

	write("a.go", goFile("a", ""))
	runGit(t, r.path, "rm", "-q", "e.go")
	write("b.go", goFile("bChanged", ""))
	write("f.go", goFile("f", ""))
	write("d.go", goFile("d", ""))
	writeFixtureFile(t, filepath.Join(r.path, "dashboard.yaml"), fmt.Sprintf(oracleDashboard, "c", "C"))
	r.head = commit("C")
	return r
}

// oracleGeneration is one collected generation drained into memory.
type oracleGeneration struct {
	scope      scope.IngestionScope
	generation scope.ScopeGeneration
	facts      []facts.Envelope
	delta      GitSyncDelta
}

// collect checks out commit and runs the real selection, native snapshotter,
// and fact builder over it. A non-empty fullCommit makes it a cumulative
// delta from fullCommit with activeCommit as its fence commit.
func (r oracleRepo) collect(t *testing.T, commit, activeCommit, fullCommit string) oracleGeneration {
	t.Helper()
	runGit(t, r.path, "checkout", "-q", commit)
	config := RepoSyncConfig{SourceMode: "explicit", ReposDir: filepath.Dir(r.path), GitAuthMethod: "none"}
	deltas := map[string]GitSyncDelta{}
	var delta GitSyncDelta
	if fullCommit != "" {
		var reason string
		var err error
		delta, reason, err = cumulativeGitDelta(context.Background(), config, r.path, "", activeCommit, fullCommit, commit)
		if err != nil || reason != "" {
			t.Fatalf("cumulativeGitDelta(%s..%s) = reason %q, %v", fullCommit, commit, reason, err)
		}
		delta.BaselineCommitSHA, delta.UnionBaseCommitSHA = activeCommit, fullCommit
		deltas[r.path] = delta
	}
	selected := buildSelectedRepositories(config, []string{r.path}, deltas, nil, map[string]string{r.path: commit}, nil, nil, nil)
	if len(selected) != 1 {
		t.Fatalf("selected = %d repositories, want 1", len(selected))
	}
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() = %v", err)
	}
	snapshot, err := (NativeRepositorySnapshotter{Engine: engine}).SnapshotRepository(context.Background(), selected[0])
	if err != nil {
		t.Fatalf("SnapshotRepository(%s) = %v", commit, err)
	}
	observed := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	built := buildStreamingGeneration(r.path, testCollectorRepositoryMetadata(r.path), "run-"+commit, observed, snapshot, false, "")
	return oracleGeneration{scope: built.Scope, generation: built.Generation, facts: drainFactChannel(built.Facts), delta: delta}
}

// oracleView is the generation-independent shape of a fact set the AC2
// oracle compares: observability rows are keyed by generation, so they are
// compared as (kind, path) counts, never by stable key.
type oracleView struct {
	files         []string
	contentDigest map[string]string
	entityKeys    map[string][]string
	observability map[string]int
	other         map[string]bool
}

func viewOf(envelopes []facts.Envelope) oracleView {
	v := oracleView{
		contentDigest: map[string]string{}, entityKeys: map[string][]string{},
		observability: map[string]int{}, other: map[string]bool{},
	}
	for _, env := range envelopes {
		if env.IsTombstone {
			continue
		}
		path, _ := env.Payload["relative_path"].(string)
		switch {
		case env.FactKind == "file":
			v.files = append(v.files, path)
		case env.FactKind == "content":
			contentPath, _ := env.Payload["content_path"].(string)
			v.contentDigest[contentPath], _ = env.Payload["content_digest"].(string)
		case env.FactKind == "content_entity":
			v.entityKeys[path] = append(v.entityKeys[path], env.StableFactKey)
		case strings.HasPrefix(env.FactKind, "observability."):
			v.observability[env.FactKind+"|"+path]++
		case env.FactKind == "repository":
			// The union keeps the delta's repository fact (its delta paths
			// drive file-scoped retracts), so its payload differs by design.
		default:
			v.other[env.FactKind+"|"+env.StableFactKey] = true
		}
	}
	sort.Strings(v.files)
	for path := range v.entityKeys {
		sort.Strings(v.entityKeys[path])
	}
	return v
}

// TestCumulativeDeltaUnionMatchesFullAtHead is the #8780 AC2 oracle: the
// cumulative delta overlaid on its union base must hold the same facts as a
// full generation built at the same head by the same builders.
func TestCumulativeDeltaUnionMatchesFullAtHead(t *testing.T) {
	r := newOracleRepo(t)
	base := r.collect(t, r.full, "", "")
	_ = r.collect(t, r.active, r.full, r.full)
	d2 := r.collect(t, r.head, r.active, r.full)
	reference := r.collect(t, r.head, "", "")

	changed := make([]string, 0, len(d2.delta.ChangedFileTargets))
	for _, target := range d2.delta.ChangedFileTargets {
		changed = append(changed, filepath.Base(target))
	}
	if want := []string{"b.go", "dashboard.yaml", "f.go"}; !slices.Equal(changed, want) {
		t.Fatalf("d2 changed = %v, want %v", changed, want)
	}
	if want := []string{"e.go"}; !slices.Equal(d2.delta.DeletedRelativePaths, want) {
		t.Fatalf("d2 deleted = %v, want %v (d.go is back with F's bytes)", d2.delta.DeletedRelativePaths, want)
	}

	union := facts.OverlayDeltaBase(base.facts, d2.facts, facts.ParseDeltaScope(d2.facts))
	got, want := viewOf(union), viewOf(reference.facts)
	// The full at C has 16 files (a-d, f, ten fillers, the dashboard), each
	// with content entities, the dashboard's observability rows, and
	// repository-scoped follow-ups; an empty side would make every
	// comparison below vacuous.
	if len(want.files) != 16 || len(want.entityKeys) != 16 || len(want.observability) == 0 || len(want.other) == 0 {
		t.Fatalf("full at C view = %d files, %d entity paths, %d observability, %d other keys; want 16, 16, >0, >0",
			len(want.files), len(want.entityKeys), len(want.observability), len(want.other))
	}
	if !slices.Equal(got.files, want.files) {
		t.Fatalf("union files = %v, full at C = %v", got.files, want.files)
	}
	if !maps.Equal(got.contentDigest, want.contentDigest) {
		t.Fatalf("union content digests = %v, full at C = %v", got.contentDigest, want.contentDigest)
	}
	if !maps.EqualFunc(got.entityKeys, want.entityKeys, slices.Equal[[]string]) {
		t.Fatalf("union content_entity keys = %v, full at C = %v", got.entityKeys, want.entityKeys)
	}
	if !maps.Equal(got.observability, want.observability) {
		t.Fatalf("union observability rows by (kind, path) = %v, full at C = %v", got.observability, want.observability)
	}
	// The collector withholds full-only follow-ups from a delta
	// (fact_builder.go: the delta returns before them), but the union keeps
	// the base's rows under the same repository-scoped keys, so no stable key
	// may differ.
	for key := range want.other {
		if !got.other[key] {
			t.Errorf("union lacks %s, which the full at C carries", key)
		}
	}
	for key := range got.other {
		if !want.other[key] {
			t.Errorf("union carries %s, which the full at C does not", key)
		}
	}

	deltaGeneration := d2.generation
	deltaGeneration.DeltaUnionBaseGenerationID = base.generation.GenerationID
	unionMat, _, err := canonical.BuildMaterialization(d2.scope, deltaGeneration, union)
	if err != nil {
		t.Fatalf("BuildMaterialization(union) = %v", err)
	}
	refMat, _, err := canonical.BuildMaterialization(reference.scope, reference.generation, reference.facts)
	if err != nil {
		t.Fatalf("BuildMaterialization(full at C) = %v", err)
	}
	if g, w := materializedFiles(unionMat), materializedFiles(refMat); !slices.Equal(g, w) {
		t.Fatalf("materialized files = %v, full at C = %v", g, w)
	}
	if g, w := materializedEntities(unionMat), materializedEntities(refMat); !maps.EqualFunc(g, w, slices.Equal[[]string]) {
		t.Fatalf("materialized entities by path = %v, full at C = %v", g, w)
	}
	if g, w := materializedDirectories(unionMat), materializedDirectories(refMat); !slices.Equal(g, w) {
		t.Fatalf("materialized directories = %v, full at C = %v", g, w)
	}
}

func materializedFiles(mat canonical.CanonicalMaterialization) []string {
	out := make([]string, 0, len(mat.Files))
	for _, file := range mat.Files {
		out = append(out, file.RelativePath)
	}
	sort.Strings(out)
	return out
}

func materializedEntities(mat canonical.CanonicalMaterialization) map[string][]string {
	out := map[string][]string{}
	for _, entity := range mat.Entities {
		out[entity.RelativePath] = append(out[entity.RelativePath], entity.EntityName)
	}
	for path := range out {
		sort.Strings(out[path])
	}
	return out
}

func materializedDirectories(mat canonical.CanonicalMaterialization) []string {
	out := make([]string, 0, len(mat.Directories))
	for _, directory := range mat.Directories {
		out = append(out, directory.Path)
	}
	sort.Strings(out)
	return out
}
