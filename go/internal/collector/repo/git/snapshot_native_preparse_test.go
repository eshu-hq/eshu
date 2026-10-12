// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"errors"
	"io/fs"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/discovery"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/preparse"
)

// snapshotterFieldClassification names every NativeRepositorySnapshotter
// field: folded into the pre-parse identity by PreparseConfigFor, or not
// changing what a snapshot emits. A new field fails
// TestPreparseConfigForCoversTheSnapshotter until it is classified (#7978
// follow-up, ruling a9-143 item 1).
var snapshotterFieldClassification = map[string]string{
	"DiscoveryOptions": "folded: ignored and preserved globs",
	"SCIP":             "folded: enabled, languages, indexer binaries",
	"EmitDataflow":     "folded",
	"Engine":           "parser engine; its output contract is GenerationContentVersion",
	"Registry":         "parser registry; its output contract is GenerationContentVersion",
	"Now":              "clock for indexed_at, which no hint or identity carries",
	"ParseWorkers":     "parse concurrency",
	"Tracer":           "telemetry",
	"Instruments":      "telemetry",
	"Logger":           "telemetry",
}

func TestPreparseConfigForCoversTheSnapshotter(t *testing.T) {
	t.Parallel()
	fields := reflect.TypeOf(NativeRepositorySnapshotter{})
	for i := 0; i < fields.NumField(); i++ {
		if name := fields.Field(i).Name; snapshotterFieldClassification[name] == "" {
			t.Errorf("NativeRepositorySnapshotter.%s is not classified for the pre-parse identity", name)
		}
	}
}

type fakeFileInfo struct {
	size    int64
	modTime time.Time
}

func (f fakeFileInfo) Name() string       { return "scip-go" }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() fs.FileMode  { return 0o755 }
func (f fakeFileInfo) ModTime() time.Time { return f.modTime }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }

// TestPreparseConfigForFoldsEachInput: each snapshotter or dependency input
// changes the identity built from PreparseConfigFor, and the SCIP binary is
// read through lookPath and stat.
func TestPreparseConfigForFoldsEachInput(t *testing.T) {
	t.Parallel()
	stat := func(size int64) func(string) (os.FileInfo, error) {
		return func(string) (os.FileInfo, error) { return fakeFileInfo{size: size, modTime: time.Unix(1, 0)}, nil }
	}
	lookPath := func(name string) (string, error) { return "/bin/" + name, nil }
	base := NativeRepositorySnapshotter{SCIP: SnapshotSCIPConfig{Enabled: true, Languages: []string{"go"}}}
	identity := func(s NativeRepositorySnapshotter, sync RepoSyncConfig, look func(string) (string, error), st func(string) (os.FileInfo, error)) string {
		return preparse.Identity("v", "c", "main", PreparseConfigFor(s, sync, look, st))
	}
	want := identity(base, RepoSyncConfig{}, lookPath, stat(1))
	config := PreparseConfigFor(base, RepoSyncConfig{}, lookPath, stat(1))
	if len(config.SCIPBinaries) != 1 || config.SCIPBinaries[0].Path != "/bin/scip-go" || config.SCIPBinaries[0].Size != 1 {
		t.Fatalf("SCIP binaries = %+v, want /bin/scip-go size 1", config.SCIPBinaries)
	}
	cells := map[string]string{
		"ignored glob": identity(NativeRepositorySnapshotter{SCIP: base.SCIP, DiscoveryOptions: discovery.Options{
			IgnoredPathGlobs: []discovery.PathGlobRule{{Pattern: "gen/**", Reason: "generated"}},
		}}, RepoSyncConfig{}, lookPath, stat(1)),
		"preserved glob": identity(NativeRepositorySnapshotter{SCIP: base.SCIP, DiscoveryOptions: discovery.Options{
			PreservedPathGlobs: []string{"docs/**"},
		}}, RepoSyncConfig{}, lookPath, stat(1)),
		"emit dataflow":      identity(NativeRepositorySnapshotter{SCIP: base.SCIP, EmitDataflow: true}, RepoSyncConfig{}, lookPath, stat(1)),
		"scip off":           identity(NativeRepositorySnapshotter{}, RepoSyncConfig{}, lookPath, stat(1)),
		"scip language":      identity(NativeRepositorySnapshotter{SCIP: SnapshotSCIPConfig{Enabled: true, Languages: []string{"go", "python"}}}, RepoSyncConfig{}, lookPath, stat(1)),
		"scip binary change": identity(base, RepoSyncConfig{}, lookPath, stat(2)),
		"scip binary missing": identity(base, RepoSyncConfig{}, func(string) (string, error) {
			return "", errors.New("not found")
		}, stat(1)),
		"dependency mode": identity(base, RepoSyncConfig{DependencyMode: true}, lookPath, stat(1)),
		"dependency name": identity(base, RepoSyncConfig{DependencyName: "pkg"}, lookPath, stat(1)),
		"dependency lang": identity(base, RepoSyncConfig{DependencyLanguage: "go"}, lookPath, stat(1)),
	}
	for name, got := range cells {
		if got == want {
			t.Errorf("%s: identity did not change", name)
		}
	}
}

// TestReconcileParseMaxSkipsFromEnv (D3, ruling a9-143.1): the valve defaults
// to 7 when unset, accepts an explicit 0 (never skip) and a positive N, and
// rejects a negative or non-integer value at startup with an error that names
// the variable and the value; there is no silent fallback.
func TestReconcileParseMaxSkipsFromEnv(t *testing.T) {
	t.Parallel()
	load := func(raw string) (RepoSyncConfig, error) {
		return LoadRepoSyncConfig("collector-git", func(key string) string {
			switch key {
			case "ESHU_REPO_RECONCILE_PARSE_MAX_SKIPS":
				return raw
			case "ESHU_REPO_SOURCE_MODE":
				return "explicit"
			case "ESHU_REPOS_DIR":
				return "/tmp/repos"
			}
			return ""
		})
	}
	for raw, want := range map[string]int{"": 7, "  ": 7, "0": 0, "1": 1, "3": 3, " 3 ": 3} {
		config, err := load(raw)
		if err != nil {
			t.Fatalf("raw %q: LoadRepoSyncConfig error = %v", raw, err)
		}
		if config.ReconcileParseMaxSkips != want {
			t.Fatalf("raw %q: ReconcileParseMaxSkips = %d, want %d", raw, config.ReconcileParseMaxSkips, want)
		}
	}
	for _, raw := range []string{"-1", "x", "1.5", "7d"} {
		_, err := load(raw)
		if err == nil {
			t.Fatalf("raw %q: LoadRepoSyncConfig error = nil, want a rejection", raw)
		}
		if msg := err.Error(); !strings.Contains(msg, "ESHU_REPO_RECONCILE_PARSE_MAX_SKIPS") || !strings.Contains(msg, strconv.Quote(raw)) {
			t.Fatalf("raw %q: error %q does not name the variable and the value", raw, msg)
		}
	}
}
