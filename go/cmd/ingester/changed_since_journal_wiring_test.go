// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildIngesterProjectorServiceWiresChangedSinceJournal proves the
// ingester's projector queue journals Ack activations only when
// ESHU_CHANGED_SINCE_LINK_ENABLED is on (#7127 PR-3b), and is dark by default.
func TestBuildIngesterProjectorServiceWiresChangedSinceJournal(t *testing.T) {
	t.Parallel()
	for value, want := range map[string]bool{"": false, "false": false, "true": true} {
		service, err := buildIngesterProjectorService(postgres.SQLDB{}, &noopCanonicalWriter{},
			func(key string) string {
				if key == "ESHU_CHANGED_SINCE_LINK_ENABLED" {
					return value
				}
				return ""
			}, nil, nil, nil)
		if err != nil {
			t.Fatalf("buildIngesterProjectorService() = %v", err)
		}
		queue, ok := service.WorkSink.(postgres.ProjectorQueue)
		if !ok {
			t.Fatalf("WorkSink type = %T, want postgres.ProjectorQueue", service.WorkSink)
		}
		if queue.ChangedSinceJournalEnabled != want {
			t.Fatalf("ESHU_CHANGED_SINCE_LINK_ENABLED=%q: ChangedSinceJournalEnabled = %v, want %v", value, queue.ChangedSinceJournalEnabled, want)
		}
	}
}
