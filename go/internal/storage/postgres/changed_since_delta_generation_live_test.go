// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// TestChangedSinceRefusesDeltaGenerationWindowLive drives the production
// ComputeChangedSinceDelta against the bootstrapped schema (#7282). A delta
// generation (scope_generations.is_delta = true) holds only the facts of the
// files that changed plus tombstones, so a raw fact-set diff against it
// reports every key the delta did not re-emit as superseded even though it
// never changed.
//
// Fixture: every full generation observes five files (f0..f4), each with a
// file fact and a content_entity fact. The delta generation re-emits a changed
// f1 and tombstones f4. Scope delta-current activates that delta. Scope
// delta-between follows the same delta with a full generation that observes
// f0..f3 with the changed f1.
//
// Contract:
//
//   - full baseline, delta current: refused as baseline_not_comparable, no
//     diff, every category unavailable;
//   - delta baseline, full current: refused the same way;
//   - full baseline, delta strictly between, full current: diffed, because both
//     endpoints are complete snapshots (f1 updated, f4 superseded, the rest
//     unchanged).
//
// Locally:
//
//	ESHU_POSTGRES_TEST_DSN=postgresql://user:pass@localhost:<port>/eshu \
//	go test ./internal/storage/postgres \
//	  -run TestChangedSinceRefusesDeltaGenerationWindowLive -count=1 -v
func TestChangedSinceRefusesDeltaGenerationWindowLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	db, _ := openServiceLineageSchemaLive(ctx, t, "eshu_7282_delta_window")
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap: %v", err)
	}
	seedChangedSinceDeltaWindow(ctx, t, db)
	store := NewStatusStore(SQLDB{DB: db})

	compute := func(t *testing.T, scopeID, since string) statuspkg.ChangedSinceSummary {
		t.Helper()
		summary, err := store.ComputeChangedSinceDelta(ctx, statuspkg.ChangedSinceFilter{
			ScopeID: scopeID, SinceGenerationID: since, SampleLimit: 10,
		})
		if err != nil {
			t.Fatalf("ComputeChangedSinceDelta(%s, %s): %v", scopeID, since, err)
		}
		return summary
	}
	assertRefused := func(t *testing.T, got statuspkg.ChangedSinceSummary, since, current string, sinceDelta, currentDelta bool) {
		t.Helper()
		if !got.Unavailable || got.UnavailableReason != statuspkg.ChangedSinceUnavailableBaselineNotComparable {
			t.Fatalf("unavailable=%v reason=%q categories=%+v; want the raw diff refused as %q",
				got.Unavailable, got.UnavailableReason, got.Categories, statuspkg.ChangedSinceUnavailableBaselineNotComparable)
		}
		if got.SinceGenerationID != since || got.CurrentActiveGenerationID != current {
			t.Fatalf("window = %s -> %s, want %s -> %s", got.SinceGenerationID, got.CurrentActiveGenerationID, since, current)
		}
		if got.SinceIsDelta != sinceDelta || got.CurrentIsDelta != currentDelta {
			t.Fatalf("since_is_delta=%v current_is_delta=%v, want %v and %v",
				got.SinceIsDelta, got.CurrentIsDelta, sinceDelta, currentDelta)
		}
		if len(got.Categories) != len(statuspkg.ChangedSinceCategories) {
			t.Fatalf("categories = %d, want %d", len(got.Categories), len(statuspkg.ChangedSinceCategories))
		}
		for _, category := range got.Categories {
			if !category.Unavailable || category.Counts != (statuspkg.ChangedSinceCounts{}) || len(category.Samples) > 0 {
				t.Fatalf("category %s = %+v; want unavailable with no counts or samples", category.Category, category)
			}
		}
	}

	t.Run("full baseline and delta current is refused", func(t *testing.T) {
		assertRefused(t, compute(t, "delta-current", "dc-full"), "dc-full", "dc-delta", false, true)
	})

	t.Run("delta baseline and full current is refused", func(t *testing.T) {
		assertRefused(t, compute(t, "delta-between", "db-delta"), "db-delta", "db-full-2", true, false)
	})

	t.Run("delta strictly between two full endpoints is diffed", func(t *testing.T) {
		got := compute(t, "delta-between", "db-full-1")
		if got.Unavailable || got.UnavailableReason != "" || got.SinceIsDelta || got.CurrentIsDelta {
			t.Fatalf("summary = %+v; want a served diff between two full generations", got)
		}
		want := statuspkg.ChangedSinceCounts{Updated: 1, Unchanged: 3, Superseded: 1}
		for _, category := range got.Categories {
			switch category.Category {
			case statuspkg.ChangedSinceCategoryFiles, statuspkg.ChangedSinceCategoryContentEntities:
				if category.Counts != want {
					t.Errorf("%s counts = %+v, want %+v", category.Category, category.Counts, want)
				}
			default:
				if category.Counts != (statuspkg.ChangedSinceCounts{}) {
					t.Errorf("%s counts = %+v, want none", category.Category, category.Counts)
				}
			}
		}
	})
}

