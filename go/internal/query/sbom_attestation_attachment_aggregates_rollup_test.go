package query

import (
	"database/sql"
	"strings"
	"testing"
)

// TestSBOMAttestationAttachmentAggregateQueriesKeepActiveScanAnchor pins the
// bounded scan shape that #3389 relies on. The count, group, and inventory
// queries each run COUNT(*) / GROUP BY over one fact_kind's active tuples with
// no payload anchor in the common case, so each must keep its single-fact_kind
// predicate, `is_tombstone = FALSE`, and the active-generation join. Those are
// exactly the columns the partial index
// fact_records_sbom_attestation_attachments_active_scan_idx is built on (index
// presence pinned in
// go/internal/storage/postgres/schema_fact_records_sbom_test.go). If a later
// edit drops the active filter or broadens the fact_kind, the planner can no
// longer bound the scan to one kind's active rows and the whole-table scan
// regression from #3389 returns.
func TestSBOMAttestationAttachmentAggregateQueriesKeepActiveScanAnchor(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"rollup":    sbomAttestationAttachmentAggregateRollupQuery,
		"inventory": sbomAttestationAttachmentInventoryQueryTemplate,
	} {
		for _, want := range []string{
			"WHERE fact.fact_kind = 'reducer_sbom_attestation_attachment'",
			"AND fact.is_tombstone = FALSE",
			"ON scope.scope_id = fact.scope_id\n AND scope.active_generation_id = fact.generation_id",
			"AND generation.status = 'active'",
		} {
			if !strings.Contains(query, want) {
				t.Fatalf("%s aggregate query missing #3389 bounded-scan anchor %q:\n%s", name, want, query)
			}
		}
	}
}

func TestSBOMAttestationAttachmentAggregateQueriesFilterSourceScopes(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.payload->'repository_ids' ? $6",
		"fact.payload->'workload_ids' ? $7",
		"fact.payload->'service_ids' ? $8",
	} {
		if !strings.Contains(sbomAttestationAttachmentAggregateRollupQuery, want) {
			t.Fatalf("rollup query missing source-scope predicate %q:\n%s", want, sbomAttestationAttachmentAggregateRollupQuery)
		}
		if !strings.Contains(sbomAttestationAttachmentInventoryQueryTemplate, want) {
			t.Fatalf("inventory query missing source-scope predicate %q:\n%s", want, sbomAttestationAttachmentInventoryQueryTemplate)
		}
	}
}

// TestSBOMAttestationAttachmentAggregateRollupUsesSinglePassGroupingSets pins
// the #3389 count reshape: the count handler computes total + per-status +
// per-kind in one GROUPING SETS scan instead of three separate queries, with the
// GROUPING() flags the Go partition relies on.
func TestSBOMAttestationAttachmentAggregateRollupUsesSinglePassGroupingSets(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"GROUP BY GROUPING SETS",
		"GROUPING(COALESCE(NULLIF(fact.payload->>'attachment_status', ''), 'unknown')) AS grouping_status",
		"GROUPING(COALESCE(NULLIF(fact.payload->>'artifact_kind', ''), 'unknown')) AS grouping_kind",
	} {
		if !strings.Contains(sbomAttestationAttachmentAggregateRollupQuery, want) {
			t.Fatalf("rollup query missing single-pass marker %q:\n%s", want, sbomAttestationAttachmentAggregateRollupQuery)
		}
	}
}

// TestBuildSBOMAttestationAttachmentAggregateCount verifies the GROUPING SETS
// rows partition into the same envelope the previous COUNT(*) + two GROUP BY
// trio produced: grand total from the (1,1) row, attachment_status buckets from
// grouping_status=0 rows, artifact_kind buckets from grouping_kind=0 rows.
func TestBuildSBOMAttestationAttachmentAggregateCount(t *testing.T) {
	t.Parallel()

	rows := []sbomAttestationAttachmentRollupRow{
		{groupingStatus: 0, groupingKind: 1, attachmentStatus: sql.NullString{String: "attached_verified", Valid: true}, count: 10},
		{groupingStatus: 0, groupingKind: 1, attachmentStatus: sql.NullString{String: "attached_unverified", Valid: true}, count: 5},
		{groupingStatus: 0, groupingKind: 1, attachmentStatus: sql.NullString{String: "subject_mismatch", Valid: true}, count: 3},
		{groupingStatus: 1, groupingKind: 0, artifactKind: sql.NullString{String: "sbom", Valid: true}, count: 12},
		{groupingStatus: 1, groupingKind: 0, artifactKind: sql.NullString{String: "attestation", Valid: true}, count: 6},
		// Grand-total row: GROUPING SETS emits NULL for both grouping columns (#3547).
		{groupingStatus: 1, groupingKind: 1, attachmentStatus: sql.NullString{Valid: false}, artifactKind: sql.NullString{Valid: false}, count: 18},
	}
	out := buildSBOMAttestationAttachmentAggregateCount(rows)

	if out.TotalAttachments != 18 {
		t.Fatalf("TotalAttachments = %d, want 18", out.TotalAttachments)
	}
	wantStatus := map[string]int{"attached_verified": 10, "attached_unverified": 5, "subject_mismatch": 3}
	if len(out.ByAttachmentStatus) != len(wantStatus) {
		t.Fatalf("ByAttachmentStatus = %v, want %v", out.ByAttachmentStatus, wantStatus)
	}
	for k, v := range wantStatus {
		if out.ByAttachmentStatus[k] != v {
			t.Fatalf("ByAttachmentStatus[%s] = %d, want %d", k, out.ByAttachmentStatus[k], v)
		}
	}
	wantKind := map[string]int{"sbom": 12, "attestation": 6}
	for k, v := range wantKind {
		if out.ByArtifactKind[k] != v {
			t.Fatalf("ByArtifactKind[%s] = %d, want %d", k, out.ByArtifactKind[k], v)
		}
	}
	// The grand-total row must not leak into either bucket map.
	if _, ok := out.ByAttachmentStatus[""]; ok {
		t.Fatal("ByAttachmentStatus contains the rolled-up grand-total row")
	}
	if _, ok := out.ByArtifactKind[""]; ok {
		t.Fatal("ByArtifactKind contains the rolled-up grand-total row")
	}
}

