// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestRetractEdgesNilFenceShapeSkipsWholeScopeDelete pins the one shape where
// the #6166 narrowing is NOT a no-op, so the divergence stays deliberate.
//
// ProcessPartitionOnce only calls planRepoWideRetractWork when a refresh fence
// is wired (shared_projection_worker.go). With refreshFence == nil it passes
// every latest row -- including unmarked per-edge rows -- straight to
// RetractEdges. For the four narrowed domains that batch now binds an empty
// repo id list and issues no whole-repository DELETE, where before #6166 it
// issued one over the whole batch.
//
// Production always wires the fence (cmd/reducer/main.go), so this is a
// test-only shape today. It is pinned here rather than left implicit because
// the worker's own comment used to claim the nil-fence path stayed
// byte-identical, which stopped being true for these four domains. If someone
// re-wires the nil-fence path, or reverts the narrowing, this test says which
// behaviour they changed.
//
// The lost retract is silent by construction -- the early return never reaches
// recordGroupedWrite -- so this also asserts the warning that makes it
// greppable.
func TestRetractEdgesNilFenceShapeSkipsWholeScopeDelete(t *testing.T) {
	t.Parallel()

	// Every row unmarked: no delta_projection, no refresh intent_type. This is
	// what the nil-fence path hands RetractEdges for a per-edge-only partition.
	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "legacy-1",
			RepositoryID: "repo-legacy",
			Payload:      map[string]any{"repo_id": "repo-legacy"},
		},
		{
			IntentID:     "legacy-2",
			RepositoryID: "repo-legacy",
			Payload:      map[string]any{"repo_id": "repo-legacy"},
		},
	}

	for _, domain := range []string{
		reducer.DomainInheritanceEdges,
		reducer.DomainRationaleEdges,
	} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer
			executor := &probeGuardRecordingExecutor{probeFound: true}
			writer := NewEdgeWriter(executor, 0)
			writer.Logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

			if err := writer.RetractEdges(context.Background(), domain, rows, "reducer/test"); err != nil {
				t.Fatalf("RetractEdges: %v", err)
			}

			// No statement may bind $repo_ids: an all-unmarked batch asks for
			// no whole-repository retract. Asserting "no statements at all"
			// would be weaker and wrong -- probe reads are allowed.
			for _, stmt := range executor.executeCalls {
				if raw, ok := stmt.Parameters["repo_ids"]; ok {
					bound, _ := raw.([]string)
					if len(bound) != 0 {
						t.Fatalf("nil-fence shape bound repo_ids %v; an unmarked batch must issue no whole-repository DELETE (cypher %q)",
							bound, stmt.Cypher)
					}
				}
			}

			// The skip must not be silent.
			if got := logs.String(); !strings.Contains(got, "whole-scope retract skipped") {
				t.Errorf("no warning logged for a skipped whole-scope retract; the lost retract would be invisible. logs=%q", got)
			}
		})
	}
}
