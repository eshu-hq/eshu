// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

const (
	catalogBenchRows      = 28_000
	catalogBenchLargePool = 10 // payloads over 1 MB: 4% of rows after cycling
	catalogBenchSmallPool = 240
)

// catalogBenchPayload builds one repository fact payload of about size bytes:
// the six catalog keys plus an imports_map of many short entries, which is the
// shape that makes the real payloads expensive to decode.
func catalogBenchPayload(index, size int) []byte {
	var b strings.Builder
	fmt.Fprintf(&b,
		`{"repo_id":"repo-%d","graph_id":"graph-%d","name":"service-%d","repo_name":"org/service-%d",`+
			`"repo_slug":"org/service-%d","remote_url":"https://github.com/org/service-%d.git","imports_map":{`,
		index, index, index, index, index, index)
	for entry := 0; b.Len() < size; entry++ {
		if entry > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"symbol_%d":["pkg/path_%d/file_a.go","pkg/path_%d/file_b.go"]`, entry, entry, entry)
	}
	b.WriteString(`}}`)
	return []byte(b.String())
}

// catalogBenchProjection returns the object the SQL projection ships for
// payload: the declared keys only, as jsonb_build_object would render them.
func catalogBenchProjection(tb testing.TB, payload []byte) []byte {
	tb.Helper()
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		tb.Fatalf("decode bench payload: %v", err)
	}
	projected := make(map[string]json.RawMessage)
	for _, key := range relationships.RepositoryCatalogPayloadKeys() {
		value, ok := decoded[key]
		if !ok {
			value = json.RawMessage("null")
		}
		projected[key] = value
	}
	out, err := json.Marshal(projected)
	if err != nil {
		tb.Fatalf("encode bench projection: %v", err)
	}
	return out
}

// catalogBenchRowsFor cycles a small pool of payloads into catalogBenchRows
// rows so 28,000 rows of about 158 KB (4.4 GB on the QA corpus) fit in
// memory: about 4 percent of rows exceed 1 MB and the largest is about 5 MB.
func catalogBenchRowsFor(tb testing.TB) (full, projected [][]any, fullBytes, projectedBytes int) {
	tb.Helper()
	pool := make([][]byte, 0, catalogBenchLargePool+catalogBenchSmallPool)
	for i := 0; i < catalogBenchLargePool; i++ {
		size := 1_100_000 + i*(5_000_000-1_100_000)/(catalogBenchLargePool-1)
		pool = append(pool, catalogBenchPayload(len(pool), size))
	}
	for i := 0; i < catalogBenchSmallPool; i++ {
		// About 60 KB on average so the whole set averages about 158 KB.
		pool = append(pool, catalogBenchPayload(len(pool), 10_000+(i%24)*4_000+i))
	}
	projections := make([][]byte, len(pool))
	for i, payload := range pool {
		projections[i] = catalogBenchProjection(tb, payload)
	}
	at := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	for row := 0; row < catalogBenchRows; row++ {
		// 1 large payload per 25 rows, the rest small, interleaved.
		var index int
		if row%25 == 0 {
			index = (row / 25) % catalogBenchLargePool
		} else {
			index = catalogBenchLargePool + row%catalogBenchSmallPool
		}
		observed := at.Add(-time.Duration(row) * time.Second)
		full = append(full, []any{pool[index], observed})
		projected = append(projected, []any{projections[index], observed})
		fullBytes += len(pool[index])
		projectedBytes += len(projections[index])
	}
	return full, projected, fullBytes, projectedBytes
}

// BenchmarkLoadRepositoryCatalogDecode compares the Go-side cost of the
// pre-#8082 input (whole repository payloads) with the projected input the
// loader now receives, through the real LoadRepositoryCatalog loop. It does
// not include the database: the server-side projection cost is measured with
// EXPLAIN ANALYZE in the #8082 packet. Data generation is outside the timer.
func BenchmarkLoadRepositoryCatalogDecode(b *testing.B) {
	full, projected, fullBytes, projectedBytes := catalogBenchRowsFor(b)
	for _, tc := range []struct {
		name  string
		rows  [][]any
		bytes int
	}{
		{"whole_payload", full, fullBytes},
		{"projected", projected, projectedBytes},
	} {
		b.Run(tc.name, func(b *testing.B) {
			queryer := &catalogQueryerFake{rows: tc.rows}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				catalog, _, err := LoadRepositoryCatalog(context.Background(), queryer)
				if err != nil {
					b.Fatalf("LoadRepositoryCatalog() error = %v", err)
				}
				if len(catalog) == 0 {
					b.Fatal("catalog is empty")
				}
			}
			b.ReportMetric(float64(tc.bytes), "payload_bytes/op")
		})
	}
}
