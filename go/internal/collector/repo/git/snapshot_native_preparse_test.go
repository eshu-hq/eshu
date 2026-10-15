// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/discovery"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/preparse"
	"github.com/eshu-hq/eshu/go/internal/parser/scip"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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
		"gitlab host":     identity(base, RepoSyncConfig{GitlabHost: "gitlab.acme.example"}, lookPath, stat(1)),
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

// TestReconcileStampIdentityWithoutParseFromEnv (#8095): the first-sweep
// stamp is off by default and accepts the same truthy spellings as the other
// boolean gates; anything else stays off, so an unset or mistyped value keeps
// today's parse.
func TestReconcileStampIdentityWithoutParseFromEnv(t *testing.T) {
	t.Parallel()
	load := func(raw string) RepoSyncConfig {
		config, err := LoadRepoSyncConfig("collector-git", func(key string) string {
			switch key {
			case "ESHU_REPO_RECONCILE_STAMP_IDENTITY_WITHOUT_PARSE":
				return raw
			case "ESHU_REPO_SOURCE_MODE":
				return "explicit"
			case "ESHU_REPOS_DIR":
				return "/tmp/repos"
			}
			return ""
		})
		if err != nil {
			t.Fatalf("raw %q: LoadRepoSyncConfig error = %v", raw, err)
		}
		return config
	}
	for raw, want := range map[string]bool{"": false, "  ": false, "0": false, "false": false, "no": false, "x": false, "1": true, "true": true, "TRUE": true, "yes": true, "on": true, " on ": true} {
		if got := load(raw).ReconcileStampIdentityWithoutParse; got != want {
			t.Errorf("raw %q: ReconcileStampIdentityWithoutParse = %t, want %t", raw, got, want)
		}
	}
}

// TestDegradedSnapshotStampsNoIdentity (review F-2, ruling a9-143.3): a
// snapshot that fell back from SCIP or skipped a file it could not read may
// differ from a later identical run, so its generation carries no pre-parse
// identity. The next sweep then reads identity_absent and parses
// (TestDecideCells) instead of re-projecting degraded facts for up to N-1
// intervals.
func TestDegradedSnapshotStampsNoIdentity(t *testing.T) {
	const identity = "preparse:v1:x"
	for _, cell := range []struct {
		name       string
		indexer    *recordingSCIPIndexer
		parser     fakeSCIPParser
		unreadable bool
		want       string
		reason     telemetry.PreparseIdentityDropReason
	}{
		{name: "healthy", indexer: &recordingSCIPIndexer{available: true}, want: identity},
		{name: "scip binary missing", indexer: &recordingSCIPIndexer{available: false}, reason: telemetry.PreparseDropSCIPBinaryUnavailable},
		{name: "scip indexer failed", indexer: &recordingSCIPIndexer{available: true, runErr: errors.New("boom")}, reason: telemetry.PreparseDropSCIPIndexerFailed},
		{name: "scip parse failed", indexer: &recordingSCIPIndexer{available: true}, parser: fakeSCIPParser{err: errors.New("bad index")}, reason: telemetry.PreparseDropSCIPParseFailed},
		{name: "file read failed", unreadable: true, reason: telemetry.PreparseDropFileReadFailed},
	} {
		t.Run(cell.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			writeCollectorTestFile(t, filepath.Join(repoRoot, "app.py"), "def main():\n    return 1\n")
			snapshotter := NativeRepositorySnapshotter{Engine: defaultCollectorTestEngine(t)}
			if cell.indexer != nil {
				config := LoadSnapshotSCIPConfig(scipEnabledTestGetenv(nil))
				config.Indexer = cell.indexer
				config.Parser = cell.parser
				if cell.parser.err == nil {
					config.Parser = fakeSCIPParser{result: scip.ParseResult{Files: map[string]map[string]any{
						filepath.Join(repoRoot, "app.py"): {"function_calls_scip": []map[string]any{}},
					}}}
				}
				snapshotter.SCIP = config
			}
			if cell.unreadable {
				// A file the import pre-scan does not read (an unreadable source
				// file fails the whole snapshot there, which stores nothing), so
				// the per-file read in the parse is the step that skips it.
				locked := filepath.Join(repoRoot, "locked.tf")
				writeCollectorTestFile(t, locked, "variable \"x\" {}\n")
				if err := os.Chmod(locked, 0); err != nil {
					t.Fatalf("chmod: %v", err)
				}
				t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
				if _, err := os.ReadFile(locked); err == nil {
					t.Skip("this user can read a mode-0 file (root?); the read-failure cell cannot run")
				}
			}
			source, logs, reader := dropRecordingSource(t)
			source.Snapshotter = snapshotter
			collected, err := source.snapshotOneRepository(context.Background(),
				SelectedRepository{RepoPath: repoRoot, PreparseIdentity: identity}, "run", time.Now().UTC(), 0)
			if err != nil {
				t.Fatalf("snapshotOneRepository() error = %v", err)
			}
			drainFactChannel(collected.Facts)
			if got := collected.Generation.PreparseIdentity; got != cell.want {
				t.Fatalf("generation identity = %q, want %q", got, cell.want)
			}
			assertIdentityDropRecorded(t, logs.String(), dropCounts(t, reader), cell.reason, repoRoot)
		})
	}
}
