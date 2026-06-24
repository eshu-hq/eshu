// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestProducerActivityQueryUsesIndexedLatestActivityShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"WHERE status IN ('pending', 'active')",
		"ORDER BY GREATEST(",
		"observed_at,",
		"ingested_at,",
		"COALESCE(activated_at, observed_at)",
		") DESC",
		"LIMIT 1",
	} {
		if !strings.Contains(producerActivityQuery, want) {
			t.Fatalf("producerActivityQuery missing %q:\n%s", want, producerActivityQuery)
		}
	}
	if strings.Contains(producerActivityQuery, "SELECT MAX(") {
		t.Fatalf("producerActivityQuery must use indexed ORDER BY/LIMIT shape, got:\n%s", producerActivityQuery)
	}
}

func TestReadProducerActivitySnapshotHandlesNullAgeExplicitly(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{rows: [][]any{{true, nil}}},
		},
	}
	got, err := readProducerActivitySnapshot(
		context.Background(),
		queryer,
		time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("readProducerActivitySnapshot() error = %v, want nil", err)
	}
	if !got.HasActiveOrPendingGeneration {
		t.Fatal("readProducerActivitySnapshot().HasActiveOrPendingGeneration = false, want true")
	}
	if got.LatestGenerationAge != 0 {
		t.Fatalf("readProducerActivitySnapshot().LatestGenerationAge = %v, want 0", got.LatestGenerationAge)
	}
}