// seedChangedSinceDeltaWindow writes the two scopes, their generations, and
// their fact_records rows through the bootstrapped schema.
func seedChangedSinceDeltaWindow(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
VALUES
  ('delta-current', 'repository', 'git', 'repo-delta-current', 'git', 'p', now(), now(), 'active'),
  ('delta-between', 'repository', 'git', 'repo-delta-between', 'git', 'p', now(), now(), 'active');
INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at, status, activated_at, superseded_at)
VALUES
  ('dc-full',   'delta-current', 'snapshot', false, now() - interval '2 hours', now(), 'superseded', now() - interval '2 hours', now() - interval '1 hour'),
  ('dc-delta',  'delta-current', 'snapshot', true,  now() - interval '1 hour',  now(), 'active',     now() - interval '1 hour',  NULL),
  ('db-full-1', 'delta-between', 'snapshot', false, now() - interval '3 hours', now(), 'superseded', now() - interval '3 hours', now() - interval '2 hours'),
  ('db-delta',  'delta-between', 'snapshot', true,  now() - interval '2 hours', now(), 'superseded', now() - interval '2 hours', now() - interval '1 hour'),
  ('db-full-2', 'delta-between', 'snapshot', false, now() - interval '1 hour',  now(), 'active',     now() - interval '1 hour',  NULL);
UPDATE ingestion_scopes SET active_generation_id = 'dc-delta' WHERE scope_id = 'delta-current';
UPDATE ingestion_scopes SET active_generation_id = 'db-full-2' WHERE scope_id = 'delta-between';`); err != nil {
		t.Fatalf("seed scopes and generations: %v", err)
	}

	type fact struct {
		scope, generation, file, version string
		tombstone                        bool
	}
	var rows []fact
	full := func(scope, generation string, files int, f1Version string) {
		for i := 0; i < files; i++ {
			version := "v1"
			if i == 1 {
				version = f1Version
			}
			rows = append(rows, fact{scope, generation, fmt.Sprintf("f%d", i), version, false})
		}
	}
	delta := func(scope, generation string) {
		rows = append(rows,
			fact{scope, generation, "f1", "v2", false},
			fact{scope, generation, "f4", "v1", true},
		)
	}
	full("delta-current", "dc-full", 5, "v1")
	delta("delta-current", "dc-delta")
	full("delta-between", "db-full-1", 5, "v1")
	delta("delta-between", "db-delta")
	full("delta-between", "db-full-2", 4, "v2")

	for _, row := range rows {
		for _, kind := range []string{"file", "content_entity"} {
			key := kind + ":" + row.file
			if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
   observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $5, 'git', $5, now(), now(), $6, jsonb_build_object('path', $7::text, 'version', $8::text))`,
				row.generation+"/"+key, row.scope, row.generation, kind, key, row.tombstone, row.file, row.version,
			); err != nil {
				t.Fatalf("seed fact %s/%s: %v", row.generation, key, err)
			}
		}
	}
}
