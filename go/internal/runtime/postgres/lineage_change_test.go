// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// lineageReplacement returns a single-use container built from a copy of the
// owned primary's data volume that binds the primary's address. The test stops
// the primary, starts the replacement, and restores the primary on cleanup.
func lineageReplacement(t *testing.T, key string) (primary, replacement string) {
	t.Helper()
	primary = restartContainer(t)
	replacement = strings.TrimSpace(os.Getenv(key))
	if os.Getenv("ESHU_READER_TEST_LINEAGE_SWAP") != "1" || replacement == "" {
		t.Skip("lineage swap requires ESHU_READER_TEST_LINEAGE_SWAP=1 and " + key)
	}
	t.Cleanup(func() {
		docker(t, "stop", replacement)
		docker(t, "start", primary)
		if _, err := probeLineage(os.Getenv("ESHU_READER_TEST_WRITER_DSN")); err != nil {
			t.Errorf("owned primary did not return after the swap: %v", err)
		}
	})
	return primary, replacement
}

// probeLineage waits for a writable primary at dsn and reads its lineage on
// an independent connection.
func probeLineage(dsn string) (lineageObservation, error) {
	var observed lineageObservation
	err := eventually(60*time.Second, func() error {
		conn, err := pgx.Connect(context.Background(), dsn)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(context.Background()) }()
		var recovery bool
		if err := conn.QueryRow(context.Background(), "SELECT pg_is_in_recovery()").Scan(&recovery); err != nil || recovery {
			return fmt.Errorf("not a writable primary yet (recovery=%v): %w", recovery, err)
		}
		observed, err = readLineagePGX(context.Background(), conn)
		return err
	})
	return observed, err
}

// requireLatched proves the same Access refuses the replacement, stays
// refused on later dials, and fails readiness, and that the writer pool's
// dial failure still carries ErrWrongTopology through its bounded errors.
func requireLatched(t *testing.T, access *Access) {
	t.Helper()
	err := eventually(30*time.Second, func() error {
		_, _ = access.Writer().ExecContext(context.Background(), "SELECT 1")
		if !access.lineage.latched.Load() {
			return errors.New("access not latched")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("replacement primary was never refused: %v", err)
	}
	for range 3 {
		if _, err := access.ContextWithCheckpoint(context.Background()); !errors.Is(err, ErrWrongTopology) {
			t.Fatalf("latched checkpoint = %v, want ErrWrongTopology", err)
		}
		access.writer.SetMaxIdleConns(0)
		if err := access.Writer().PingContext(context.Background()); !errors.Is(err, ErrWrongTopology) {
			t.Fatalf("latched writer dial = %v, want ErrWrongTopology", err)
		}
		if err := access.Ping(context.Background()); !errors.Is(err, ErrWrongTopology) {
			t.Fatalf("latched readiness = %v, want ErrWrongTopology", err)
		}
	}
}

// TestAccessRejectsPromotedCopyOnNewTimeline swaps in a copy of the primary
// that ended recovery on a new timeline with the same system identifier. No
// write or checkpoint runs after Open, and the copy's flushed WAL is at or
// past the watermark, so only the timeline predicate can refuse it.
func TestAccessRejectsPromotedCopyOnNewTimeline(t *testing.T) {
	primary, promoted := lineageReplacement(t, "ESHU_READER_TEST_PROMOTED_CONTAINER")
	access := testAccess(t, "")
	base, watermark := access.lineage.identity(), access.lineage.watermark.Load()
	docker(t, "stop", primary)
	docker(t, "start", promoted)
	dsn := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	if _, err := probeLineage(dsn); err != nil {
		t.Fatal(err)
	}
	// Before the Access dials, advance the promoted copy's WAL past the
	// Access watermark on an independent connection: a promoted copy that
	// accepted writes past what this Access observed is refused only by the
	// timeline predicate.
	conn, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(context.Background(), "SELECT pg_switch_wal()")
	_ = conn.Close(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	observed, err := probeLineage(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if observed.timeline == base.timeline || observed.flush < watermark {
		t.Fatalf("promoted copy precondition: timeline %s (base %s), flush %s (watermark %s)", observed.timeline, base.timeline, formatLSN(observed.flush), formatLSN(watermark))
	}
	requireLatched(t, access)
}

// TestAccessRejectsRestoredSnapshotBelowWatermark swaps in a copy of the
// primary's volume taken before this Access raised its watermark: same system
// identifier and timeline, flushed WAL behind what the Access observed.
func TestAccessRejectsRestoredSnapshotBelowWatermark(t *testing.T) {
	primary, restored := lineageReplacement(t, "ESHU_READER_TEST_RESTORED_CONTAINER")
	access := testAccess(t, "")
	ctx := context.Background()
	if _, err := access.Writer().ExecContext(ctx, "CREATE TABLE IF NOT EXISTS eshu_lineage_restore_probe(id bigserial PRIMARY KEY, payload text NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := access.Writer().ExecContext(ctx, "INSERT INTO eshu_lineage_restore_probe(payload) SELECT repeat('r', 100) FROM generate_series(1, 1000)"); err != nil {
		t.Fatal(err)
	}
	if _, err := access.ContextWithCheckpoint(ctx); err != nil {
		t.Fatal(err)
	}
	base, watermark := access.lineage.identity(), access.lineage.watermark.Load()
	docker(t, "stop", primary)
	docker(t, "start", restored)
	observed, err := probeLineage(os.Getenv("ESHU_READER_TEST_WRITER_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	if observed.timeline != base.timeline || observed.flush >= watermark {
		t.Fatalf("restored copy precondition: timeline %s (base %s), flush %s (watermark %s)", observed.timeline, base.timeline, formatLSN(observed.flush), formatLSN(watermark))
	}
	requireLatched(t, access)
}
