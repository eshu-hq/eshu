// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runtimedeclaration "github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
)

// TestComposerUnavailableSkipSelection drives the native parse, fact stream,
// and real Git sync decision. Deterministic invalid input must not keep a
// clean scope reparsing; a body changed after parse must keep the retry.
func TestComposerUnavailableSkipSelection(t *testing.T) {
	for _, tc := range []struct {
		name, before, after string
		wantSkipReason      string
		removeAfterParse    bool
		wantDropped         bool
	}{
		{"unchanged malformed", `{"require":{"php":"8.2",}`, "", runtimedeclaration.SkipReasonManifestUnparseable, false, false},
		{"unchanged malformed over cap", `{"require":{"php":"` + strings.Repeat("x", runtimedeclaration.MaxManifestBytesPerFile), "", runtimedeclaration.SkipReasonByteCap, false, false},
		{"changed after parse", `{"require":{"php":"8.2",}`, `{"require":{"php":"8.3.12"}}`, runtimedeclaration.SkipReasonManifestUnparseable, false, true},
		{"capped source shrinks after parse", `{"require":{"php":"` + strings.Repeat("x", runtimedeclaration.MaxManifestBytesPerFile), `{"require":{"php":"8.3.12"}}`, runtimedeclaration.SkipReasonByteCap, false, true},
		{"read fails after parse", `{"require":{"php":"8.2",}`, `{"require":{"php":"8.3.12"}}`, runtimedeclaration.SkipReasonManifestUnparseable, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newSkipParseWorld(t, "", "big")
			repoPath := w.repoPath("big")
			writeCollectorTestFile(t, filepath.Join(repoPath, "composer.json"), tc.before)
			identity := w.identity()
			observed := time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)
			snapshot, err := (NativeRepositorySnapshotter{Now: func() time.Time { return observed }}).
				SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoPath, PreparseIdentity: identity})
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.ComposerRuntimeUnparsed) != 1 {
				t.Fatalf("missing Composer parse outcomes = %+v", snapshot.ComposerRuntimeUnparsed)
			}
			if got := snapshot.ComposerRuntimeUnparsed[0].SkipReason; got != tc.wantSkipReason {
				t.Fatalf("Composer skip reason = %q, want %q", got, tc.wantSkipReason)
			}
			if tc.after != "" {
				if tc.removeAfterParse {
					if err := os.Remove(filepath.Join(repoPath, "composer.json")); err != nil {
						t.Fatal(err)
					}
				} else {
					writeCollectorTestFile(t, filepath.Join(repoPath, "composer.json"), tc.after)
				}
			}
			collected := buildStreamingGeneration(repoPath, testCollectorRepositoryMetadata(repoPath), "composer-skip-selection", observed, snapshot, false, "")
			if got := decodeDeclarationList(t, drainFactChannel(collected.Facts)); len(got) != 0 {
				t.Fatalf("unaccepted Composer document emitted %+v", got)
			}
			if got := collected.Generation.PreparseIdentityDropped.Load(); got != tc.wantDropped {
				t.Fatalf("stream_dropped = %v, want %v", got, tc.wantDropped)
			}
			state := w.overdue(identity, 0)
			state.ActiveStreamDroppedPaths = collected.Generation.PreparseIdentityDroppedPaths.Sorted()
			w.resolver.fallback = state
			if tc.removeAfterParse {
				writeCollectorTestFile(t, filepath.Join(repoPath, "composer.json"), tc.after)
			}
			synced := w.sync(7, nil, "big")
			if tc.wantDropped {
				if len(synced.SelectedRepoPaths) != 1 || len(w.resolver.reprojected) != 0 {
					t.Fatalf("changed Composer source selected %d, reprojected %d; want retry parse", len(synced.SelectedRepoPaths), len(w.resolver.reprojected))
				}
			} else if len(synced.SelectedRepoPaths) != 0 || len(w.resolver.reprojected) != 1 {
				t.Fatalf("unchanged deterministic skip selected %d, reprojected %d; want skip parse", len(synced.SelectedRepoPaths), len(w.resolver.reprojected))
			}
		})
	}
}
