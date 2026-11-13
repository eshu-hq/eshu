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
		"summary.collector_instance_id",
		"LIMIT 200",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("collector fact evidence query missing %q:\n%s", want, query)
		}
	}
	// The #3466 read must never scan fact_records; the aggregate is precomputed
	// into collector_evidence_summary by the reducer resweep. The #10601 leg1
	// summary read (reopened) also probes no per-scope state: the instance
	// identity is resweep-maintained, so workflow_work_items stays out too.
	for _, forbidden := range []string{"fact_records", "fact.payload", "source_uri", "source_record_id", "workflow_work_items", "LEFT JOIN LATERAL ("} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("collector fact evidence query must not reference %q:\n%s", forbidden, query)
		}
	}
}

// TestCollectorFactEvidenceQueryPreAggregatesBeforeWorkflowIdentity guards
// the #3466 aggregate shape as amended by the #10601 leg1 summary read
// (reopened): the summary aggregate still reads the precomputed
// collector_evidence_summary, and the collector-instance identity comes from
// the resweep-maintained summary.collector_instance_id column — not a lateral
// probe, and not a DISTINCT ON over the joined set.
func TestCollectorFactEvidenceQueryPreAggregatesBeforeWorkflowIdentity(t *testing.T) {
	t.Parallel()

	query := collectorFactEvidenceQuery
	for _, want := range []string{
		"active_scopes AS (",
		"summary.collector_instance_id",
		"SUM(summary.observation_count) AS observation_count",
		"JOIN collector_evidence_summary AS summary",
		"GROUP BY summary.collector_kind, summary.collector_instance_id, summary.evidence_source",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("collector fact evidence query missing bounded aggregate marker %q:\n%s", want, query)
		}
	}
}

// TestCollectorFactEvidenceQueryReadsInstanceFromSummary guards the #10601
// leg1 summary read (reopened): the collector-instance identity is maintained
// by the resweep (rebuildCollectorEvidenceSummarySQL keeps
// summary.collector_instance_id with the pre-change DISTINCT ON semantics),
// so the snapshot read is a pure summary read. The per-scope lateral probe of
// workflow_work_items that the first #10601 fix shipped still cost 4.1s cold /
// 1.1s warm on QA at 6139 active scopes (one indexed probe per active scope
// plus the 10.5k-row summary scan against the 5s snapshot budget); the
// summary read keeps the emitted rows shape- and value-identical (see
// collector_evidence_summary_read_live_test.go).
func TestCollectorFactEvidenceQueryReadsInstanceFromSummary(t *testing.T) {
	t.Parallel()

	query := collectorFactEvidenceQuery
	for _, want := range []string{
		"FROM active_scopes AS scope",
		"JOIN collector_evidence_summary AS summary",
		"summary.collector_instance_id",
		"GROUP BY summary.collector_kind, summary.collector_instance_id, summary.evidence_source",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("collector fact evidence query missing summary-read marker %q:\n%s", want, query)
		}
	}
	for _, forbidden := range []string{
		"LEFT JOIN LATERAL (",
		"FROM workflow_work_items AS workflow_item",
		") AS item ON TRUE",
		"workflow_instances AS MATERIALIZED (",
		"DISTINCT ON (",
		"scoped_instance AS (",
		"fact_records",
	} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("collector fact evidence query must not contain %q (the snapshot read must not probe per-scope state):\n%s", forbidden, query)
		}
	}
}

// TestCollectorFactEvidenceQueryReadsSummaryNotFactRecords guards the #3466 fix:
// the readiness read is the bounded contract. It MUST join the precomputed
// collector_evidence_summary materialized by the reducer resweep and MUST NOT
// scan fact_records (the source of the 5.4–9.2s / 6.6M-row read). The per-scope
// fact LATERAL aggregate moved into the resweep statement
// (rebuildCollectorEvidenceSummarySQL), guarded by
// TestRebuildCollectorEvidenceSQLIsAtomicUpsertDeleteStale, and the per-scope
// instance-identity lookup moved with it (guarded by
// TestRebuildCollectorEvidenceSQLMaintainsCollectorInstance). Both legs live
// in the resweep now, so the read probes no per-scope state at all.
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
