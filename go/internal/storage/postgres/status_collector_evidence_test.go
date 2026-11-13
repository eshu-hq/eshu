// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
		"claim_ecosystem",
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
				{"documentation", "collector-documentation", "source_facts", []string{"confluence"}, int64(5), now.Add(-3 * time.Minute), now.Add(-2 * time.Minute)},
				{"git", "collector-git-default", "source_facts", []string{"git"}, int64(17), now.Add(-5 * time.Minute), now.Add(-4 * time.Minute)},
				{"aws", "collector-aws", "reducer_facts", []string{"aws"}, int64(2), now.Add(-4 * time.Minute), now.Add(-1 * time.Minute)},
			},
		}},
	}

	got, err := readCollectorFactEvidence(context.Background(), queryer)
	if err != nil {
		t.Fatalf("readCollectorFactEvidence() error = %v, want nil", err)
	}
	if len(got) != 3 {
		t.Fatalf("collector fact evidence rows = %d, want 3", len(got))
	}
	if got[0].CollectorKind != "documentation" ||
		got[0].InstanceID != "collector-documentation" ||
		got[0].EvidenceSource != "source_facts" ||
		got[0].ObservationCount != 5 ||
		!stringSliceContains(got[0].SourceSystems, "confluence") {
		t.Fatalf("documentation evidence row = %#v", got[0])
	}
	if got[1].CollectorKind != "git" ||
		got[1].InstanceID != "collector-git-default" ||
		got[1].EvidenceSource != "source_facts" ||
		got[1].ObservationCount != 17 ||
		!stringSliceContains(got[1].SourceSystems, "git") {
		t.Fatalf("Git evidence row = %#v", got[1])
	}
	if got[2].CollectorKind != "aws" ||
		got[2].InstanceID != "collector-aws" ||
		got[2].EvidenceSource != "reducer_facts" ||
		got[2].ObservationCount != 2 ||
		!stringSliceContains(got[2].SourceSystems, "aws") {
		t.Fatalf("AWS evidence row = %#v", got[2])
	}

	query := strings.Join(queryer.queries, "\n")
	for _, want := range []string{
		"active_scopes AS (",
		"JOIN collector_evidence_summary AS summary",
		"summary.generation_id = scope.generation_id",
		"SUM(summary.observation_count) AS observation_count",
		"ARRAY_AGG(DISTINCT summary.source_system ORDER BY summary.source_system)",
		"FILTER (WHERE summary.source_system <> '')",
		"AS source_systems",
		"collector_kind IN (",
		"'git'",
		"'ci_cd_run'",
		"LEFT JOIN LATERAL (",
		"FROM workflow_work_items AS workflow_item",
		"LIMIT 200",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("collector fact evidence query missing %q:\n%s", want, query)
		}
	}
	// The #3466 read must never scan fact_records; the aggregate is precomputed
	// into collector_evidence_summary by the reducer resweep.
	for _, forbidden := range []string{"fact_records", "fact.payload", "source_uri", "source_record_id"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("collector fact evidence query must not reference %q:\n%s", forbidden, query)
		}
	}
}

// TestCollectorFactEvidenceQueryPreAggregatesBeforeWorkflowIdentity guards
// the #3466 aggregate shape as amended by #10601: the summary aggregate still
// reads the precomputed collector_evidence_summary, and the collector-instance
// leg is the scoped lateral lookup (one indexed probe per active scope), not
// a DISTINCT ON over the joined set.
func TestCollectorFactEvidenceQueryPreAggregatesBeforeWorkflowIdentity(t *testing.T) {
	t.Parallel()

	query := collectorFactEvidenceQuery
	for _, want := range []string{
		"active_scopes AS (",
		"LEFT JOIN LATERAL (",
		"FROM workflow_work_items AS workflow_item",
		"SUM(summary.observation_count) AS observation_count",
		"JOIN collector_evidence_summary AS summary",
		"GROUP BY summary.collector_kind, collector_instance_id, summary.evidence_source",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("collector fact evidence query missing bounded aggregate marker %q:\n%s", want, query)
		}
	}
}

// TestCollectorFactEvidenceQueryUsesLateralInstanceLookup guards #10601: the
// collector-instance leg must be one indexed lookup per active scope
// (LEFT JOIN LATERAL ... LIMIT 1 over workflow_work_items keyed by
// collector_kind, scope_id, generation_id in updated_at DESC,
// work_item_id ASC order), evaluated before the summary fanout and never a
// DISTINCT ON over a joined set that the outer aggregate then re-scans per
// summary row. On QA at 5376 active scopes the materialized DISTINCT ON CTE
// cost 13.8s per snapshot (44M-row CTE-scan join filter) against the 5s
// snapshot budget; the inline lateral leg costs 3.5s cold / 1.1s warm with
// byte-identical rows (43-row QA diff empty).
func TestCollectorFactEvidenceQueryUsesLateralInstanceLookup(t *testing.T) {
	t.Parallel()

	query := collectorFactEvidenceQuery
	for _, want := range []string{
		"FROM active_scopes AS scope",
		"LEFT JOIN LATERAL (",
		"FROM workflow_work_items AS workflow_item",
		"AND NULLIF(BTRIM(workflow_item.collector_instance_id), '') IS NOT NULL",
		"ORDER BY workflow_item.updated_at DESC, workflow_item.work_item_id ASC",
		"LIMIT 1",
		") AS item ON TRUE",
		"JOIN collector_evidence_summary AS summary",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("collector fact evidence query missing lateral instance-lookup marker %q:\n%s", want, query)
		}
	}
	for _, forbidden := range []string{
		"workflow_instances AS MATERIALIZED (",
		"DISTINCT ON (",
		"scoped_instance AS (",
		"fact_records",
	} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("collector fact evidence query must not contain %q (the #10601 join fanout):\n%s", forbidden, query)
		}
	}
}

// TestCollectorFactEvidenceQueryReadsSummaryNotFactRecords guards the #3466 fix:
// the readiness read is the bounded contract. It MUST join the precomputed
// collector_evidence_summary materialized by the reducer resweep and MUST NOT
// scan fact_records (the source of the 5.4–9.2s / 6.6M-row read). The per-scope
// fact LATERAL aggregate moved into the resweep statement
// (rebuildCollectorEvidenceSummarySQL), guarded by
// TestRebuildCollectorEvidenceSQLIsAtomicUpsertDeleteStale. The #10601
// instance-identity LATERAL is over workflow_work_items (one indexed probe
// per active scope), not over facts, so it is allowed here: only fact-side
// scans are forbidden.
func TestCollectorFactEvidenceQueryReadsSummaryNotFactRecords(t *testing.T) {
	t.Parallel()

	query := collectorFactEvidenceQuery
	for _, want := range []string{
		"JOIN collector_evidence_summary AS summary",
		"summary.scope_id = scope.scope_id",
		"summary.generation_id = scope.generation_id",
		"FILTER (WHERE summary.source_system <> '')",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("collector fact evidence query missing summary-read marker %q:\n%s", want, query)
		}
	}
	for _, forbidden := range []string{"fact_records", "fact_summary AS"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("collector fact evidence read must not contain %q (it must read the summary, not scan facts):\n%s", forbidden, query)
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
