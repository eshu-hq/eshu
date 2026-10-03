// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/evidencebundle"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

type bundleSelectionReader struct {
	raw          statuspkg.RawSnapshot
	terraformErr error
	retainedErr  error
	selection    statuspkg.SnapshotSelection
	calls        int
}

func (r *bundleSelectionReader) ReadStatusSnapshot(ctx context.Context, asOf time.Time) (statuspkg.RawSnapshot, error) {
	return r.ReadStatusSnapshotFiltered(ctx, asOf, statuspkg.FullSnapshotSelection())
}

func (r *bundleSelectionReader) ReadStatusSnapshotFiltered(
	ctx context.Context,
	_ time.Time,
	selection statuspkg.SnapshotSelection,
) (statuspkg.RawSnapshot, error) {
	r.selection = selection
	r.calls++
	if err := ctx.Err(); err != nil {
		return statuspkg.RawSnapshot{}, err
	}
	if r.retainedErr != nil {
		return statuspkg.RawSnapshot{}, r.retainedErr
	}
	if !selection.SkipTerraformStateEvidence && r.terraformErr != nil {
		return statuspkg.RawSnapshot{}, r.terraformErr
	}
	raw := r.raw
	if selection.SkipTerraformStateEvidence {
		raw.TerraformStateLastSerials = nil
		raw.TerraformStateRecentWarnings = nil
	}
	return raw, nil
}

