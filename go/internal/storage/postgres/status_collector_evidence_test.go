package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestReadRegistryCollectorSnapshotsUsesBoundedStatusOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 14, 30, 0, 0, time.UTC)
	queryer := &fakeQueryer{
		responses: []fakeRows{
			{
				rows: [][]any{
					{
						"oci_registry",
						int64(2),
						int64(3),
						int64(8),
						now.Add(-2 * time.Minute),
						int64(1),
						int64(0),
					},
					{
						"package_registry",
						int64(1),
						int64(1),
						int64(4),
						now.Add(-5 * time.Minute),
						int64(0),
						int64(1),
					},
				},
			},
			{
				rows: [][]any{
					{"package_registry", "npm", int64(5), int64(3), int64(1), int64(1), int64(1), int64(1)},
				},
			},
			{
				rows: [][]any{
					{"oci_registry", "registry_rate_limited", int64(2)},
					{"package_registry", "registry_auth_denied", int64(1)},
				},
			},
		},
	}

	got, err := readRegistryCollectorSnapshots(context.Background(), queryer, now)
	if err != nil {
		t.Fatalf("readRegistryCollectorSnapshots() error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("readRegistryCollectorSnapshots() len = %d, want 2", len(got))
	}
	if got[0].CollectorKind != "oci_registry" || got[0].ConfiguredInstances != 2 ||
		got[0].ActiveScopes != 3 || got[0].RecentCompletedGenerations != 8 ||
		got[0].RetryableFailures != 1 || got[0].TerminalFailures != 0 {
		t.Fatalf("OCI registry snapshot = %#v", got[0])
	}
	if got[0].LastCompletedAt != now.Add(-2*time.Minute) {
		t.Fatalf("OCI LastCompletedAt = %v, want %v", got[0].LastCompletedAt, now.Add(-2*time.Minute))
	}
	if len(got[0].FailureClassCounts) != 1 ||
		got[0].FailureClassCounts[0].Name != "registry_rate_limited" ||
		got[0].FailureClassCounts[0].Count != 2 {
		t.Fatalf("OCI FailureClassCounts = %#v", got[0].FailureClassCounts)
	}
	if len(got[1].MetadataTargetCounts) != 1 ||
		got[1].MetadataTargetCounts[0].Ecosystem != "npm" ||
		got[1].MetadataTargetCounts[0].Planned != 5 ||
		got[1].MetadataTargetCounts[0].Completed != 3 ||
		got[1].MetadataTargetCounts[0].Skipped != 1 ||
		got[1].MetadataTargetCounts[0].Stale != 1 ||
		got[1].MetadataTargetCounts[0].Failed != 1 ||
		got[1].MetadataTargetCounts[0].RateLimited != 1 {
		t.Fatalf("package registry MetadataTargetCounts = %#v", got[1].MetadataTargetCounts)
	}
	joinedQueries := strings.Join(queryer.queries, "\n")
	for _, privateColumn := range []string{"repository_path", "package_name", "metadata_url", "credential_env", "credential_value"} {
		if strings.Contains(strings.ToLower(joinedQueries), privateColumn) {
			t.Fatalf("registry status query mentions private column %q:\n%s", privateColumn, joinedQueries)
		}
	}
	for _, want := range []string{
		"updated_at >= $1::timestamptz - INTERVAL '24 hours'",
		"DISTINCT ON (collector_kind)",
		"SPLIT_PART(fairness_key, ':', 4)",
		"registry_rate_limited",
	} {
		if !strings.Contains(joinedQueries, want) {
			t.Fatalf("registry status query missing %q:\n%s", want, joinedQueries)
		}
	}
	if strings.Contains(joinedQueries, "'unknown'") {
		t.Fatalf("registry status query still emits unreachable unknown failure class:\n%s", joinedQueries)
	}
}

func TestReadCollectorFactEvidenceUsesBoundedActiveFactMetadata(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 7, 11, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{
		responses: []fakeRows{{
			rows: [][]any{
				{"documentation", "collector-documentation", "source_facts", int64(5), now.Add(-3 * time.Minute), now.Add(-2 * time.Minute)},
				{"aws", "collector-aws", "reducer_facts", int64(2), now.Add(-4 * time.Minute), now.Add(-1 * time.Minute)},
			},
		}},
	}

	got, err := readCollectorFactEvidence(context.Background(), queryer)
	if err != nil {
		t.Fatalf("readCollectorFactEvidence() error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("collector fact evidence rows = %d, want 2", len(got))
	}
	if got[0].CollectorKind != "documentation" ||
		got[0].InstanceID != "collector-documentation" ||
		got[0].EvidenceSource != "source_facts" ||
		got[0].ObservationCount != 5 {
		t.Fatalf("documentation evidence row = %#v", got[0])
	}
	if got[1].CollectorKind != "aws" ||
		got[1].InstanceID != "collector-aws" ||
		got[1].EvidenceSource != "reducer_facts" ||
		got[1].ObservationCount != 2 {
		t.Fatalf("AWS evidence row = %#v", got[1])
	}

	query := strings.Join(queryer.queries, "\n")
	for _, want := range []string{
		"JOIN fact_records AS fact",
		"fact.generation_id = scope.active_generation_id",
		"fact.is_tombstone = FALSE",
		"COUNT(*) AS observation_count",
		"WHEN fact.fact_kind LIKE 'reducer_%' THEN 'reducer_facts'",
		"collector_kind IN (",
		"FROM workflow_work_items AS workflow_item",
		"LIMIT 200",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("collector fact evidence query missing %q:\n%s", want, query)
		}
	}
	for _, forbidden := range []string{"fact.payload", "source_uri", "source_record_id"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("collector fact evidence query uses private field %q:\n%s", forbidden, query)
		}
	}
}

func TestRegistryCollectorStatusQueryCastsAsOfParameter(t *testing.T) {
	t.Parallel()

	if strings.Contains(registryCollectorStatusQuery, "$1 - INTERVAL '24 hours'") {
		t.Fatalf("registryCollectorStatusQuery leaves as-of parameter under-typed:\n%s", registryCollectorStatusQuery)
	}
	if !strings.Contains(registryCollectorStatusQuery, "$1::timestamptz - INTERVAL '24 hours'") {
		t.Fatalf("registryCollectorStatusQuery missing timestamptz cast for interval bound:\n%s", registryCollectorStatusQuery)
	}
}
