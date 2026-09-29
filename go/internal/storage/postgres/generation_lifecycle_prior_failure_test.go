// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// #7385: a superseded work item keeps its old failure under
// failure_details.prior_failure (#7320). The lifecycle drilldown reads
// failure_details of the newest failure row and exposes the parsed status,
// class, message and updated_at. The old details text stays off the wire.

// lifecycleRowWithDetails is lifecycleRow for a generation whose newest failure
// row carries failureDetails.
func lifecycleRowWithDetails(failureDetails string) []any {
	observed := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)
	row := lifecycleRow(
		"git-repository-scope:acme/app", "gen-old", "repository", "github", "git", "gen-new",
		false, "snapshot", "head_sha", "superseded",
		observed, observed, observed, observed,
		1, 0, 0, 0, 0, 0, 1,
		"projector_superseded_by_newer_generation", "projector work superseded", "superseded", observed,
	)
	row[len(row)-1] = failureDetails
	return row
}

func listLifecycleFailure(t *testing.T, failureDetails string) *statuspkg.GenerationLatestFailure {
	t.Helper()
	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{lifecycleRowWithDetails(failureDetails)}}}}
	page, err := NewStatusStore(queryer).ListGenerationLifecycle(context.Background(), statuspkg.GenerationLifecycleFilter{Limit: 50})
	if err != nil {
		t.Fatalf("ListGenerationLifecycle() error = %v, want nil for any failure_details text", err)
	}
	if len(page.Records) != 1 || page.Records[0].LatestFailure == nil {
		t.Fatalf("records = %+v, want one record with a latest failure", page.Records)
	}
	return page.Records[0].LatestFailure
}

func TestListGenerationLifecycleParsesPriorFailure(t *testing.T) {
	t.Parallel()

	details := `{"scope_id":"s","prior_failure":{"status":"dead_letter","failure_class":"graph_write_timeout",` +
		`"failure_message":"neo4j execute group timed out","failure_details":"phase=semantic rows=500",` +
		`"updated_at":"2026-06-09T09:00:00.123456+00:00"}}`
	failure := listLifecycleFailure(t, details)

	want := statuspkg.GenerationPriorFailure{
		Status:         "dead_letter",
		FailureClass:   "graph_write_timeout",
		FailureMessage: "neo4j execute group timed out",
		UpdatedAt:      "2026-06-09T09:00:00Z",
	}
	if failure.PriorFailure == nil || *failure.PriorFailure != want {
		t.Fatalf("PriorFailure = %+v, want %+v", failure.PriorFailure, want)
	}
	raw, err := json.Marshal(failure)
	if err != nil {
		t.Fatalf("marshal latest failure: %v", err)
	}
	if strings.Contains(string(raw), "phase=semantic") || strings.Contains(string(raw), "failure_details") {
		t.Fatalf("the prior failure's details text is on the wire: %s", raw)
	}
}

func TestListGenerationLifecyclePriorFailureIsNilWhenDetailsCarryNone(t *testing.T) {
	t.Parallel()

	for name, details := range map[string]string{
		"json object without the key": `{"scope_id":"s","work_item_id":"w"}`,
		"free text":                   "phase=semantic label=Variable rows=500",
		"json array":                  `[1,2]`,
		"prior_failure not an object": `{"prior_failure":"oops"}`,
		"empty":                       "",
	} {
		if failure := listLifecycleFailure(t, details); failure.PriorFailure != nil {
			t.Errorf("%s: PriorFailure = %+v, want nil", name, failure.PriorFailure)
		}
	}
}

func TestListGenerationLifecycleQuerySelectsFailureDetails(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{rows: nil}}}
	if _, err := NewStatusStore(queryer).ListGenerationLifecycle(context.Background(), statuspkg.GenerationLifecycleFilter{Limit: 5}); err != nil {
		t.Fatalf("ListGenerationLifecycle() error = %v", err)
	}
	if len(queryer.queries) == 0 || !strings.Contains(queryer.queries[0], "work.failure_details") {
		t.Fatalf("the newest-failure read does not select failure_details:\n%v", queryer.queries)
	}
	if strings.Contains(queryer.queries[0], "IS JSON") {
		t.Fatal("the read must not test JSON validity in SQL; the Go scan decides (#7385)")
	}
}
