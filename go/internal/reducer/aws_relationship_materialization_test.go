package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// recordingCloudResourceEdgeWriter captures edge writes and retracts so tests
// can assert the exact materialization request.
type recordingCloudResourceEdgeWriter struct {
	writeCalls      int
	writtenRows     []map[string]any
	writeEvidence   string
	retractCalls    int
	retractScopeIDs []string
	retractEvidence string
	writeErr        error
	retractErr      error
}

func (w *recordingCloudResourceEdgeWriter) WriteCloudResourceEdges(
	_ context.Context,
	rows []map[string]any,
	evidenceSource string,
) error {
	w.writeCalls++
	w.writtenRows = append(w.writtenRows, rows...)
	w.writeEvidence = evidenceSource
	return w.writeErr
}

func (w *recordingCloudResourceEdgeWriter) RetractCloudResourceEdges(
	_ context.Context,
	scopeIDs []string,
	_ string,
	evidenceSource string,
) error {
	w.retractCalls++
	w.retractScopeIDs = append(w.retractScopeIDs, scopeIDs...)
	w.retractEvidence = evidenceSource
	return w.retractErr
}

func readyLookup(ready, found bool) GraphProjectionReadinessLookup {
	return func(_ GraphProjectionPhaseKey, _ GraphProjectionPhase) (bool, bool) {
		return ready, found
	}
}

func awsRelationshipIntent() Intent {
	return Intent{
		IntentID:     "intent-edges-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainAWSRelationshipMaterialization,
		EntityKeys:   []string{"aws_resource_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

func TestAWSRelationshipMaterializationRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := AWSRelationshipMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		EdgeWriter:      &recordingCloudResourceEdgeWriter{},
		ReadinessLookup: readyLookup(true, true),
	}

	intent := awsRelationshipIntent()
	intent.Domain = DomainSQLRelationshipMaterialization
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestAWSRelationshipMaterializationRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := AWSRelationshipMaterializationHandler{
		EdgeWriter:      &recordingCloudResourceEdgeWriter{},
		ReadinessLookup: readyLookup(true, true),
	}
	if _, err := handler.Handle(context.Background(), awsRelationshipIntent()); err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestAWSRelationshipMaterializationRequiresEdgeWriter(t *testing.T) {
	t.Parallel()

	handler := AWSRelationshipMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		ReadinessLookup: readyLookup(true, true),
	}
	if _, err := handler.Handle(context.Background(), awsRelationshipIntent()); err == nil {
		t.Fatal("expected error when edge writer is nil")
	}
}

func TestAWSRelationshipMaterializationGatesOnCanonicalNodesPhase(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	handler := AWSRelationshipMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		EdgeWriter:      writer,
		ReadinessLookup: readyLookup(false, false), // PR-1 phase not yet committed
	}

	_, err := handler.Handle(context.Background(), awsRelationshipIntent())
	if err == nil {
		t.Fatal("expected a retryable error while canonical nodes phase is not ready")
	}
	if !IsRetryable(err) {
		t.Fatalf("error must be retryable so the intent re-enters the queue, got %v", err)
	}
	if writer.writeCalls != 0 || writer.retractCalls != 0 {
		t.Fatalf("no graph writes allowed before nodes commit: write=%d retract=%d", writer.writeCalls, writer.retractCalls)
	}
}

func TestAWSRelationshipMaterializationProjectsResolvedEdges(t *testing.T) {
	t.Parallel()

	source := resourceEnvelope("111122223333", "us-east-1", "aws_lambda_function",
		"arn:aws:lambda:us-east-1:111122223333:function:fn", "arn:aws:lambda:us-east-1:111122223333:function:fn")
	target := resourceEnvelope("111122223333", "us-east-1", "aws_kms_key",
		"arn:aws:kms:us-east-1:111122223333:key/abc", "arn:aws:kms:us-east-1:111122223333:key/abc")
	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_KMS_KEY",
		"source_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"source_arn":         "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"target_resource_id": "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_arn":         "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_type":        "aws_kms_key",
	})

	writer := &recordingCloudResourceEdgeWriter{}
	handler := AWSRelationshipMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{source, target, rel}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), awsRelationshipIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
	if len(writer.writtenRows) != 1 {
		t.Fatalf("written edge rows = %d, want 1", len(writer.writtenRows))
	}
	if writer.writeEvidence != awsRelationshipEvidenceSource {
		t.Fatalf("write evidence = %q, want %q", writer.writeEvidence, awsRelationshipEvidenceSource)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
}

