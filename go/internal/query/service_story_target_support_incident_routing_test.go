// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/support"
)

func TestBuildServiceStoryTargetSupportStatementsFollowsTheRepositoryGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		filter serviceStoryTargetSupportFilter
		want   int
	}{
		{"repository target", serviceStoryTargetSupportFilter{TargetKind: "repository", TargetID: "repo-x", Limit: 10}, 3},
		{"repository target by repository field", serviceStoryTargetSupportFilter{TargetKind: "repository", Repository: "repo-x", Limit: 10}, 3},
		{"service whose repository defines exactly it", serviceStoryTargetSupportFilter{
			TargetKind: "service", Repository: "repo-x", RepositoryDefinesTarget: true, RepositoryWorkloadCount: 1,
		}, 3},
		{"service whose repository defines several workloads", serviceStoryTargetSupportFilter{
			TargetKind: "service", Repository: "repo-x", RepositoryDefinesTarget: true, RepositoryWorkloadCount: 3,
		}, 3},
		{"service, graph did not show the repository defining it", serviceStoryTargetSupportFilter{
			TargetKind: "service", Repository: "repo-x", RepositoryWorkloadCount: 1,
		}, 0},
		{"service, repository defines none", serviceStoryTargetSupportFilter{
			TargetKind: "service", Repository: "repo-x", RepositoryDefinesTarget: true,
		}, 0},
		{"service without a repository", serviceStoryTargetSupportFilter{
			TargetKind: "service", RepositoryDefinesTarget: true, RepositoryWorkloadCount: 1,
		}, 0},
		{"target kind the story does not serve", serviceStoryTargetSupportFilter{TargetKind: "workload", TargetID: "x"}, 0},
		{"no target at all", serviceStoryTargetSupportFilter{}, 0},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			statements := buildServiceStoryTargetSupportStatements(tt.filter)
			if len(statements) != tt.want {
				t.Fatalf("statements = %d, want %d", len(statements), tt.want)
			}
			if tt.want == 0 {
				return
			}
			if !strings.Contains(statements[0].query, "'"+storySupportLinkFactKind+"'") {
				t.Fatalf("first statement is not the Jira link read:\n%s", statements[0].query)
			}
			if !strings.Contains(statements[1].query, "'"+support.IncidentCorrelationKind+"'") {
				t.Fatalf("second statement is not the PagerDuty routing read:\n%s", statements[1].query)
			}
			if !strings.Contains(statements[2].query, "'"+support.WorkItemRecordKind+"'") {
				t.Fatalf("third statement is not the Jira issue-link read:\n%s", statements[2].query)
			}
			for i, statement := range statements {
				if len(statement.args) != 2 || statement.args[0] != "repo-x" {
					t.Fatalf("statement %d args = %#v, want the repository and a row limit", i, statement.args)
				}
			}
		})
	}
}

func TestBuildServiceStoryTargetSupportStatementsBoundEachReadToTheLimitPlusOne(t *testing.T) {
	t.Parallel()

	for limit, want := range map[int]int{0: serviceStoryTargetSupportLimit + 1, 5: 6, 1000: serviceStoryTargetSupportLimit + 1} {
		statements := buildServiceStoryTargetSupportStatements(serviceStoryTargetSupportFilter{
			TargetKind: "repository", TargetID: "repo-x", Limit: limit,
		})
		for i, statement := range statements {
			if got := statement.args[1]; got != want {
				t.Fatalf("limit %d statement %d bound %v rows, want %d", limit, i, got, want)
			}
		}
	}
}

func routingRow(id, kind, observedAt string, payload map[string]any, repository, service string) map[string]any {
	row := map[string]any{
		"fact_id": id, "fact_kind": kind, "observed_at": observedAt, "payload": payload,
		"scope_id": "s", "generation_id": "g", "source_system": "x",
	}
	if repository != "" || service != "" {
		row["correlation"] = map[string]any{"repository_id": repository, "provider_service_id": service}
	}
	return row
}

