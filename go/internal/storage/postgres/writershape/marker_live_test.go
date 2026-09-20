// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writershape

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestGraphWriterShapeClaimConcurrencyLive proves the exactly-once claim
// against real Postgres: eight concurrent starters converge on one winner
// (issue #6868). It also proves the full marker lifecycle: absent reads 0,
// the winner's mark advances the version, and a newer claim loses after
// the mark.
//
//	ESHU_GRAPH_WRITER_SHAPE_LIVE=1 ESHU_POSTGRES_DSN=... \
//	  go test ./internal/storage/postgres/writershape -run TestGraphWriterShapeClaimConcurrencyLive -count=1 -v
func TestGraphWriterShapeClaimConcurrencyLive(t *testing.T) {
	if os.Getenv("ESHU_GRAPH_WRITER_SHAPE_LIVE") != "1" {
		t.Skip("set ESHU_GRAPH_WRITER_SHAPE_LIVE=1 to run the writer-shape claim proof")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("ESHU_POSTGRES_DSN is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = database.Close() }()
	if err := database.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}

	store := NewStore(postgres.SQLDB{DB: database})
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	key := fmt.Sprintf("test-shape-%d", time.Now().UnixNano())
	defer func() {
		_, _ = database.ExecContext(context.Background(), `DELETE FROM graph_writer_shape WHERE shape_key = $1`, key)
	}()

	if version, err := store.AppliedVersion(ctx, key); err != nil || version != 0 {
		t.Fatalf("applied = %d, %v; want 0, nil (absent marker)", version, err)
	}

	const starters = 8
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < starters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			won, err := store.ClaimVersion(ctx, key, 1, time.Minute)
			if err != nil {
				t.Errorf("claim error = %v, want nil", err)
				return
			}
			if won {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := winners.Load(); got != 1 {
		t.Fatalf("winners = %d, want exactly 1", got)
	}

	if err := store.MarkAppliedVersion(ctx, key, 1); err != nil {
		t.Fatalf("mark applied: %v", err)
	}
	if version, err := store.AppliedVersion(ctx, key); err != nil || version != 1 {
		t.Fatalf("applied = %d, %v; want 1, nil", version, err)
	}
	if won, err := store.ClaimVersion(ctx, key, 1, time.Minute); err != nil || won {
		t.Fatalf("re-claim = %v, %v; want false, nil (version applied)", won, err)
	}
	if won, err := store.ClaimVersion(ctx, key, 2, time.Minute); err != nil || !won {
		t.Fatalf("newer claim = %v, %v; want true, nil", won, err)
	}
}
