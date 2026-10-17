// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

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
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
	"github.com/eshu-hq/eshu/go/internal/status/queue"
	"github.com/eshu-hq/eshu/go/internal/status/semantic"
	"github.com/eshu-hq/eshu/go/internal/status/tfstate"
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
	raw.TerraformStateLastSerials = []tfstate.LocatorSerial{{SafeLocatorHash: "safe-hash", Serial: 42, ObservedAt: fixed}}
	raw.TerraformStateRecentWarnings = []tfstate.LocatorWarning{{SafeLocatorHash: "safe-hash", WarningKind: "state_missing", ObservedAt: fixed}}
	raw.CollectorFactEvidence = []collector.FactEvidence{{
		InstanceID: "git-1", CollectorKind: "git", EvidenceSource: "file", ObservationCount: 8, LastObservedAt: fixed,
	}}
	raw.RegistryCollectors = []statuspkg.RegistryCollectorSnapshot{{CollectorKind: "package_registry", ConfiguredInstances: 1}}
	reader := &bundleSelectionReader{raw: raw, terraformErr: errors.New("terraform evidence unavailable")}

	bundleMux := http.NewServeMux()
	(&Handler{StatusReader: reader, Neo4j: evidenceBundleFixtureGraph{count: 5}}).Mount(bundleMux)
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
	(&Handler{StatusReader: baselineReader, Neo4j: evidenceBundleFixtureGraph{count: 5}}).Mount(baselineMux)
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
}

func TestEvidenceBundleSelectedReadPreservesFailures(t *testing.T) {
	for _, tc := range []struct {
		name       string
		reader     *bundleSelectionReader
		cancel     bool
		expired    bool
		wantStatus int
	}{
		{name: "retained read", reader: &bundleSelectionReader{retainedErr: errors.New("queue read unavailable")}, wantStatus: http.StatusInternalServerError},
		// A canceled request context is the client walking away: 499, not a
		// server fault (#7674).
		{name: "cancelled context", reader: &bundleSelectionReader{}, cancel: true, wantStatus: querycontract.StatusClientClosedRequest},
		{name: "expired deadline", reader: &bundleSelectionReader{}, expired: true, wantStatus: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			(&Handler{StatusReader: tc.reader}).Mount(mux)
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
			if rec.Code != tc.wantStatus || tc.reader.calls != 1 || tc.reader.selection != want {
				t.Fatalf("status/selection/calls = %d/%+v/%d, want %d/%+v/1", rec.Code, tc.reader.selection, tc.reader.calls, tc.wantStatus, want)
			}
		})
	}
}

func TestEvidenceBundleSelectedReadStillRejectsInvalidBundle(t *testing.T) {
	raw := evidenceBundleFixtureSnapshot()
	raw.SemanticExtraction.Reason = "dial tcp 10.0.5.3:5432"
	reader := &bundleSelectionReader{raw: raw}
	mux := http.NewServeMux()
	(&Handler{StatusReader: reader, Neo4j: evidenceBundleFixtureGraph{count: 5}}).Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0/evidence/bundle", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want validation 500: %s", rec.Code, rec.Body.String())
	}
}

// evidenceBundleFixtureSnapshot is the shared RawSnapshot fixture for the
// live-bundle handler tests: one stage, one domain backlog with a matching
// queue blockage, one coordinator-registered collector, and a
// not-configured semantic provider. It mirrors the root fixture of the same
// name in evidence_bundle_live_test.go: the staying root bundle tests and
// the moved leaf tests cannot share an unexported helper across the package
// boundary, so each side keeps its own copy.
func evidenceBundleFixtureSnapshot() statuspkg.RawSnapshot {
	fixed := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	return statuspkg.RawSnapshot{
		AsOf: fixed,
		ScopeActivity: statuspkg.ScopeActivitySnapshot{
			Active: 5, Changed: 1, Unchanged: 4,
		},
		GenerationHistory: statuspkg.GenerationHistorySnapshot{
			Active: 1, Completed: 9, Superseded: 5, Other: 2,
		},
		StageCounts: []statuspkg.StageStatusCount{
			{Stage: "parse", Status: "pending", Count: 2},
			{Stage: "parse", Status: "claimed", Count: 6},
			{Stage: "parse", Status: "running", Count: 3},
			{Stage: "parse", Status: "succeeded", Count: 11},
		},
		DomainBacklogs: []statuspkg.DomainBacklog{
			{
				Domain:      "aws_relationship_materialization",
				Outstanding: 5,
				InFlight:    4,
				DeadLetter:  1,
				OldestAge:   41500 * time.Millisecond,
			},
		},
		QueueBlockages: []queue.Blockage{
			{Stage: "reduce", Domain: "aws_relationship_materialization", ConflictDomain: "aws", ConflictKey: "k1", Blocked: 3},
			{Stage: "reduce", Domain: "aws_relationship_materialization", ConflictDomain: "aws", ConflictKey: "k2", Blocked: 2},
		},
		Queue: statuspkg.QueueSnapshot{
			Total: 18, Outstanding: 7, Pending: 4, InFlight: 2, Retrying: 1,
			Succeeded: 10, DeadLetter: 1, OverdueClaims: 3,
			OldestOutstandingAge: 12500 * time.Millisecond,
		},
		Coordinator: &statuspkg.CoordinatorSnapshot{
			CollectorInstances: []collector.InstanceSummary{
				{
					InstanceID: "git-1", CollectorKind: "git", Mode: "direct",
					Enabled: true, ClaimsEnabled: true, DisplayName: "git",
					LastObservedAt: fixed, UpdatedAt: fixed,
				},
			},
		},
		SemanticExtraction: semantic.ExtractionStatus{
			State: "unavailable", Reason: "provider_not_configured", ProviderConfigured: false,
		},
	}
}

// evidenceBundleFixtureGraph is a GraphQuery stub returning a fixed
// repository count for MATCH (r:Repository) RETURN count(r), matching what
// getIndexStatus's identical query would report against the same graph.
type evidenceBundleFixtureGraph struct {
	count int
	err   error
}

func (g evidenceBundleFixtureGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, errors.New("Run not implemented by evidenceBundleFixtureGraph")
}

func (g evidenceBundleFixtureGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	if g.err != nil {
		return nil, g.err
	}
	return map[string]any{"count": g.count}, nil
}