func TestBuildStoryTargetSupportAttachesCorrelatedPagerDutyRowsToARepository(t *testing.T) {
	t.Parallel()

	filter := serviceStoryTargetSupportFilter{Repository: "repo-x", TargetKind: "repository", TargetID: "repo-x", Limit: 10}
	facts := []map[string]any{
		routingRow("applied", support.AppliedPagerDutyResourceKind, "2026-06-01T11:00:00+00:00",
			map[string]any{"resource_class": "service", "provider_object_id": "PA"}, "repo-x", "PA"),
		routingRow("observed", support.ObservedPagerDutyServiceKind, "2026-06-01T10:00:00+00:00",
			map[string]any{"provider_object_id": "PB", "service_id": "PB"}, "repo-x", "PB"),
		routingRow("other-repo", support.ObservedPagerDutyServiceKind, "2026-06-01T09:00:00+00:00",
			map[string]any{"provider_object_id": "PC"}, "repo-y", "PC"),
		routingRow("team", support.AppliedPagerDutyResourceKind, "2026-06-01T09:00:00+00:00",
			map[string]any{"resource_class": "team", "provider_object_id": "PA"}, "repo-x", "PA"),
		routingRow("no-correlation", support.ObservedPagerDutyServiceKind, "2026-06-01T09:00:00+00:00",
			map[string]any{"provider_object_id": "PD"}, "", ""),
	}
	section := buildStoryTargetSupport(filter, facts, false)

	if got := strings.Join(supportEvidenceFactIDs(section), ","); got != "applied,observed" {
		t.Fatalf("evidence fact ids = %q, want applied,observed (a wrong repository, a team and an uncorrelated row are dropped)", got)
	}
	for _, row := range section["evidence"].([]map[string]any) {
		if got := StringVal(row, "link_basis"); got != "incident_repository_correlation" {
			t.Fatalf("evidence %s link_basis = %q, want incident_repository_correlation", StringVal(row, "fact_id"), got)
		}
	}
	if got := IntVal(section, "incident_routing_count"); got != 2 {
		t.Fatalf("incident_routing_count = %d, want 2", got)
	}
	if got := IntVal(section, "work_item_count"); got != 0 {
		t.Fatalf("work_item_count = %d, want 0", got)
	}
}

func TestBuildStoryTargetSupportAppliesTheGraphGateToPagerDutyRows(t *testing.T) {
	t.Parallel()

	fact := routingRow("observed", support.ObservedPagerDutyServiceKind, "2026-06-01T10:00:00+00:00",
		map[string]any{"provider_object_id": "PB"}, "repo-x", "PB")
	service := func(count int, defines bool) serviceStoryTargetSupportFilter {
		return serviceStoryTargetSupportFilter{
			Repository: "repo-x", TargetKind: "service", TargetID: "workload:p", ServiceID: "workload:p",
			RepositoryWorkloadCount: count, RepositoryDefinesTarget: defines, Limit: 10,
		}
	}

	sole := buildStoryTargetSupport(service(1, true), []map[string]any{fact}, false)
	rows := sole["evidence"].([]map[string]any)
	if len(rows) != 1 || StringVal(rows[0], "link_basis") != "repository_sole_workload" {
		t.Fatalf("sole-workload service evidence = %#v, want one repository_sole_workload row", rows)
	}

	shared := buildStoryTargetSupport(service(2, true), []map[string]any{fact}, false)
	if got := IntVal(shared, "evidence_count"); got != 0 {
		t.Fatalf("shared-repository evidence_count = %d, want 0", got)
	}
	ambiguous := shared["ambiguous_evidence"].([]map[string]any)
	if len(ambiguous) != 1 || StringVal(ambiguous[0], "link_basis") != "repository_multiple_workloads" {
		t.Fatalf("shared-repository ambiguous evidence = %#v, want one repository_multiple_workloads row", ambiguous)
	}

	for name, filter := range map[string]serviceStoryTargetSupportFilter{
		"graph did not show it defining the target": service(1, false),
		"repository defines none":                   service(0, true),
	} {
		closed := buildStoryTargetSupport(filter, []map[string]any{fact}, false)
		if IntVal(closed, "evidence_count")+IntVal(closed, "ambiguous_count") != 0 {
			t.Fatalf("%s: support = %#v, want fail closed", name, closed)
		}
	}
}

