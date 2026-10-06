// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestWithoutTerraformStateEvidenceSetsOnlyTheSkipFlag(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   status.SnapshotSelection
		want status.SnapshotSelection
	}{
		{"full", status.FullSnapshotSelection(), status.SnapshotSelection{IncludeCollectorFactEvidence: true, IncludeRegistryCollectors: true, SkipTerraformStateEvidence: true}},
		{"zero", status.SnapshotSelection{}, status.SnapshotSelection{SkipTerraformStateEvidence: true}},
	} {
		in := tc.in
		if got := in.WithoutTerraformStateEvidence(); got != tc.want {
			t.Fatalf("%s: WithoutTerraformStateEvidence() = %+v, want %+v", tc.name, got, tc.want)
		}
		if in != tc.in {
			t.Fatalf("%s: receiver mutated to %+v", tc.name, in)
		}
	}
}

func TestLoadReportWithSelectionPassesSelectionAndWrapsErrors(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 10, 6, 12, 0, 0, 0, time.FixedZone("EDT", -4*60*60))
	selection := status.FullSnapshotSelection().WithoutTerraformStateEvidence()
	reader := &fakeReader{snapshot: status.RawSnapshot{
		AsOf:  asOf.UTC(),
		Queue: status.QueueSnapshot{Total: 3, Succeeded: 3},
	}}
	report, err := status.LoadReportWithSelection(context.Background(), reader, asOf, status.DefaultOptions(), selection)
	if err != nil {
		t.Fatalf("LoadReportWithSelection() error = %v", err)
	}
	if reader.selection != selection || !reader.asOf.Equal(asOf) || reader.asOf.Location() != time.UTC {
		t.Fatalf("reader got selection %+v at %v, want %+v at UTC %v", reader.selection, reader.asOf, selection, asOf.UTC())
	}
	if report.Queue.Total != 3 {
		t.Fatalf("report queue total = %d, want 3", report.Queue.Total)
	}

	wantErr := errors.New("boom")
	if _, err := status.LoadReportWithSelection(context.Background(), &fakeReader{err: wantErr}, asOf, status.DefaultOptions(), selection); !errors.Is(err, wantErr) {
		t.Fatalf("LoadReportWithSelection() error = %v, want wrapped %v", err, wantErr)
	}
	if _, err := status.LoadReportWithSelection(context.Background(), nil, asOf, status.DefaultOptions(), selection); err == nil {
		t.Fatal("LoadReportWithSelection(nil reader) error = nil, want non-nil")
	}
}
