// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

func TestReadWorkflowExtensionOutcomesAggregatesLatestScopeResults(t *testing.T) {
	completed := time.Date(2026, 10, 28, 23, 0, 0, 0, time.UTC)
	queryer := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{{
		"aws", "instance-a", int64(2), int64(1), int64(1), int64(0), completed, sql.NullTime{Time: completed, Valid: true},
	}}}}}
	instances := []collector.InstanceSummary{
		{CollectorKind: "aws", InstanceID: "instance-a", Enabled: true, ExtensionHosted: true},
		{CollectorKind: "aws", InstanceID: "in-tree", Enabled: true},
		{CollectorKind: "aws", InstanceID: "disabled", ExtensionHosted: true},
		{CollectorKind: "aws", InstanceID: "deactivated", Enabled: true, ExtensionHosted: true, DeactivatedAt: completed},
	}
	got, err := readWorkflowExtensionOutcomes(context.Background(), queryer, instances)
	if err != nil {
		t.Fatalf("readWorkflowExtensionOutcomes() error = %v", err)
	}
	if len(got) != 1 || got[0].CollectorKind != "aws" || got[0].InstanceID != "instance-a" ||
		got[0].ScopeCount != 2 || got[0].PartialScopes != 1 || got[0].WarningCount != 1 ||
		got[0].InFlightScopes != 0 || !got[0].UpdatedAt.Equal(completed) ||
		!got[0].LastCommittedAt.Equal(completed) {
		t.Fatalf("outcomes = %#v, want one bounded aggregate", got)
	}
	if len(queryer.queries) != 1 || !strings.Contains(queryer.queries[0].query, "FROM unnest($1::text[], $2::text[])") ||
		!strings.Contains(queryer.queries[0].query, "DISTINCT ON (scope_id)") ||
		!strings.Contains(queryer.queries[0].query, "collector_instance_id = requested.collector_instance_id") ||
		!strings.Contains(queryer.queries[0].query, "updated_at DESC, work_item_id DESC") ||
		!strings.Contains(queryer.queries[0].query, "UNION ALL") ||
		!strings.Contains(queryer.queries[0].query, "committed.last_committed_at") ||
		!strings.Contains(queryer.queries[0].query, "extension_result_state = 'partial_withheld'") {
		t.Fatalf("query = %#v, want keyed latest completed outcome per scope", queryer.queries)
	}
	if got := queryer.queries[0].args; len(got) != 2 ||
		!slices.Equal(got[0].([]string), []string{"aws"}) ||
		!slices.Equal(got[1].([]string), []string{"instance-a"}) {
		t.Fatalf("query args = %#v, want only the enabled extension instance", got)
	}
}

func TestReadWorkflowExtensionOutcomesWithheldAttemptKeepsCommittedTime(t *testing.T) {
	attempted := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	committed := attempted.Add(-time.Hour)
	for _, tc := range []struct {
		name      string
		committed sql.NullTime
		want      time.Time
	}{
		{name: "prior completion", committed: sql.NullTime{Time: committed, Valid: true}, want: committed},
		{name: "no completion"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queryer := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{{
				"aws", "instance-a", int64(1), int64(1), int64(3), int64(0), attempted, tc.committed,
			}}}}}
			rows, err := readWorkflowExtensionOutcomes(context.Background(), queryer, []collector.InstanceSummary{{
				CollectorKind: "aws", InstanceID: "instance-a", Enabled: true, ExtensionHosted: true,
			}})
			if err != nil {
				t.Fatalf("readWorkflowExtensionOutcomes() error = %v", err)
			}
			if len(rows) != 1 || !rows[0].LastCommittedAt.Equal(tc.want) || !rows[0].UpdatedAt.Equal(attempted) || rows[0].WarningCount != 3 {
				t.Fatalf("withheld outcome = %+v, want attempt time %s, committed time %s, 3 warnings", rows, attempted, tc.want)
			}
		})
	}
}
