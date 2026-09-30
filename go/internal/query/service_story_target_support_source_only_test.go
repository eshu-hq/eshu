// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestBuildStoryTargetSupportExplainsSourceOnlySupportFacts(t *testing.T) {
	t.Parallel()

	got := buildStoryTargetSupportWithSourceOnlySummary(
		serviceStoryTargetSupportFilter{
			Repository: "repo-payments-api",
			TargetKind: "service",
			TargetID:   "workload:payments-api",
			ServiceID:  "workload:payments-api",
			Limit:      serviceStoryTargetSupportLimit,
		},
		nil,
		false,
		serviceStoryTargetSupportSourceOnlySummary{
			TotalCount:           2,
			WorkItemCount:        1,
			IncidentRoutingCount: 1,
		},
	)

	if gotCount := querycontract.IntVal(got, "evidence_count"); gotCount != 0 {
		t.Fatalf("evidence_count = %d, want 0 for source-only support facts", gotCount)
	}
	if gotCount := querycontract.IntVal(got, "work_item_count"); gotCount != 0 {
		t.Fatalf("work_item_count = %d, want 0 for source-only Jira facts", gotCount)
	}
	if gotCount := querycontract.IntVal(got, "incident_routing_count"); gotCount != 0 {
		t.Fatalf("incident_routing_count = %d, want 0 for source-only PagerDuty facts", gotCount)
	}
	coverage := querycontract.MapValue(got, "coverage")
	if gotCount, want := querycontract.IntVal(coverage, "source_only_count"), 2; gotCount != want {
		t.Fatalf("coverage.source_only_count = %d, want %d", gotCount, want)
	}
	if gotCount, want := querycontract.IntVal(coverage, "work_item_source_only_count"), 1; gotCount != want {
		t.Fatalf("coverage.work_item_source_only_count = %d, want %d", gotCount, want)
	}
	if gotCount, want := querycontract.IntVal(coverage, "incident_routing_source_only_count"), 1; gotCount != want {
		t.Fatalf("coverage.incident_routing_source_only_count = %d, want %d", gotCount, want)
	}
	missing := querycontract.MapSliceValue(got, "missing_evidence")
	if gotReason := querycontract.StringVal(missing[0], "reason"); gotReason != "support_source_only_not_target_linked" {
		t.Fatalf("missing_evidence[0].reason = %q, want support_source_only_not_target_linked", gotReason)
	}
}

func TestContentReaderServiceStoryTargetSupportReportsSourceOnlySupportFacts(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"payload"},
			rows:    [][]driver.Value{},
		},
		{
			columns: []string{
				"support_source_only_count",
				"work_item_source_only_count",
				"incident_routing_source_only_count",
			},
			rows: [][]driver.Value{{int64(2), int64(1), int64(1)}},
		},
	})
	reader := NewContentReader(db)

	got, err := reader.ServiceStoryTargetSupportEvidence(t.Context(), serviceStoryTargetSupportFilter{
		Repository:              "repo-payments-api",
		TargetKind:              "service",
		TargetID:                "workload:payments-api",
		ServiceID:               "workload:payments-api",
		Limit:                   serviceStoryTargetSupportLimit,
		RepositoryWorkloadCount: 1,
		RepositoryDefinesTarget: true,
	})
	if err != nil {
		t.Fatalf("ServiceStoryTargetSupportEvidence() error = %v, want nil", err)
	}
	support := got.Support
	if gotCount := querycontract.IntVal(support, "evidence_count"); gotCount != 0 {
		t.Fatalf("evidence_count = %d, want 0", gotCount)
	}
	missing := querycontract.MapSliceValue(support, "missing_evidence")
	if gotReason := querycontract.StringVal(missing[0], "reason"); gotReason != "support_source_only_not_target_linked" {
		t.Fatalf("missing_evidence[0].reason = %q, want support_source_only_not_target_linked", gotReason)
	}
	coverage := querycontract.MapValue(support, "coverage")
	if gotCount, want := querycontract.IntVal(coverage, "source_only_count"), 2; gotCount != want {
		t.Fatalf("coverage.source_only_count = %d, want %d", gotCount, want)
	}
}

// TestContentReaderServiceTargetSupportFailsClosedWithoutAGraphVerdict proves a
// service target the graph did not show its repository defining issues no row
// read at all: the only statement is the source-only aggregate, and the story
// reports the source-only reason instead of attaching or hiding the links (#7138).
func TestContentReaderServiceTargetSupportFailsClosedWithoutAGraphVerdict(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"support_source_only_count",
				"work_item_source_only_count",
				"incident_routing_source_only_count",
			},
			rows: [][]driver.Value{{int64(3), int64(2), int64(1)}},
		},
	})
	reader := NewContentReader(db)

	got, err := reader.ServiceStoryTargetSupportEvidence(t.Context(), serviceStoryTargetSupportFilter{
		Repository: "repo-payments-api",
		TargetKind: "service",
		TargetID:   "workload:payments-api",
		ServiceID:  "workload:payments-api",
		Limit:      serviceStoryTargetSupportLimit,
	})
	if err != nil {
		t.Fatalf("ServiceStoryTargetSupportEvidence() error = %v, want nil", err)
	}
	if gotCount := querycontract.IntVal(got.Support, "evidence_count"); gotCount != 0 {
		t.Fatalf("evidence_count = %d, want 0 (fail closed)", gotCount)
	}
	if gotReason := querycontract.StringVal(querycontract.MapSliceValue(got.Support, "missing_evidence")[0], "reason"); gotReason != "support_source_only_not_target_linked" {
		t.Fatalf("missing_evidence[0].reason = %q, want support_source_only_not_target_linked", gotReason)
	}
	if gotCount := querycontract.IntVal(querycontract.MapValue(got.Support, "coverage"), "repository_workload_count"); gotCount != 0 {
		t.Fatalf("coverage.repository_workload_count = %d, want 0", gotCount)
	}
}

func TestBuildServiceStoryTargetSupportSourceOnlySQLStaysAggregateOnly(t *testing.T) {
	t.Parallel()

	query, args := buildServiceStoryTargetSupportSourceOnlySQL(serviceStoryTargetSupportFactKinds())

	assertSupportSQLContainsAll(
		t, query,
		"COUNT(*) AS support_source_only_count",
		"COUNT(*) FILTER (WHERE fact.fact_kind LIKE 'work_item.%') AS work_item_source_only_count",
		"COUNT(*) FILTER (WHERE fact.fact_kind LIKE 'incident_routing.%') AS incident_routing_source_only_count",
		"SELECT DISTINCT unnest($1::text[]) AS fact_kind",
		"fact.fact_kind = kind.fact_kind",
		"generation.status = 'active'",
		serviceStoryTargetSupportUnlinkedPredicate,
	)
	for _, forbidden := range []string{"fact.payload AS", "source_record_id", "ORDER BY", "LIMIT"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("source-only support SQL leaked row-shaped fragment %q:\n%s", forbidden, query)
		}
	}
	if len(args) != 1 {
		t.Fatalf("args len = %d, want fact kind array only", len(args))
	}
	if strings.Contains(query, "candidate_refs") {
		t.Fatalf("source-only support SQL still tests the documentation ref keys:\n%s", query)
	}
}