func TestEvidenceBundleSelectsAllExceptTerraformState(t *testing.T) {
	fixed := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	raw := evidenceBundleFixtureSnapshot()
	raw.TerraformStateLastSerials = []statuspkg.TerraformStateLocatorSerial{{SafeLocatorHash: "safe-hash", Serial: 42, ObservedAt: fixed}}
	raw.TerraformStateRecentWarnings = []statuspkg.TerraformStateLocatorWarning{{SafeLocatorHash: "safe-hash", WarningKind: "state_missing", ObservedAt: fixed}}
	raw.CollectorFactEvidence = []statuspkg.CollectorFactEvidence{{
		InstanceID: "git-1", CollectorKind: "git", EvidenceSource: "file", ObservationCount: 8, LastObservedAt: fixed,
	}}
	raw.RegistryCollectors = []statuspkg.RegistryCollectorSnapshot{{CollectorKind: "package_registry", ConfiguredInstances: 1}}
	reader := &bundleSelectionReader{raw: raw, terraformErr: errors.New("terraform evidence unavailable")}

	bundleMux := http.NewServeMux()
	(&EvidenceHandler{StatusReader: reader, Neo4j: evidenceBundleFixtureGraph{count: 5}}).Mount(bundleMux)
	bundleRec := httptest.NewRecorder()
	bundleMux.ServeHTTP(bundleRec, httptest.NewRequest(http.MethodGet, "/api/v0/evidence/bundle", nil))
	if bundleRec.Code != http.StatusOK {
		t.Fatalf("bundle status = %d, want 200 despite unused Terraform read error: %s", bundleRec.Code, bundleRec.Body.String())
	}
	wantSelection := statuspkg.FullSnapshotSelection()
	wantSelection.SkipTerraformStateEvidence = true
	if reader.calls != 1 || reader.selection != wantSelection {
		t.Fatalf("bundle selection/calls = %+v/%d, want %+v/1", reader.selection, reader.calls, wantSelection)
	}
	var bundle evidencebundle.Bundle
	if err := json.Unmarshal(bundleRec.Body.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	if err := evidencebundle.Validate(bundle); err != nil {
		t.Fatalf("bundle validation = %v", err)
	}
	if bundle.Validation.Status != "passed" || bundle.Contents.PipelineState == nil || bundle.Contents.SemanticProviderState == nil {
		t.Fatalf("bundle lost validation or required sections: %+v", bundle)
	}
	pipeline := bundle.Contents.PipelineState
	if pipeline.RepositoryCount != 5 || pipeline.Queue.Total != 18 || pipeline.QueueBlockedCount != 5 ||
		pipeline.HealthState != "stalled" || len(pipeline.StageSummaries) != 1 || len(pipeline.DomainBacklogs) != 1 ||
		len(pipeline.Collectors) != 1 || pipeline.Collectors[0].Health != "observed" {
		t.Fatalf("bundle lost retained status/collector evidence: %+v", pipeline)
	}
	if bundle.Contents.SemanticProviderState.State != "unavailable" || len(bundle.Redaction.Rules) == 0 || bundle.Bounds.MaxHandles == 0 {
		t.Fatalf("bundle lost semantic/redaction/bounds: %+v", bundle)
	}

	// The same selected route with a healthy Terraform source is the complete
	// baseline. Only the wall-clock stamp and its derived bundle ID may vary.
	baselineReader := &bundleSelectionReader{raw: raw}
	baselineMux := http.NewServeMux()
	(&EvidenceHandler{StatusReader: baselineReader, Neo4j: evidenceBundleFixtureGraph{count: 5}}).Mount(baselineMux)
	baselineRec := httptest.NewRecorder()
	baselineMux.ServeHTTP(baselineRec, httptest.NewRequest(http.MethodGet, "/api/v0/evidence/bundle", nil))
	if baselineRec.Code != http.StatusOK || baselineReader.calls != 1 || baselineReader.selection != wantSelection {
		t.Fatalf("healthy baseline status/selection/calls = %d/%+v/%d, want 200/%+v/1",
			baselineRec.Code, baselineReader.selection, baselineReader.calls, wantSelection)
	}
	var baseline evidencebundle.Bundle
	if err := json.Unmarshal(baselineRec.Body.Bytes(), &baseline); err != nil {
		t.Fatal(err)
	}
	if err := evidencebundle.Validate(baseline); err != nil || baseline.Validation.Status != "passed" {
		t.Fatalf("healthy baseline validation = %v/%s", err, baseline.Validation.Status)
	}
	bundle.Identity.CreatedAt = fixed.Format(time.RFC3339)
	baseline.Identity.CreatedAt = fixed.Format(time.RFC3339)
	bundle.BundleID = ""
	baseline.BundleID = ""
	if !reflect.DeepEqual(bundle, baseline) {
		t.Fatalf("Terraform-only failure changed the complete typed bundle beyond CreatedAt/BundleID")
	}

	// The full status surface still needs Terraform evidence and propagates
	// that read error. The bundle has no fallback after a failed retained read.
	fullReader := &bundleSelectionReader{raw: raw, terraformErr: reader.terraformErr}
	statusMux := http.NewServeMux()
	(&StatusHandler{StatusReader: fullReader}).Mount(statusMux)
	fullRec := httptest.NewRecorder()
	statusMux.ServeHTTP(fullRec, httptest.NewRequest(http.MethodGet, "/api/v0/status/pipeline", nil))
	if fullRec.Code != http.StatusInternalServerError || fullReader.calls != 1 || fullReader.selection != statuspkg.FullSnapshotSelection() {
		t.Fatalf("full status failure/selection = %d/%+v/%d, want 500/full/1", fullRec.Code, fullReader.selection, fullReader.calls)
	}
}

func TestEvidenceBundleSelectedReadPreservesFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		reader  *bundleSelectionReader
		cancel  bool
		expired bool
	}{
		{name: "retained read", reader: &bundleSelectionReader{retainedErr: errors.New("queue read unavailable")}},
		{name: "cancelled context", reader: &bundleSelectionReader{}, cancel: true},
		{name: "expired deadline", reader: &bundleSelectionReader{}, expired: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			(&EvidenceHandler{StatusReader: tc.reader}).Mount(mux)
			req := httptest.NewRequest(http.MethodGet, "/api/v0/evidence/bundle", nil)
			if tc.cancel {
				ctx, cancel := context.WithCancel(req.Context())
				cancel()
				req = req.WithContext(ctx)
			}
			if tc.expired {
				ctx, cancel := context.WithDeadline(req.Context(), time.Now().Add(-time.Second))
				defer cancel()
				if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
					t.Fatal("expected deadline to be expired before request")
				}
				req = req.WithContext(ctx)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			want := statuspkg.FullSnapshotSelection()
			want.SkipTerraformStateEvidence = true
			if rec.Code != http.StatusInternalServerError || tc.reader.calls != 1 || tc.reader.selection != want {
				t.Fatalf("status/selection/calls = %d/%+v/%d, want 500/%+v/1", rec.Code, tc.reader.selection, tc.reader.calls, want)
			}
		})
	}
}

func TestEvidenceBundleSelectedReadStillRejectsInvalidBundle(t *testing.T) {
	raw := evidenceBundleFixtureSnapshot()
	raw.SemanticExtraction.Reason = "dial tcp 10.0.5.3:5432"
	reader := &bundleSelectionReader{raw: raw}
	mux := http.NewServeMux()
	(&EvidenceHandler{StatusReader: reader, Neo4j: evidenceBundleFixtureGraph{count: 5}}).Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0/evidence/bundle", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want validation 500: %s", rec.Code, rec.Body.String())
	}
}
