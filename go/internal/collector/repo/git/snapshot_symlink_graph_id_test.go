// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestFileGraphIDStableThroughSymlinkedCheckoutRoot snapshots one repository
// twice: once through its direct path and once through a symlink that points
// at the same directory (the macOS /var -> /private/var shape from #7998).
// The file fact graph_id values must be identical either way: they key on
// the repository id and the path inside the repository, never on how the
// checkout directory was spelled.
func TestFileGraphIDStableThroughSymlinkedCheckoutRoot(t *testing.T) {
	t.Parallel()
	realDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp root: %v", err)
	}
	body := "def handler():\n    pass\n"
	if err := os.WriteFile(filepath.Join(realDir, "app.py"), []byte(body), 0o600); err != nil {
		t.Fatalf("write app.py: %v", err)
	}
	linkedDir := filepath.Join(filepath.Dir(realDir), "linked-checkout")
	if err := os.Symlink(realDir, linkedDir); err != nil {
		t.Fatalf("symlink checkout: %v", err)
	}

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}
	snapshotter := NativeRepositorySnapshotter{Engine: engine}
	observedAt := time.Date(2026, time.October, 12, 0, 0, 0, 0, time.UTC)

	directSnapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: realDir})
	if err != nil {
		t.Fatalf("snapshot direct path: %v", err)
	}
	linkedSnapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: linkedDir})
	if err != nil {
		t.Fatalf("snapshot symlinked path: %v", err)
	}

	// buildStreamingGeneration takes the repository path the caller used to
	// reach the checkout, while the snapshot file paths come from discovery
	// (symlink-resolved). The symlinked spelling is the production shape:
	// source_processing passes repository.RepoPath through unresolved.
	directFacts := drainFactChannel(buildStreamingGeneration(
		realDir, testCollectorRepositoryMetadata(realDir), "run-direct",
		observedAt, directSnapshot, false, "").Facts)
	linkedFacts := drainFactChannel(buildStreamingGeneration(
		linkedDir, testCollectorRepositoryMetadata(linkedDir), "run-linked",
		observedAt, linkedSnapshot, false, "").Facts)

	directIDs := sortedFileGraphIDs(t, directFacts)
	linkedIDs := sortedFileGraphIDs(t, linkedFacts)
	if len(directIDs) == 0 {
		t.Fatalf("direct snapshot emitted no file facts")
	}
	for _, id := range linkedIDs {
		if strings.Contains(id, "..") {
			t.Fatalf("symlinked checkout emitted file graph_id with ../ segment: %q (all: %q)", id, linkedIDs)
		}
	}
	if strings.Join(directIDs, "\n") != strings.Join(linkedIDs, "\n") {
		t.Fatalf("file graph_ids differ by checkout spelling:\ndirect %q\nlinked %q", directIDs, linkedIDs)
	}
}

func sortedFileGraphIDs(t *testing.T, envelopes []facts.Envelope) []string {
	t.Helper()
	var ids []string
	for _, envelope := range envelopes {
		if envelope.FactKind != "file" {
			continue
		}
		id, _ := envelope.Payload["graph_id"].(string)
		if id == "" {
			t.Fatalf("file fact without graph_id: %+v", envelope)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
