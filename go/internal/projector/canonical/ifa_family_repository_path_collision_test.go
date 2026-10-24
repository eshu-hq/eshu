// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
//
// Regression test for #6228 publishes-live determinism miss: repository
// facts whose derived canonical path is empty collide on nornicdb's
// UNIQUE(Repository.path) — exactly one empty-path node survives, so every
// other scope's Repository node (and its BUILT_FROM edges) silently never
// materializes. Every repository fact in the driven family cassettes must
// derive a distinct canonical path (path, falling back to local_path, the
// same derivation the projector uses).

package canonical_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa/familyodu"
)

func ifaFamilyRepoPaths(t *testing.T, rel string) [][2]string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	envs, err := familyodu.LoadCassetteEnvelopes(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("load %s: %v", rel, err)
	}
	var out [][2]string
	for _, env := range envs {
		if env.FactKind != "repository" {
			continue
		}
		path, _ := env.Payload["path"].(string)
		localPath, _ := env.Payload["local_path"].(string)
		if path == "" {
			path = localPath
		}
		id, _ := env.Payload["repo_id"].(string)
		t.Logf("repo %s derives path %q", id, path)
		out = append(out, [2]string{id, path})
	}
	return out
}

func TestIfaFamilyRepositoryPathsAreDistinct(t *testing.T) {
	pairs := ifaFamilyRepoPaths(t, "testdata/cassettes/builtfrom/ifa-built-from-family.json")
	pairs = append(pairs, ifaFamilyRepoPaths(t, "testdata/cassettes/publishes/ifa-publishes-family.json")...)
	seen := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		id, path := pair[0], pair[1]
		if first, dup := seen[path]; dup {
			t.Fatalf("duplicate derived repository path %q (repos %s and %s): nornicdb UNIQUE(Repository.path) keeps exactly one node, the others silently never materialize", path, first, id)
		}
		seen[path] = id
	}
}
