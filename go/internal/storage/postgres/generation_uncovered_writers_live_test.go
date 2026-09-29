// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

// uncoveredWritersScope seeds scope-uw. Rows are added per case with
// uncoveredWriterRow.
const uncoveredWritersScope = `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES ('scope-uw', 'repository', 'github', 'proof/uw', 'git',
          'proof/uw', now(), now(), 'active');
`

// uncoveredWriterRow inserts one generation of scope-uw. ingested and pws are
// minutes before now (pws < 0 means NULL); activated marks activated_at.
func uncoveredWriterRow(id, status string, isDelta bool, ingested, pws int, activated bool, class string) string {
	pwsSQL := "NULL"
	if pws >= 0 {
		pwsSQL = "now() - make_interval(mins => " + strconv.Itoa(pws) + ")"
	}
	activatedSQL := "NULL"
	if activated {
		activatedSQL = "now() - make_interval(mins => " + strconv.Itoa(ingested) + ")"
	}
	deltaSQL := "false"
	if isDelta {
		deltaSQL = "true"
	}
	row := `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at,
    status, activated_at, projection_write_started_at)
VALUES ('` + id + `', 'scope-uw', 'push', ` + deltaSQL + `, now() - make_interval(mins => ` + strconv.Itoa(ingested) + `),
    now() - make_interval(mins => ` + strconv.Itoa(ingested) + `), '` + status + `', ` + activatedSQL + `, ` + pwsSQL + `);`
	if class != "" {
		row += `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, failure_class, payload, created_at, updated_at)
VALUES ('projector_scope-uw_` + id + `', 'scope-uw', '` + id + `', 'projector', 'source_local',
    'superseded', 1, '` + class + `', '{}'::jsonb, now(), now());`
	}
	return row
}

// TestUncoveredProjectionWritersLive runs the #7389 probe rows on real
// Postgres: a dirty writer is reported, a later activated full covers it,
// pending is excluded, failed is included, an activated writer is excluded,
// and a pre-migration full (NULL write start) or no full at all fails closed.
func TestUncoveredProjectionWritersLive(t *testing.T) {
	dsn := supersessionProofDSN(t)
	for _, tc := range []struct {
		name  string
		rows  []string
		want  []string
		class string
	}{
		{
			name: "dirty_superseded_writer",
			rows: []string{
				uncoveredWriterRow("full-a", "active", false, 60, 59, true, ""),
				uncoveredWriterRow("delta-b", "superseded", true, 50, 49, false, "projector_superseded_by_newer_generation"),
			},
			want:  []string{"delta-b"},
			class: "projector_superseded_by_newer_generation",
		},
		{
			name: "covered_by_later_full",
			rows: []string{
				uncoveredWriterRow("full-a", "superseded", false, 60, 59, true, ""),
				uncoveredWriterRow("delta-b", "superseded", true, 50, 49, false, "projector_superseded_by_newer_generation"),
				uncoveredWriterRow("full-c", "active", false, 40, 39, true, ""),
			},
		},
		{
			name: "pending_excluded_failed_included",
			rows: []string{
				uncoveredWriterRow("full-a", "active", false, 60, 59, true, ""),
				uncoveredWriterRow("pending-p", "pending", true, 50, 49, false, ""),
				uncoveredWriterRow("failed-f", "failed", true, 45, 44, false, "projection_failure"),
			},
			want:  []string{"failed-f"},
			class: "projection_failure",
		},
		{
			name: "activated_writer_excluded",
			rows: []string{
				uncoveredWriterRow("full-a", "superseded", false, 60, 59, true, ""),
				uncoveredWriterRow("delta-b", "superseded", true, 50, 49, true, ""),
				uncoveredWriterRow("delta-c", "active", true, 40, 39, true, ""),
			},
		},
		{
			name: "pre_migration_full_fails_closed",
			rows: []string{
				uncoveredWriterRow("full-old", "superseded", false, 90, -1, true, ""),
				uncoveredWriterRow("delta-b", "superseded", true, 80, 79, false, ""),
				uncoveredWriterRow("full-a", "active", false, 60, -1, true, ""),
			},
			want: []string{"delta-b"},
		},
		{
			name: "no_full_fails_closed",
			rows: []string{
				uncoveredWriterRow("delta-b", "superseded", true, 50, 49, false, ""),
			},
			want: []string{"delta-b"},
		},
		{
			name: "writer_older_than_full_write_start_is_covered",
			rows: []string{
				uncoveredWriterRow("delta-b", "superseded", true, 70, 55, false, ""),
				uncoveredWriterRow("full-a", "active", false, 60, 50, true, ""),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := openLivenessProofDB(t, dsn)
			provisionLivenessSchema(t, database, uncoveredWritersScope+strings.Join(tc.rows, "\n"))
			store := NewIngestionStore(SQLDB{DB: database})
			writers, err := store.UncoveredProjectionWriters(context.Background(), "scope-uw")
			if err != nil {
				t.Fatalf("UncoveredProjectionWriters() = %v", err)
			}
			got := make([]string, 0, len(writers))
			for _, writer := range writers {
				got = append(got, writer.GenerationID)
				if writer.ProjectionWriteStartedAt.IsZero() {
					t.Errorf("writer %s has no write start", writer.GenerationID)
				}
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("uncovered writers = %v, want %v", got, tc.want)
			}
			if tc.class != "" && writers[0].FailureClass != tc.class {
				t.Fatalf("failure class = %q, want %q", writers[0].FailureClass, tc.class)
			}
		})
	}
}
