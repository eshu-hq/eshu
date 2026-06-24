package query

import (
	"database/sql/driver"
	"strings"
	"testing"
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

	if gotCount := IntVal(got, "evidence_count"); gotCount != 0 {
		t.Fatalf("evidence_count = %d, want 0 for source-only support facts", gotCount)
	}
	if gotCount := IntVal(got, "work_item_count"); gotCount != 0 {
		t.Fatalf("work_item_count = %d, want 0 for source-only Jira facts", gotCount)
	}
	if gotCount := IntVal(got, "incident_routing_count"); gotCount != 0 {
		t.Fatalf("incident_routing_count = %d, want 0 for source-only PagerDuty facts", gotCount)
	}
	coverage := mapValue(got, "coverage")
	if gotCount, want := IntVal(coverage, "source_only_count"), 2; gotCount != want {
		t.Fatalf("coverage.source_only_count = %d, want %d", gotCount, want)
	}
	if gotCount, want := IntVal(coverage, "work_item_source_only_count"), 1; gotCount != want {
		t.Fatalf("coverage.work_item_source_only_count = %d, want %d", gotCount, want)
	}
	if gotCount, want := IntVal(coverage, "incident_routing_source_only_count"), 1; gotCount != want {
		t.Fatalf("coverage.incident_routing_source_only_count = %d, want %d", gotCount, want)
	}
	missing := mapSliceValue(got, "missing_evidence")
	if gotReason := StringVal(missing[0], "reason"); gotReason != "support_source_only_not_target_linked" {
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

	got, err := reader.serviceStoryTargetSupportEvidence(t.Context(), serviceStoryTargetSupportFilter{
		Repository: "repo-payments-api",
		TargetKind: "service",
		TargetID:   "workload:payments-api",
		ServiceID:  "workload:payments-api",
		Limit:      serviceStoryTargetSupportLimit,
	})
	if err != nil {
		t.Fatalf("serviceStoryTargetSupportEvidence() error = %v, want nil", err)
	}
	support := got.Support
	if gotCount := IntVal(support, "evidence_count"); gotCount != 0 {
		t.Fatalf("evidence_count = %d, want 0", gotCount)
	}
	missing := mapSliceValue(support, "missing_evidence")
	if gotReason := StringVal(missing[0], "reason"); gotReason != "support_source_only_not_target_linked" {
		t.Fatalf("missing_evidence[0].reason = %q, want support_source_only_not_target_linked", gotReason)
	}
	coverage := mapValue(support, "coverage")
	if gotCount, want := IntVal(coverage, "source_only_count"), 2; gotCount != want {
		t.Fatalf("coverage.source_only_count = %d, want %d", gotCount, want)
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
		"fact.fact_kind = ANY($1::text[])",
		"generation.status = 'active'",
		"jsonb_array_length",
	)
	for _, forbidden := range []string{"fact.payload AS", "source_record_id", "ORDER BY", "LIMIT"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("source-only support SQL leaked row-shaped fragment %q:\n%s", forbidden, query)
		}
	}
	if len(args) != 1 {
		t.Fatalf("args len = %d, want fact kind array only", len(args))
	}
}