// TestBuildSBOMAttestationAttachmentAggregateCountHandlesNullGroupingColumns is the
// regression test for #3547. GROUPING SETS emits NULL for every column that is not
// part of a given grouping set. The grand-total row (groupingStatus=1, groupingKind=1)
// has both attachment_status and artifact_kind as NULL. Before the fix the scan
// targets were plain string fields and sql.Scan returned
// "converting NULL to string is unsupported"; after the fix they are sql.NullString.
// This test constructs the exact row shape that was previously rejected and asserts
// that the count envelope is correctly assembled.
func TestBuildSBOMAttestationAttachmentAggregateCountHandlesNullGroupingColumns(t *testing.T) {
	t.Parallel()

	// Mimic exactly what Postgres emits for a dataset with two status buckets,
	// one kind bucket, and a grand total — the grand-total row has NULL for both
	// grouping columns. This is the shape that previously caused the 500.
	rows := []sbomAttestationAttachmentRollupRow{
		// attachment_status bucket: Valid=true
		{groupingStatus: 0, groupingKind: 1, attachmentStatus: sql.NullString{String: "attached_verified", Valid: true}, count: 7},
		// artifact_kind bucket: Valid=true
		{groupingStatus: 1, groupingKind: 0, artifactKind: sql.NullString{String: "sbom", Valid: true}, count: 7},
		// Grand-total row: both columns NULL (#3547 root cause)
		{groupingStatus: 1, groupingKind: 1, attachmentStatus: sql.NullString{Valid: false}, artifactKind: sql.NullString{Valid: false}, count: 7},
	}

	out := buildSBOMAttestationAttachmentAggregateCount(rows)

	if out.TotalAttachments != 7 {
		t.Fatalf("TotalAttachments = %d, want 7", out.TotalAttachments)
	}
	if got := out.ByAttachmentStatus["attached_verified"]; got != 7 {
		t.Fatalf("ByAttachmentStatus[attached_verified] = %d, want 7", got)
	}
	if got := out.ByArtifactKind["sbom"]; got != 7 {
		t.Fatalf("ByArtifactKind[sbom] = %d, want 7", got)
	}
	// The grand-total row must not leak a "" key into either bucket map.
	if _, ok := out.ByAttachmentStatus[""]; ok {
		t.Fatal("ByAttachmentStatus must not contain empty-string key from NULL grand-total row (#3547)")
	}
	if _, ok := out.ByArtifactKind[""]; ok {
		t.Fatal("ByArtifactKind must not contain empty-string key from NULL grand-total row (#3547)")
	}
}

func TestNextSBOMAttestationAttachmentAggregateOffsetBound(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		offset    int
		limit     int
		truncated bool
		want      any
	}{
		{"not truncated returns nil", 0, 100, false, nil},
		{"normal next offset", 200, 100, true, 300},
		{"exactly at ceiling boundary returns ceiling", 9900, 100, true, 10000},
		{"would exceed ceiling returns nil", 9950, 100, true, nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := nextSBOMAttestationAttachmentAggregateOffset(tc.offset, tc.limit, tc.truncated)
			if got != tc.want {
				t.Fatalf("nextSBOMAttestationAttachmentAggregateOffset(%d, %d, %v) = %v, want %v",
					tc.offset, tc.limit, tc.truncated, got, tc.want)
			}
		})
	}
}

func TestSBOMAttestationAttachmentInventoryGroupExpressionEnumIsClosed(t *testing.T) {
	t.Parallel()

	cases := []SBOMAttestationAttachmentInventoryDimension{
		SBOMAttestationAttachmentInventoryByAttachmentStatus,
		SBOMAttestationAttachmentInventoryByArtifactKind,
		SBOMAttestationAttachmentInventoryBySubjectDigest,
	}
	for _, dim := range cases {
		if _, err := sbomAttestationAttachmentInventoryGroupExpression(dim); err != nil {
			t.Fatalf("dimension %q must be supported: %v", dim, err)
		}
	}
	if _, err := sbomAttestationAttachmentInventoryGroupExpression("document_id"); err == nil {
		t.Fatal("sbomAttestationAttachmentInventoryGroupExpression must reject unknown dimensions to keep SQL substitution safe")
	}
}
