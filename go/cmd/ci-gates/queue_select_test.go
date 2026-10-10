// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func queueTestRegistry() *cigates.Registry {
	return &cigates.Registry{Gates: []cigates.Gate{
		{ID: "blocking", Blocking: true, Triggers: []string{"go/**"}, CI: cigates.CI{Workflow: "test.yml", Job: "go-core"}},
		{ID: "advisory", Triggers: []string{"docs/**"}, CI: cigates.CI{Workflow: "test.yml", Job: "docs-advisory"}},
		{ID: "ci-only", Blocking: true, Triggers: []string{"docs/**"}, CI: cigates.CI{Workflow: "test.yml", Job: "docs-ci-only"}},
		{ID: "other-workflow", Blocking: true, Triggers: []string{"go/**"}, CI: cigates.CI{Workflow: "other.yml", Job: "other"}},
	}}
}

func queueRunner(files string) *endpointRunner {
	return &endpointRunner{routes: map[string]string{"compare/": `{"files":` + files + `}`}}
}

func TestQueueSelectIncludesAdvisoryAndCIOnlyRows(t *testing.T) {
	t.Parallel()
	runner := queueRunner(`[{"filename":"docs/new.md","previous_filename":"go/old.go"}]`)
	got, err := selectQueueGates(context.Background(), runner, []*cigates.Registry{queueTestRegistry()}, "eshu-hq/eshu", "main", queueBranchFixture, headSHAFixture, "test.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Selected || got.Truncated || !reflect.DeepEqual(got.Gates, []string{"blocking", "advisory", "ci-only"}) {
		t.Fatalf("selection = %+v", got)
	}
	if !reflect.DeepEqual(got.Jobs, []string{"go-core", "docs-advisory", "docs-ci-only"}) {
		t.Fatalf("jobs = %v", got.Jobs)
	}
	if len(runner.calls) != 1 || !strings.Contains(runner.calls[0], baseSHAFixture+"..."+headSHAFixture) {
		t.Fatalf("compare calls = %v", runner.calls)
	}
}

func TestQueueSelectUnionsTrustedRegistryAndMatchesConcreteJob(t *testing.T) {
	t.Parallel()
	head := &cigates.Registry{Gates: []cigates.Gate{{ID: "new", Triggers: []string{"docs/**"}, CI: cigates.CI{Workflow: "golden.yml", Job: "differential", CheckNames: []string{"differential nornicdb vs neo4j"}}}}}
	trusted := &cigates.Registry{Gates: []cigates.Gate{{ID: "old", Blocking: true, Triggers: []string{"go/**"}, CI: cigates.CI{Workflow: "golden.yml", Job: "differential", CheckNames: []string{"differential nornicdb vs neo4j"}}}}}
	got, err := selectQueueGates(context.Background(), queueRunner(`[{"filename":"go/x.go"}]`), []*cigates.Registry{head, trusted}, "eshu-hq/eshu", "main", queueBranchFixture, headSHAFixture, "golden.yml", "differential nornicdb vs neo4j")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Selected || !reflect.DeepEqual(got.Gates, []string{"old"}) || !reflect.DeepEqual(got.Jobs, []string{"differential"}) {
		t.Fatalf("selection = %+v", got)
	}
}

func TestQueueSelectSelectsWholeWorkflowAtCapAndOnPolicyChanges(t *testing.T) {
	t.Parallel()
	var files []string
	for i := 0; i < mergeGroupCompareFileCap; i++ {
		files = append(files, fmt.Sprintf(`{"filename":"unrelated/%d"}`, i))
	}
	for _, tt := range []struct {
		name      string
		files     string
		truncated bool
	}{
		{name: "cap", files: `[` + strings.Join(files, ",") + `]`, truncated: true},
		{name: "workflow edit", files: `[{"filename":".github/workflows/test.yml"}]`},
		{name: "registry edit", files: `[{"filename":"specs/ci-gates.v1.yaml"}]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectQueueGates(context.Background(), queueRunner(tt.files), []*cigates.Registry{queueTestRegistry()}, "eshu-hq/eshu", "main", queueBranchFixture, headSHAFixture, "test.yml", "")
			if err != nil {
				t.Fatal(err)
			}
			if !got.Selected || got.Truncated != tt.truncated || len(got.Gates) != 3 {
				t.Fatalf("selection = %+v", got)
			}
		})
	}
}

func TestQueueSelectFailsClosedForUnknownWorkflowAndBadCompare(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		workflow string
		files    string
		branch   string
	}{
		{name: "unknown workflow", workflow: "unknown.yml", files: `[{"filename":"go/x.go"}]`, branch: queueBranchFixture},
		{name: "empty diff", workflow: "test.yml", files: `[]`, branch: queueBranchFixture},
		{name: "malformed compare", workflow: "test.yml", files: `null`, branch: queueBranchFixture},
		{name: "bad branch", workflow: "test.yml", files: `[{"filename":"go/x.go"}]`, branch: "gh-readonly-queue/main/pr-1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := selectQueueGates(context.Background(), queueRunner(tt.files), []*cigates.Registry{queueTestRegistry()}, "eshu-hq/eshu", "main", tt.branch, headSHAFixture, tt.workflow, "")
			if err == nil {
				t.Fatal("expected fail-closed error")
			}
		})
	}
}

func TestQueueSelectUnselectedKnownWorkflow(t *testing.T) {
	t.Parallel()
	got, err := selectQueueGates(context.Background(), queueRunner(`[{"filename":"unrelated/x.txt"}]`), []*cigates.Registry{queueTestRegistry()}, "eshu-hq/eshu", "main", queueBranchFixture, headSHAFixture, "test.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Selected || len(got.Gates) != 0 || len(got.Jobs) != 0 {
		t.Fatalf("selection = %+v", got)
	}
}
