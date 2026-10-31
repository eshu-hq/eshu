// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// benchFleetQueryer is self-contained (no shared test fakes) so this
// benchmark compiles on the pre-fix tree for before/after comparison. It
// serves a 50-row SLSA page with ~1KB payloads per row.
type benchFleetQueryer struct {
	mu    sync.Mutex
	calls int
	page  [][]any
}

func (q *benchFleetQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("benchFleetQueryer: unexpected ExecContext call")
}

func (q *benchFleetQueryer) queryCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.calls
}

func (q *benchFleetQueryer) QueryContext(ctx context.Context, _ string, _ ...any) (db.Rows, error) {
	q.mu.Lock()
	q.calls++
	first := q.calls == 1
	q.mu.Unlock()
	if first {
		// Dwell the first pass so the 16 racing goroutines pile onto one
		// flight post-fix; pre-fix each caller loads regardless.
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &benchFleetRows{rows: q.page}, nil
}

type benchFleetRows struct {
	rows  [][]any
	index int
}

func (r *benchFleetRows) Next() bool { return r.index < len(r.rows) }

func (r *benchFleetRows) Close() error { return nil }

func (r *benchFleetRows) Err() error { return nil }

func (r *benchFleetRows) Scan(dest ...any) error {
	if r.index >= len(r.rows) {
		return fmt.Errorf("scan called without row")
	}
	row := r.rows[r.index]
	if len(dest) != len(row) {
		return fmt.Errorf("scan destination count = %d, want %d", len(dest), len(row))
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			value, ok := row[i].(string)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want string", i, row[i])
			}
			*target = value
		case *int64:
			value, ok := row[i].(int64)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want int64", i, row[i])
			}
			*target = value
		case *time.Time:
			value, ok := row[i].(time.Time)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want time.Time", i, row[i])
			}
			*target = value
		case *bool:
			value, ok := row[i].(bool)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want bool", i, row[i])
			}
			*target = value
		case *[]byte:
			value, ok := row[i].([]byte)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want []byte", i, row[i])
			}
			*target = value
		default:
			return fmt.Errorf("unsupported scan target %T", dest[i])
		}
	}
	r.index++
	return nil
}

func benchSLSAPage(rows int) [][]any {
	payload := []byte(`{"statement_id":"stmt-1","subject_digest":"sha256:` + strings.Repeat("a", 64) +
		`","predicate":"` + strings.Repeat("p", 800) + `"}`)
	page := make([][]any, 0, rows)
	for i := 0; i < rows; i++ {
		page = append(page, []any{
			fmt.Sprintf("fact-slsa-%d", i), "scope-sbom", "gen-1",
			"attestation.slsa_provenance", "stable-key-1", "1.0.0",
			"sbom_attestation", int64(0), "reported", "sbom_attestation",
			"source-key-1", "uri://example", "rec-1",
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			false,
			payload,
		})
	}
	return page
}

// BenchmarkSharedFleetSLSALoads measures 16 concurrent workers racing one
// fleet-wide SLSA load (#9697): pre-fix each caller pays a full 50-row
// decoded pass; post-fix they share the leader's single pass.
func BenchmarkSharedFleetSLSALoads(b *testing.B) {
	q := &benchFleetQueryer{page: benchSLSAPage(50)}
	store := NewFactStore(q)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(16)
		for j := 0; j < 16; j++ {
			go func() {
				defer wg.Done()
				_, _ = store.ListActiveContainerImageSLSAFacts(context.Background())
			}()
		}
		wg.Wait()
	}
	b.ReportMetric(float64(q.queryCount())/float64(b.N), "db-passes/op")
}
