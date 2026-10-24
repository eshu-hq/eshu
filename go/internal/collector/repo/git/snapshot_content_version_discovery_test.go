// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// contentVersionDiscoveryGaps returns the files under root (slash-relative)
// that a registered parser handles but production discovery does not admit
// (#8905). The content-version digest only sees admitted files, so a fixture
// in such a gap, for example under a directory named deps, build, or vendor,
// is invisible: a parser change that alters its facts moves no digest and the
// pin test stays green. The walk uses the snapshotter's own file-set
// resolution and discoveryOptions, not a copy of its prune list, so a new
// default-ignored directory is caught without editing this helper.
func contentVersionDiscoveryGaps(t *testing.T, root string) []string {
	t.Helper()
	// Discovery runs on a copy in a temp root, as the digest does, so a
	// .gitignore or .eshuignore above the checkout cannot change the result.
	tempRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp root: %v", err)
	}
	copyRoot := filepath.Join(tempRoot, "discovery-repo")
	copyContentVersionFixture(t, root, copyRoot)

	registry := parser.DefaultRegistry()
	fileSet, _, err := resolveNativeSnapshotFileSet(copyRoot, registry, NativeRepositorySnapshotter{}.discoveryOptions())
	if err != nil {
		t.Fatalf("resolve fixture file set: %v", err)
	}
	admitted := map[string]bool{}
	for _, path := range fileSet.FilePaths() {
		rel, relErr := filepath.Rel(copyRoot, path)
		if relErr != nil {
			t.Fatalf("relativize admitted path %q: %v", path, relErr)
		}
		admitted[filepath.ToSlash(rel)] = true
	}

	var gaps []string
	err = filepath.WalkDir(copyRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		rel, relErr := filepath.Rel(copyRoot, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if _, parsed := registry.LookupByPath(rel); parsed && !admitted[rel] {
			gaps = append(gaps, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk fixture: %v", err)
	}
	sort.Strings(gaps)
	return gaps
}

// TestContentVersionFixtureFilesReachDiscovery fails when a fixture file a
// parser handles sits where discovery skips it (#8905). Fix a failure by
// moving the file to a directory the collector does not prune, then bump
// gitGenerationContentVersion and re-pin.
func TestContentVersionFixtureFilesReachDiscovery(t *testing.T) {
	t.Parallel()
	if gaps := contentVersionDiscoveryGaps(t, "testdata/content_version"); len(gaps) > 0 {
		t.Fatalf("testdata/content_version files discovery skips, so the digest never sees them: %v: "+
			"move them out of ignored directories (see defaultIgnoredDirs), then bump "+
			"gitGenerationContentVersion and re-pin with -update-content-version-pin", gaps)
	}
}

// TestContentVersionDiscoveryGapsFlagsPrunedFixture is the seeded violation for
// the guard above: a parsed manifest under an ignored directory is reported, the
// same file under an admitted directory is not.
func TestContentVersionDiscoveryGapsFlagsPrunedFixture(t *testing.T) {
	t.Parallel()
	seed := func(t *testing.T, dir string) string {
		t.Helper()
		root := t.TempDir()
		target := filepath.Join(root, dir)
		if err := os.MkdirAll(target, 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", target, err)
		}
		body := []byte("module example.com/seed\n\ngo 1.21\n")
		if err := os.WriteFile(filepath.Join(target, "go.mod"), body, 0o600); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}
		return root
	}

	t.Run("pruned directory is a gap", func(t *testing.T) {
		t.Parallel()
		got := contentVersionDiscoveryGaps(t, seed(t, "deps/gomod"))
		if want := []string{"deps/gomod/go.mod"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("gaps = %v, want %v", got, want)
		}
	})
	t.Run("admitted directory is not a gap", func(t *testing.T) {
		t.Parallel()
		if got := contentVersionDiscoveryGaps(t, seed(t, "manifests/gomod")); len(got) != 0 {
			t.Fatalf("gaps = %v, want none", got)
		}
	})
}
