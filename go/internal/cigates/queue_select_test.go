// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"reflect"
	"testing"
)

func TestSelectQueueWorkflowUnionsBlockingAndAdvisoryRows(t *testing.T) {
	t.Parallel()
	head := &Registry{Gates: []Gate{
		{ID: "new-advisory", Triggers: []string{"docs/**"}, CI: CI{Workflow: "test.yml", Job: "docs"}},
		{ID: "new-blocker", Blocking: true, Triggers: []string{"go/**"}, CI: CI{Workflow: "test.yml", Job: "core"}},
	}}
	trusted := &Registry{Gates: []Gate{
		{ID: "old-blocker", Blocking: true, Triggers: []string{"go/**"}, CI: CI{Workflow: "test.yml", Job: "legacy"}},
	}}
	got, err := SelectQueueWorkflow([]*Registry{head, trusted}, []string{"docs/new.md", "go/old.go"}, false, "test.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Selected || !reflect.DeepEqual(got.Gates, []string{"new-advisory", "new-blocker", "old-blocker"}) || !reflect.DeepEqual(got.Jobs, []string{"docs", "core", "legacy"}) {
		t.Fatalf("selection = %+v", got)
	}
}

func TestSelectQueueWorkflowFailsClosedOnUnknownOwner(t *testing.T) {
	t.Parallel()
	reg := &Registry{Gates: []Gate{{ID: "gate", Blocking: true, Triggers: []string{"go/**"}, CI: CI{Workflow: "test.yml", Job: "core"}}}}
	for _, tt := range []struct{ workflow, job string }{{"missing.yml", ""}, {"test.yml", "missing"}} {
		if _, err := SelectQueueWorkflow([]*Registry{reg}, []string{"go/x.go"}, false, tt.workflow, tt.job); err == nil {
			t.Fatalf("workflow %q job %q: expected error", tt.workflow, tt.job)
		}
	}
}
