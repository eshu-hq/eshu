// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildBootstrapProjectorWiresChangedSinceJournal proves bootstrap-index
// journals Ack activations only when ESHU_CHANGED_SINCE_LINK_ENABLED is on
// (#7127 PR-3b), and is dark by default.
func TestBuildBootstrapProjectorWiresChangedSinceJournal(t *testing.T) {
	t.Parallel()
	for value, want := range map[string]bool{"": false, "false": false, "true": true} {
		deps, err := buildBootstrapProjector(context.Background(), &fakeBootstrapSQLDB{}, &noopCanonicalWriter{},
			func(key string) string {
				if key == "ESHU_CHANGED_SINCE_LINK_ENABLED" {
					return value
				}
				return ""
			}, nil, nil, nil)
		if err != nil {
			t.Fatalf("buildBootstrapProjector() = %v", err)
		}
		queue, ok := deps.workSink.(postgres.ProjectorQueue)
		if !ok {
			t.Fatalf("workSink type = %T, want postgres.ProjectorQueue", deps.workSink)
		}
		if queue.ChangedSinceJournalEnabled != want {
			t.Fatalf("ESHU_CHANGED_SINCE_LINK_ENABLED=%q: ChangedSinceJournalEnabled = %v, want %v", value, queue.ChangedSinceJournalEnabled, want)
		}
	}
}