func TestAWSRelationshipMaterializationUnresolvedTargetNotWritten(t *testing.T) {
	t.Parallel()

	source := resourceEnvelope("111122223333", "us-east-1", "aws_lambda_function",
		"arn:aws:lambda:us-east-1:111122223333:function:fn", "arn:aws:lambda:us-east-1:111122223333:function:fn")
	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_KMS_KEY",
		"source_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"source_arn":         "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"target_resource_id": "arn:aws:kms:us-east-1:111122223333:key/not-scanned",
		"target_arn":         "arn:aws:kms:us-east-1:111122223333:key/not-scanned",
		"target_type":        "aws_kms_key",
	})

	writer := &recordingCloudResourceEdgeWriter{}
	handler := AWSRelationshipMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{source, rel}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), awsRelationshipIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0 — unresolved target must not write", writer.writeCalls)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded (graceful degrade, not failure)", result.Status)
	}
}

func TestAWSRelationshipMaterializationRetractsPriorGenerationEdges(t *testing.T) {
	t.Parallel()

	source := resourceEnvelope("111122223333", "us-east-1", "aws_lambda_function",
		"arn:aws:lambda:us-east-1:111122223333:function:fn", "arn:aws:lambda:us-east-1:111122223333:function:fn")
	target := resourceEnvelope("111122223333", "us-east-1", "aws_kms_key",
		"arn:aws:kms:us-east-1:111122223333:key/abc", "arn:aws:kms:us-east-1:111122223333:key/abc")
	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_KMS_KEY",
		"source_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"source_arn":         "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"target_resource_id": "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_arn":         "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_type":        "aws_kms_key",
	})

	writer := &recordingCloudResourceEdgeWriter{}
	handler := AWSRelationshipMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{source, target, rel}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), awsRelationshipIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1 when a prior generation exists", writer.retractCalls)
	}
	if len(writer.retractScopeIDs) != 1 || writer.retractScopeIDs[0] != "scope-1" {
		t.Fatalf("retract scope ids = %v, want [scope-1]", writer.retractScopeIDs)
	}
}

func TestAWSRelationshipMaterializationSkipsFirstGenerationRetract(t *testing.T) {
	t.Parallel()

	source := resourceEnvelope("111122223333", "us-east-1", "aws_lambda_function",
		"arn:aws:lambda:us-east-1:111122223333:function:fn", "arn:aws:lambda:us-east-1:111122223333:function:fn")
	target := resourceEnvelope("111122223333", "us-east-1", "aws_kms_key",
		"arn:aws:kms:us-east-1:111122223333:key/abc", "arn:aws:kms:us-east-1:111122223333:key/abc")
	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_KMS_KEY",
		"source_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"source_arn":         "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"target_resource_id": "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_arn":         "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_type":        "aws_kms_key",
	})

	writer := &recordingCloudResourceEdgeWriter{}
	handler := AWSRelationshipMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{source, target, rel}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	if _, err := handler.Handle(context.Background(), awsRelationshipIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 0 {
		t.Fatalf("retractCalls = %d, want 0 on the first generation", writer.retractCalls)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1", writer.writeCalls)
	}
}

func TestAWSRelationshipMaterializationEmptyGenerationIsNoOp(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceEdgeWriter{}
	handler := AWSRelationshipMaterializationHandler{
		FactLoader:           &stubFactLoader{},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), awsRelationshipIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0 for empty generation", writer.writeCalls)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
}

func TestAWSRelationshipMaterializationPropagatesWriteError(t *testing.T) {
	t.Parallel()

	source := resourceEnvelope("111122223333", "us-east-1", "aws_lambda_function",
		"arn:aws:lambda:us-east-1:111122223333:function:fn", "arn:aws:lambda:us-east-1:111122223333:function:fn")
	target := resourceEnvelope("111122223333", "us-east-1", "aws_kms_key",
		"arn:aws:kms:us-east-1:111122223333:key/abc", "arn:aws:kms:us-east-1:111122223333:key/abc")
	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_KMS_KEY",
		"source_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"source_arn":         "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"target_resource_id": "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_arn":         "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_type":        "aws_kms_key",
	})

	writer := &recordingCloudResourceEdgeWriter{writeErr: errors.New("boom")}
	handler := AWSRelationshipMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{source, target, rel}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), awsRelationshipIntent()); err == nil {
		t.Fatal("expected the write error to propagate")
	}
}
