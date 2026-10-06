// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// terraformSelectionReader records the selection a runtime surface requests
// and, when omitOnSkip is set, drops Terraform evidence as the Postgres status
// store does for a selection that skips it.
type terraformSelectionReader struct {
	snapshot      statuspkg.RawSnapshot
	omitOnSkip    bool
	selections    []statuspkg.SnapshotSelection
	unfilteredHit int
}

func (r *terraformSelectionReader) ReadStatusSnapshot(ctx context.Context, asOf time.Time) (statuspkg.RawSnapshot, error) {
	r.unfilteredHit++
	return r.ReadStatusSnapshotFiltered(ctx, asOf, statuspkg.FullSnapshotSelection())
}

func (r *terraformSelectionReader) ReadStatusSnapshotFiltered(
	_ context.Context,
	_ time.Time,
	selection statuspkg.SnapshotSelection,
) (statuspkg.RawSnapshot, error) {
	r.selections = append(r.selections, selection)
	raw := r.snapshot
	if r.omitOnSkip && selection.SkipTerraformStateEvidence {
		raw.TerraformStateLastSerials = nil
		raw.TerraformStateRecentWarnings = nil
	}
	return raw, nil
}

func terraformMetricsSnapshot() statuspkg.RawSnapshot {
	asOf := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	return statuspkg.RawSnapshot{
		AsOf:          asOf,
		ScopeActivity: statuspkg.ScopeActivitySnapshot{Active: 3, Changed: 1, Unchanged: 2},
		Queue:         statuspkg.QueueSnapshot{Total: 4, Outstanding: 1, Pending: 1, Succeeded: 3},
		TerraformStateLastSerials: []statuspkg.TerraformStateLocatorSerial{
			{SafeLocatorHash: "hash-a", BackendKind: "s3", Serial: 7, ObservedAt: asOf},
		},
		TerraformStateRecentWarnings: []statuspkg.TerraformStateLocatorWarning{
			{SafeLocatorHash: "hash-a", BackendKind: "s3", WarningKind: "state_missing", ObservedAt: asOf},
		},
	}
}

// TestStatusMetricsSkipsTerraformEvidence proves runtime /metrics, which
// renders no Terraform-state gauge, requests a selection without the
// Terraform-state reads and renders byte-identical output either way (#7009).
func TestStatusMetricsSkipsTerraformEvidence(t *testing.T) {
	t.Parallel()
	scrape := func(reader *terraformSelectionReader) string {
		t.Helper()
		handler, err := NewStatusMetricsHandler("collector-git", reader)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	baseline := &terraformSelectionReader{snapshot: terraformMetricsSnapshot()}
	selected := &terraformSelectionReader{snapshot: terraformMetricsSnapshot(), omitOnSkip: true}
	want := scrape(baseline)
	got := scrape(selected)
	wantSelection := statuspkg.FullSnapshotSelection().WithoutTerraformStateEvidence()
	if len(selected.selections) != 1 || selected.selections[0] != wantSelection || selected.unfilteredHit != 0 {
		t.Fatalf("selections = %+v (unfiltered %d), want [%+v]", selected.selections, selected.unfilteredHit, wantSelection)
	}
	if got != want {
		t.Fatalf("metrics changed when Terraform evidence was omitted:\nfull=%s\nomitted=%s", want, got)
	}
}
