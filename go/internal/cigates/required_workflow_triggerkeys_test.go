// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import "testing"

func TestWorkflowTriggerKeys_Sequence(t *testing.T) {
	t.Parallel()

	keys, err := workflowTriggerKeys([]byte("name: Trigger Test\non: [push, workflow_run, pull_request_target]\njobs: {}\n"))
	if err != nil {
		t.Fatalf("parse sequence triggers: %v", err)
	}
	for _, want := range []string{"push", "workflow_run", "pull_request_target"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("sequence triggers missing %q: %v", want, keys)
		}
	}
}

func TestWorkflowTriggerKeys_RejectsAlias(t *testing.T) {
	t.Parallel()

	_, err := workflowTriggerKeys([]byte("events: &events [push, workflow_run]\non: *events\njobs: {}\n"))
	if err == nil {
		t.Fatal("alias trigger configuration must be rejected")
	}
}
