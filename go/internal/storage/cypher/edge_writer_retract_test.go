// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEdgeWriterRetractEdgesWorkloadDependencyDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainWorkloadDependency, rows, "finalization/workloads")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "source:Workload") {
		t.Fatalf("cypher missing Workload match: %s", executor.calls[0].Cypher)
	}
	for _, want := range []string{
		"source.repo_id IN $repo_ids",
		"rel.evidence_source = $evidence_source",
		"DELETE rel",
	} {
		if !strings.Contains(executor.calls[0].Cypher, want) {
			t.Fatalf("workload_dependency retract must preserve exact source-repo and writer ownership boundary %q:\n%s", want, executor.calls[0].Cypher)
		}
	}
	if got := executor.calls[0].Parameters["evidence_source"]; got != reducer.EvidenceSourceWorkloads {
		t.Fatalf("retract evidence_source = %#v, want %q", got, reducer.EvidenceSourceWorkloads)
	}
	if got := executor.calls[0].Parameters["repo_ids"]; !reflect.DeepEqual(got, []string{"repo-a"}) {
		t.Fatalf("retract repo_ids = %#v, want exact source repository [repo-a]", got)
	}
}
