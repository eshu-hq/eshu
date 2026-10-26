// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/secret/lines"
)

// secretLinesPolicyUpsert rewrites keys 1..$1 of repo-p; one in five carries a
// stored marker, which migration 138 derives nothing for and 235 classifies.
const secretLinesPolicyUpsert = `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
SELECT 'repo-p', 'p_' || lpad(i::text, 4, '0') || '.tf',
       CASE (i + $2::int) % 5
         WHEN 0 THEN 'token = "writer' || $2::int || 'value' || i || '"'
         WHEN 1 THEN 'stored = REDACTED_aws_access_key' || E'\n' || 'n = ' || $2::int
         WHEN 2 THEN 'package clean' || $2::int
         WHEN 3 THEN 'npmAuthIdent: "ident' || $2::int || 'x' || i || '"'
         ELSE 'url = "https://u:pw' || $2::int || '@h.example.com/"'
       END,
       md5(i::text || $2::text), 2, 'hcl', now()
FROM generate_series(1, $1::int) AS i
ORDER BY 2
ON CONFLICT (repo_id, relative_path) DO UPDATE
SET content = EXCLUDED.content, content_hash = EXCLUDED.content_hash, indexed_at = EXCLUDED.indexed_at`

type readinessSample struct {
	at    time.Time
	ready bool
}

func TestSecretLinesPolicyMigrationUnderWritersLive(t *testing.T) {
	ctx, db := openSecretLinesDatabase(t)
	db.SetMaxOpenConns(16)
	const keys = 400
	// A 138-era database: the 138 function, ready at epoch 3, rows derived by
	// 138 (the stored marker lines have none).
	if _, err := db.ExecContext(ctx, secretLinesMigrationSQLByName(t, secretLinesMigrationName)); err != nil {
		t.Fatalf("re-apply 138: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE content_file_secret_lines_state SET state = 'ready', epoch = 3, policy_version = 138 WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, secretLinesPolicyUpsert, keys, 0); err != nil {
		t.Fatalf("seed: %v", err)
	}
	requireSecretLinesParity(t, ctx, db, "138 seed")

	runCtx, cancel := context.WithCancel(ctx)
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		errs    []error
		upserts int
		samples []readinessSample
	)
	for writer := 1; writer <= 3; writer++ {
		wg.Add(1)
		go func(writer int) {
			defer wg.Done()
			for variant := writer; runCtx.Err() == nil; variant += 3 {
				_, err := db.ExecContext(runCtx, secretLinesPolicyUpsert, keys, variant)
				mu.Lock()
				if err != nil && runCtx.Err() == nil {
					errs = append(errs, err)
				} else if err == nil {
					upserts++
				}
				mu.Unlock()
			}
		}(writer)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for runCtx.Err() == nil {
			ready, err := lines.Ready(runCtx, db)
			if err == nil {
				mu.Lock()
				samples = append(samples, readinessSample{at: time.Now(), ready: ready})
				mu.Unlock()
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	time.Sleep(time.Second)
	applyStart := time.Now()
	if _, err := db.ExecContext(ctx, secretLinesMigrationSQLByName(t, "content_file_secret_lines_marker_policy")); err != nil {
		t.Fatalf("apply 235 under writers: %v", err)
	}
	committed := time.Now()
	time.Sleep(500 * time.Millisecond)
	repairStart := time.Now()
	outcome, err := lines.NewRepairer(SQLDB{DB: db}, db, lines.RepairOptions{Finalize: lines.Options{BatchSize: 50}}).Tick(ctx)
	if err != nil || outcome != lines.RepairPublished {
		t.Fatalf("repair under writers = %s, %v; want published", outcome, err)
	}
	time.Sleep(500 * time.Millisecond)
	cancel()
	wg.Wait()

	if len(errs) > 0 {
		t.Fatalf("%d writer statements failed; first: %v", len(errs), errors.Join(errs[:min(3, len(errs))]...))
	}
	if upserts < 6 {
		t.Fatalf("only %d upserts completed; the writers did not overlap the apply and the rebuild", upserts)
	}
	readyBefore, inWindow := 0, 0
	for _, sample := range samples {
		switch {
		case sample.at.Before(applyStart) && sample.ready:
			readyBefore++
		case sample.at.After(committed) && sample.at.Before(repairStart):
			inWindow++
			if sample.ready {
				t.Fatalf("readiness read ready at %v, between 235's commit and the repair: a read could use stale 138 rows", sample.at.Sub(committed))
			}
		}
	}
	if readyBefore == 0 || inWindow == 0 {
		t.Fatalf("readiness samples: %d ready before the apply, %d in the window; the poller did not cover both", readyBefore, inWindow)
	}
	requireSecretLinesParity(t, ctx, db, "after 235 and the repair under writers")
	var markerRows, markerFiles int
	if err := db.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM content_file_secret_lines WHERE repo_id = 'repo-p' AND finding_kind = 'aws_access_key' AND line_text LIKE '%REDACTED_aws_access_key%'),
       (SELECT count(*) FROM content_files WHERE repo_id = 'repo-p' AND content LIKE '%REDACTED_aws_access_key%')`).Scan(&markerRows, &markerFiles); err != nil {
		t.Fatal(err)
	}
	if markerFiles == 0 || markerRows != markerFiles {
		t.Fatalf("marker lines with their kind = %d, files holding a marker = %d; want equal and non-zero", markerRows, markerFiles)
	}
	t.Logf("upserts %d, readiness samples %d (%d in the window), marker files %d", upserts, len(samples), inWindow, markerFiles)
}
