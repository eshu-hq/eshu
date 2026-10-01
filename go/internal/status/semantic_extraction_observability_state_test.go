// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestSemanticExtractionObservabilitySurvivesMissingCapabilityState(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(status.RawSnapshot{
		SemanticExtraction: status.SemanticExtractionStatus{
			Queue: status.SemanticExtractionQueueSnapshot{
				Total:   2,
				Pending: 2,
			},
			Budget: status.SemanticExtractionBudgetSnapshot{
				EstimatedInputTokens: 120,
			},
			Audit: status.SemanticExtractionAuditSnapshot{
				ActorClassCounts: []status.NamedCount{{Name: "hosted_worker", Count: 2}},
			},
		},
	}, status.DefaultOptions())

	if got, want := report.SemanticExtraction.State, status.SemanticExtractionUnavailable; got != want {
		t.Fatalf("SemanticExtraction.State = %q, want %q", got, want)
	}
	if report.SemanticExtraction.DeterministicPathsAffected {
		t.Fatal("DeterministicPathsAffected = true, want false")
	}
	if got, want := report.SemanticExtraction.Queue.Pending, 2; got != want {
		t.Fatalf("Queue.Pending = %d, want %d", got, want)
	}
	if got, want := report.SemanticExtraction.Budget.EstimatedInputTokens, int64(120); got != want {
		t.Fatalf("Budget.EstimatedInputTokens = %d, want %d", got, want)
	}
	if got, want := len(report.SemanticExtraction.Audit.ActorClassCounts), 1; got != want {
		t.Fatalf("Audit.ActorClassCounts len = %d, want %d", got, want)
	}
}

func TestLoadSemanticExtractionStatusPreservesProjectionAndWrappers(t *testing.T) {
	t.Parallel()
	asOf := time.Date(2026, 10, 1, 10, 0, 0, 0, time.FixedZone("EDT", -4*60*60))
	base := &fakeReader{snapshot: status.RawSnapshot{
		SemanticExtraction: status.SemanticExtractionStatus{
			Queue:  status.SemanticExtractionQueueSnapshot{Total: 3, Pending: 2},
			Budget: status.SemanticExtractionBudgetSnapshot{EstimatedInputTokens: 42},
			Audit:  status.SemanticExtractionAuditSnapshot{ActorClassCounts: []status.NamedCount{{Name: "worker", Count: 3}}},
		},
	}}
	profile := status.SemanticProviderProfileStatus{ProfileID: "configured", State: status.SemanticProviderProfileConfigured}
	wrapped := status.WithRetryPolicies(status.WithSemanticProviderProfiles(base, profile))
	got, err := status.LoadSemanticExtractionStatus(t.Context(), wrapped, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if base.selection != status.SemanticOnlySnapshotSelection() || !base.asOf.Equal(asOf.UTC()) {
		t.Fatalf("selection=%+v asOf=%v; want semantic-only and UTC", base.selection, base.asOf)
	}
	wantRaw := base.snapshot
	wantRaw.SemanticExtraction.ProviderProfiles = []status.SemanticProviderProfileStatus{profile}
	want := status.BuildReport(wantRaw, status.DefaultOptions()).SemanticExtraction
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("semantic status=%+v, want %+v", got, want)
	}
}

func TestLoadSemanticExtractionStatusFailsClosed(t *testing.T) {
	t.Parallel()
	if _, err := status.LoadSemanticExtractionStatus(t.Context(), nil, time.Now()); err == nil {
		t.Fatal("nil reader accepted")
	}
	want := errors.New("semantic read failed")
	got, err := status.LoadSemanticExtractionStatus(t.Context(), &fakeReader{err: want}, time.Now())
	if !errors.Is(err, want) || !reflect.DeepEqual(got, status.SemanticExtractionStatus{}) {
		t.Fatalf("status=%+v err=%v, want zero and wrapped failure", got, err)
	}
}
