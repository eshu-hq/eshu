// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// pythonFoldRoutingDB serves fileCount Python file facts split across two
// repositories, each payload carrying the parsed keys the Python reader
// consumes. Rows are numbered out of byte order so paging must sort.
func pythonFoldRoutingDB(t *testing.T, fileCount int) (*supplyChainImpactRoutingDB, map[string][]byte, map[string]string) {
	t.Helper()
	routed := &supplyChainImpactRoutingDB{
		branchRows: map[string][][2]string{},
		facts:      map[string]string{},
		payloads:   map[string][]byte{},
		scopes:     map[string][2]string{},
	}
	rawByID := map[string][]byte{}
	repoByID := map[string]string{}
	for n := fileCount - 1; n >= 0; n-- {
		id := fmt.Sprintf("pyfile:%05d", (n*7919)%fileCount)
		repo := "repo-a"
		if n%3 == 0 {
			repo = "repo-b"
		}
		payload := map[string]any{
			"repo_id":       repo,
			"relative_path": fmt.Sprintf("pkg/f%d.py", n),
			"parsed_file_data": map[string]any{
				"language": "python",
				"imports": []any{
					map[string]any{"source": "requests", "name": "requests", "lang": "python"},
				},
				"function_calls": []any{
					map[string]any{"full_name": "requests.get", "name": "get", "lang": "python"},
				},
				"functions": []any{
					map[string]any{"name": "view", "decorators": []any{"app.route"}},
				},
			},
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		routed.facts[id] = factload.FactKindFile
		routed.payloads[id] = raw
		rawByID[id] = raw
		repoByID[id] = repo
	}
	return routed, rawByID, repoByID
}

// TestFoldPythonReachabilityFilesPagesAndFolds pins #9860 at the store seam:
// file facts page through the cursor in stable order, each row folds as it is
// scanned so no page is ever held, only allowlisted repositories fold, and the
// folded bytes equal the raw payload total.
func TestFoldPythonReachabilityFilesPagesAndFolds(t *testing.T) {
	t.Parallel()

	// 1200 files, 800 allowlisted, over 500-row pages: the fold must make two
	// page reads, proving it pages instead of one giant load.
	routed, rawByID, repoByID := pythonFoldRoutingDB(t, 1200)
	store := NewFactStore(routed)
	fold := supplychaincore.NewFileReachabilityFold(nil)
	files, payloadBytes, err := store.FoldPythonReachabilityFiles(
		context.Background(), "scope-py", "gen-py", "repo_id", []string{"repo-a"}, fold,
	)
	if err != nil {
		t.Fatalf("FoldPythonReachabilityFiles() error = %v, want nil", err)
	}
	var wantFiles int
	var wantBytes int64
	for id, repo := range repoByID {
		if repo != "repo-a" {
			continue
		}
		wantFiles++
		wantBytes += int64(len(rawByID[id]))
	}
	if files != wantFiles {
		t.Fatalf("folded files = %d, want %d (repo-a only)", files, wantFiles)
	}
	if fold.Files() != wantFiles {
		t.Fatalf("fold.Files() = %d, want %d", fold.Files(), wantFiles)
	}
	if payloadBytes != wantBytes {
		t.Fatalf("folded bytes = %d, want raw payload total %d", payloadBytes, wantBytes)
	}
	var pageCalls int
	for _, call := range routed.calls {
		if call.Label == "python_fold" {
			pageCalls++
		}
	}
	if pageCalls != 2 {
		t.Fatalf("page reads = %d, want 2 for 800 allowlisted files over 500-row pages", pageCalls)
	}
	if _, ok := fold.PythonReachabilityDigestEnvelope(); !ok {
		t.Fatal("PythonReachabilityDigestEnvelope() ok = false, want a digest")
	}

	// Narrowing the allowlist to nothing folds nothing and mints no digest.
	empty := supplychaincore.NewFileReachabilityFold(nil)
	nothing, noBytes, err := store.FoldPythonReachabilityFiles(
		context.Background(), "scope-py", "gen-py", "repo_id", []string{"repo-zzz"}, empty,
	)
	if err != nil {
		t.Fatalf("FoldPythonReachabilityFiles() error = %v, want nil", err)
	}
	if nothing != 0 || noBytes != 0 {
		t.Fatalf("folded = (%d, %d), want (0, 0) for an unmatched allowlist", nothing, noBytes)
	}
	if _, ok := empty.PythonReachabilityDigestEnvelope(); ok {
		t.Fatal("PythonReachabilityDigestEnvelope() ok = true, want no digest for zero files")
	}
}

// TestFoldPythonReachabilityFilesRejectsBlankKey pins the short-circuit: a
// blank key or an empty allowlist reads nothing.
func TestFoldPythonReachabilityFilesRejectsBlankKey(t *testing.T) {
	t.Parallel()

	routed, _, _ := pythonFoldRoutingDB(t, 10)
	store := NewFactStore(routed)
	before := len(routed.calls)
	fold := supplychaincore.NewFileReachabilityFold(nil)
	files, payloadBytes, err := store.FoldPythonReachabilityFiles(
		context.Background(), "scope-py", "gen-py", "  ", []string{"repo-a"}, fold,
	)
	if err != nil {
		t.Fatalf("FoldPythonReachabilityFiles() error = %v, want nil", err)
	}
	if files != 0 || payloadBytes != 0 {
		t.Fatalf("folded = (%d, %d), want (0, 0) for a blank key", files, payloadBytes)
	}
	if len(routed.calls) != before {
		t.Fatal("pager issued a statement for a blank key, want no read")
	}
}
