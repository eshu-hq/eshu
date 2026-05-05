package reducer

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestCloudAssetResolutionHandlerBuildsCanonicalWriteRequest(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudAssetResolutionWriter{
		result: CloudAssetResolutionWriteResult{
			CanonicalID:      "canonical:cloud_asset:aws:s3:bucket:logs-prod",
			CanonicalWrites:  1,
			ReconciledScopes: 2,
			EvidenceSummary:  "canonical cloud asset written",
		},
	}
	handler := CloudAssetResolutionHandler{Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		SourceSystem: "git",
		Domain:       DomainCloudAssetResolution,
		Cause:        "shared cloud asset follow-up required",
		EntityKeys: []string{
			"aws:s3:bucket:logs-prod",
			"tfstate:module.logs.aws_s3_bucket.logs_prod",
			"aws:s3:bucket:logs-prod",
		},
		RelatedScopeIDs: []string{
			"scope-999",
			"scope-123",
			"scope-999",
		},
		EnqueuedAt:  time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		AvailableAt: time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		Status:      IntentStatusClaimed,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	if got, want := len(writer.requests), 1; got != want {
		t.Fatalf("writer request count = %d, want %d", got, want)
	}

	request := writer.requests[0]
	wantEntityKeys := []string{
		"aws:s3:bucket:logs-prod",
		"tfstate:module.logs.aws_s3_bucket.logs_prod",
	}
	if !slices.Equal(request.EntityKeys, wantEntityKeys) {
		t.Fatalf("request.EntityKeys = %v, want %v", request.EntityKeys, wantEntityKeys)
	}

	wantRelatedScopes := []string{
		"scope-123",
		"scope-999",
	}
	if !slices.Equal(request.RelatedScopeIDs, wantRelatedScopes) {
		t.Fatalf("request.RelatedScopeIDs = %v, want %v", request.RelatedScopeIDs, wantRelatedScopes)
	}

	if got, want := result.Domain, DomainCloudAssetResolution; got != want {
		t.Fatalf("result.Domain = %q, want %q", got, want)
	}
	if got, want := result.Status, ResultStatusSucceeded; got != want {
		t.Fatalf("result.Status = %q, want %q", got, want)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("result.CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := result.EvidenceSummary, "canonical cloud asset written"; got != want {
		t.Fatalf("result.EvidenceSummary = %q, want %q", got, want)
	}
}

func TestCloudAssetResolutionHandlerRejectsMissingEntityKeys(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudAssetResolutionWriter{}
	handler := CloudAssetResolutionHandler{Writer: writer}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-2",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          DomainCloudAssetResolution,
		Cause:           "shared cloud asset follow-up required",
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want non-nil")
	}
	if got, want := len(writer.requests), 0; got != want {
		t.Fatalf("writer request count = %d, want %d", got, want)
	}
}

func TestCloudAssetResolutionHandlerPublishesCloudCanonicalPhase(t *testing.T) {
	t.Parallel()

	publisher := &recordingGraphProjectionPhasePublisher{}
	handler := CloudAssetResolutionHandler{
		Writer: &recordingCloudAssetResolutionWriter{
			result: CloudAssetResolutionWriteResult{CanonicalWrites: 1},
		},
		PhasePublisher: publisher,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-cloud-phase",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          DomainCloudAssetResolution,
		Cause:           "shared cloud asset follow-up required",
		EntityKeys:      []string{"aws:s3:bucket:logs-prod"},
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := len(publisher.calls), 1; got != want {
		t.Fatalf("publisher call count = %d, want %d", got, want)
	}
	if got, want := publisher.calls[0][0].Key.Keyspace, GraphProjectionKeyspaceCloudResourceUID; got != want {
		t.Fatalf("published keyspace = %q, want %q", got, want)
	}
	if got, want := publisher.calls[0][0].Phase, GraphProjectionPhaseCanonicalNodesCommitted; got != want {
		t.Fatalf("published phase = %q, want %q", got, want)
	}
}

func TestCloudAssetResolutionHandlerRequiresCanonicalWriter(t *testing.T) {
	t.Parallel()

	handler := CloudAssetResolutionHandler{}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-5",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          DomainCloudAssetResolution,
		Cause:           "shared cloud asset follow-up required",
		EntityKeys:      []string{"aws:s3:bucket:logs-prod"},
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want non-nil")
	}
	if got, want := err.Error(), "cloud asset resolution writer is required"; got != want {
		t.Fatalf("Handle() error = %q, want %q", got, want)
	}
}

type recordingCloudAssetResolutionWriter struct {
	requests []CloudAssetResolutionWrite
	result   CloudAssetResolutionWriteResult
	err      error
}

func (w *recordingCloudAssetResolutionWriter) WriteCloudAssetResolution(
	_ context.Context,
	request CloudAssetResolutionWrite,
) (CloudAssetResolutionWriteResult, error) {
	w.requests = append(w.requests, request)
	return w.result, w.err
}