// TestSortServiceStoryTargetSupportFactsOrdersByInstantNotText: the JSON
// timestamp drops trailing zeros, so "…:00.5+00:00" sorts after "…:00+00:00" as
// text but the two are half a second apart and a text sort would mis-order
// neighbours with different fractional widths.
func TestSortServiceStoryTargetSupportFactsOrdersByInstantNotText(t *testing.T) {
	t.Parallel()

	facts := []map[string]any{
		{"fact_id": "a", "observed_at": "2026-06-01T12:00:00+00:00"},
		{"fact_id": "b", "observed_at": "2026-06-01T12:00:00.45+00:00"},
		{"fact_id": "c", "observed_at": "2026-06-01T12:00:00.5+00:00"},
		{"fact_id": "d", "observed_at": "2026-06-01T12:00:00.05+00:00"},
		{"fact_id": "e", "observed_at": "2026-06-01T12:00:00+00:00"},
		{"fact_id": "f", "observed_at": "2026-06-01T08:00:00-04:00"},
	}
	sortServiceStoryTargetSupportFacts(facts)
	var got []string
	for _, fact := range facts {
		got = append(got, StringVal(fact, "fact_id"))
	}
	// f is 12:00:00 UTC too, so a, e and f tie and fall back to fact_id descending.
	if want := "c,b,d,f,e,a"; strings.Join(got, ",") != want {
		t.Fatalf("order = %v, want %s", got, want)
	}
}

// TestServiceStoryIncidentRoutingIndexesMatchQuery binds the routing statement's
// probes to the partial indexes migration 003 creates, derived from the builder
// so a predicate or key changed in the Go text without the migration fails here
// instead of silently turning an index condition into a heap filter.
func TestServiceStoryIncidentRoutingIndexesMatchQuery(t *testing.T) {
	t.Parallel()

	migration := normalizeSQLWhitespace(migrationSQLByName(t, "fact_records"))
	query, _ := support.IncidentRoutingSQL("repo-x", 10)
	query = normalizeSQLWhitespace(query)
	bare := func(expression string) string { return strings.ReplaceAll(expression, "fact.", "") }

	for _, tc := range []struct {
		index      string
		kind       string
		keys       []string
		predicates []string
	}{
		{
			index: "fact_records_incident_routing_applied_service_idx", kind: support.AppliedPagerDutyResourceKind,
			keys:       []string{bare(support.AppliedServiceKey("fact"))},
			predicates: []string{"payload->>'resource_class' = 'service'"},
		},
		{
			index: "fact_records_incident_routing_observed_service_idx", kind: support.ObservedPagerDutyServiceKind,
			keys: []string{bare(support.ObservedServiceKey("fact"))},
		},
		{
			index: "fact_records_incident_repository_correlation_service_idx", kind: support.IncidentCorrelationKind,
			keys: []string{"(payload->>'repository_id')", "(payload->>'provider_service_id')"},
		},
	} {
		definition := migrationIndexDefinition(t, migration, tc.index)
		for _, want := range append([]string{"fact_kind = '" + tc.kind + "'", "is_tombstone = FALSE"}, tc.predicates...) {
			if !strings.Contains(definition, want) {
				t.Errorf("index %s lost %q the routing statement carries:\n%s", tc.index, want, definition)
			}
			if !strings.Contains(query, want) {
				t.Errorf("routing statement lost %q that index %s is partial on:\n%s", want, tc.index, query)
			}
		}
		for _, key := range tc.keys {
			if !strings.Contains(definition, key) {
				t.Errorf("index %s is not keyed on %q:\n%s", tc.index, key, definition)
			}
		}
	}
}

func migrationIndexDefinition(t *testing.T, migration, index string) string {
	t.Helper()
	_, rest, found := strings.Cut(migration, "CREATE INDEX IF NOT EXISTS "+index+" ")
	if !found {
		t.Fatalf("migration does not create index %s", index)
	}
	definition, _, _ := strings.Cut(rest, ";")
	return definition
}
