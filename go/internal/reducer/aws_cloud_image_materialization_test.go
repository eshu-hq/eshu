// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fakeContainerImageExistenceLookup is a test double for
// ContainerImageExistenceLookup: existing names the exact uids
// ExistingContainerImageUIDs reports as present; every other uid is absent.
type fakeContainerImageExistenceLookup struct {
	existing map[string]struct{}
	calls    int
	lastUIDs []string
	err      error
}

func (l *fakeContainerImageExistenceLookup) ExistingContainerImageUIDs(
	_ context.Context, uids []string,
) (map[string]struct{}, error) {
	l.calls++
	l.lastUIDs = append([]string(nil), uids...)
	if l.err != nil {
		return nil, l.err
	}
	found := make(map[string]struct{}, len(uids))
	for _, uid := range uids {
		if _, ok := l.existing[uid]; ok {
			found[uid] = struct{}{}
		}
	}
	return found, nil
}

// recordingCloudResourceContainerImageEdgeWriter captures AWS cloud-image
// edge writes and retracts so tests can assert the exact materialization
// request.
type recordingCloudResourceContainerImageEdgeWriter struct {
	writeCalls        int
	writtenRows       []map[string]any
	writeScopeID      string
	writeGenerationID string
	writeEvidence     string
	retractCalls      int
	retractScopeIDs   []string
	retractEvidence   string
	writeErr          error
	retractErr        error
}

func (w *recordingCloudResourceContainerImageEdgeWriter) WriteCloudResourceContainerImageEdges(
	_ context.Context,
	rows []map[string]any,
	scopeID string,
	generationID string,
	evidenceSource string,
) error {
	w.writeCalls++
	w.writtenRows = append(w.writtenRows, rows...)
	w.writeScopeID = scopeID
	w.writeGenerationID = generationID
	w.writeEvidence = evidenceSource
	return w.writeErr
}

func (w *recordingCloudResourceContainerImageEdgeWriter) RetractCloudResourceContainerImageEdges(
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

func awsCloudImageIntent() Intent {
	return Intent{
		IntentID:     "intent-aws-cloud-image-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainAWSCloudImageMaterialization,
		EntityKeys:   []string{"aws_resource_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

// awsCloudImageFixture is one Lambda function whose relationship carries a
// resolved digest, plus an ECS task definition whose relationship is tag-only
// (must stay Postgres-only).
//
// awsCloudImageFixtureTargetUID is the exact :ContainerImage node uid the
// Lambda relationship's resolved_image_uri
// ("123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc") computes
// via containerImageNodeUIDFromDigestRef, so tests can wire
// ContainerImageExistenceLookup against the same identity the extraction
// path derives.
const awsCloudImageFixtureTargetUID = "oci-descriptor://123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc"

func awsCloudImageFixture() []facts.Envelope {
	const acct = "123456789012"
	fnARN := "arn:aws:lambda:us-east-1:123456789012:function:demo"
	tdARN := "arn:aws:ecs:us-east-1:123456789012:task-definition/demo:1"
	return []facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id": acct, "region": "us-east-1",
			"resource_type": "aws_lambda_function", "resource_id": fnARN, "arn": fnARN,
		}),
		awsResourceEnvelope(map[string]any{
			"account_id": acct, "region": "us-east-1",
			"resource_type": "aws_ecs_task_definition", "resource_id": tdARN, "arn": tdARN,
		}),
		awsRelationshipEnvelope(map[string]any{
			"account_id": acct, "region": "us-east-1",
			"relationship_type":  lambdaFunctionUsesImageRelationshipType,
			"source_resource_id": fnARN, "source_arn": fnARN,
			"target_resource_id": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
			"target_type":        "container_image",
			"attributes": map[string]any{
				"package_type":       "Image",
				"resolved_image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc",
			},
		}),
		awsRelationshipEnvelope(map[string]any{
			"account_id": acct, "region": "us-east-1",
			"relationship_type":  ecsTaskDefinitionUsesImageRelationshipType,
			"source_resource_id": tdARN, "source_arn": tdARN,
			"target_resource_id": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
			"target_type":        "container_image",
		}),
	}
}

func TestAWSCloudImageMaterializationRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := AWSCloudImageMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		EdgeWriter:      &recordingCloudResourceContainerImageEdgeWriter{},
		ReadinessLookup: readyLookup(true, true),
	}
	intent := awsCloudImageIntent()
	intent.Domain = DomainAWSRelationshipMaterialization
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestAWSCloudImageMaterializationRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := AWSCloudImageMaterializationHandler{
		EdgeWriter:      &recordingCloudResourceContainerImageEdgeWriter{},
		ReadinessLookup: readyLookup(true, true),
	}
	if _, err := handler.Handle(context.Background(), awsCloudImageIntent()); err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestAWSCloudImageMaterializationRequiresEdgeWriter(t *testing.T) {
	t.Parallel()

	handler := AWSCloudImageMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		ReadinessLookup: readyLookup(true, true),
	}
	if _, err := handler.Handle(context.Background(), awsCloudImageIntent()); err == nil {
		t.Fatal("expected error when edge writer is nil")
	}
}

func TestAWSCloudImageMaterializationGatesOnCanonicalNodesPhase(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceContainerImageEdgeWriter{}
	handler := AWSCloudImageMaterializationHandler{
		FactLoader:      &stubFactLoader{envelopes: awsCloudImageFixture()},
		EdgeWriter:      writer,
		ReadinessLookup: readyLookup(false, false),
	}

	_, err := handler.Handle(context.Background(), awsCloudImageIntent())
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

func TestAWSCloudImageMaterializationProjectsLambdaImageEdgeAndSkipsECSTagOnly(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	writer := &recordingCloudResourceContainerImageEdgeWriter{}
	existence := &fakeContainerImageExistenceLookup{
		existing: map[string]struct{}{awsCloudImageFixtureTargetUID: {}},
	}
	handler := AWSCloudImageMaterializationHandler{
		FactLoader:              &stubFactLoader{envelopes: awsCloudImageFixture()},
		EdgeWriter:              writer,
		ReadinessLookup:         readyLookup(true, true),
		PriorGenerationCheck:    func(context.Context, string, string) (bool, error) { return true, nil },
		ContainerImageExistence: existence,
		Instruments:             inst,
	}

	result, err := handler.Handle(context.Background(), awsCloudImageIntent())
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
		t.Fatalf("written rows = %d, want 1 (lambda only; ECS task-def stays postgres-only)", len(writer.writtenRows))
	}
	if writer.writeEvidence != awsCloudImageEvidenceSource {
		t.Fatalf("write evidence = %q, want %q", writer.writeEvidence, awsCloudImageEvidenceSource)
	}
	if writer.writeScopeID != "scope-1" {
		t.Fatalf("write scope id = %q, want scope-1", writer.writeScopeID)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1", writer.retractCalls)
	}
	if writer.retractEvidence != awsCloudImageEvidenceSource {
		t.Fatalf("retract evidence = %q, want %q", writer.retractEvidence, awsCloudImageEvidenceSource)
	}
	if existence.calls != 1 {
		t.Fatalf("ContainerImageExistence calls = %d, want 1", existence.calls)
	}

	// The golden case: target EXISTS, so the metric MUST still increment --
	// this is the counterpart proof to the target-missing test below, showing
	// the new existence filter does not regress the already-materializing
	// case (issue #5450 P1 follow-up).
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if !metricHasAttrs(rm, "eshu_dp_aws_cloud_image_edges_total", map[string]string{
		telemetry.MetricDimensionResolutionMode: awsCloudImageResolutionMode,
	}) {
		t.Fatal("eshu_dp_aws_cloud_image_edges_total must increment when the target ContainerImage exists")
	}
}

func TestAWSCloudImageMaterializationIdempotentOnReprojection(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceContainerImageEdgeWriter{}
	handler := AWSCloudImageMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: awsCloudImageFixture()},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	first, err := handler.Handle(context.Background(), awsCloudImageIntent())
	if err != nil {
		t.Fatalf("first Handle error: %v", err)
	}
	second, err := handler.Handle(context.Background(), awsCloudImageIntent())
	if err != nil {
		t.Fatalf("second Handle error: %v", err)
	}
	if first.CanonicalWrites != second.CanonicalWrites {
		t.Fatalf("reprojection changed write count: first=%d second=%d", first.CanonicalWrites, second.CanonicalWrites)
	}
	if writer.writeCalls != 2 {
		t.Fatalf("writeCalls = %d, want 2 (one per reprojection)", writer.writeCalls)
	}
}

// TestAWSCloudImageMaterializationRetractsPriorEdgeWhenRelationshipDisappears
// is the handler-side half of the #5450 retraction-safety proof (the
// enqueue-side half lives in
// internal/projector/aws_cloud_image_materialization_intents_test.go's
// TestBuildProjectionQueuesAWSCloudImageMaterializationWithoutLambdaRelationship).
// It simulates the Zip-switch scenario directly against Handle: a later
// generation for the SAME scope carries an aws_resource fact for the Lambda
// function but NO lambda_function_uses_image relationship (the fact simply
// stopped appearing, exactly like a package_type flip from Image to Zip).
// PriorGenerationCheck reports hasPrior=true (this is NOT the scope's first
// generation), so Handle must retract unconditionally BEFORE checking
// whether there are any rows to write -- proving retract-first already holds
// even when the current relationship set is empty, so pairing it with the
// projector's now-persistent aws_resource trigger correctly converges the
// edge set to zero instead of leaving a stale prior edge.
func TestAWSCloudImageMaterializationRetractsPriorEdgeWhenRelationshipDisappears(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceContainerImageEdgeWriter{}
	fnARN := "arn:aws:lambda:us-east-1:123456789012:function:demo"
	handler := AWSCloudImageMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			// Lambda function still exists (still scanned every generation)
			// but its image relationship is gone -- e.g. switched to Zip.
			awsResourceEnvelope(map[string]any{
				"account_id": "123456789012", "region": "us-east-1",
				"resource_type": "aws_lambda_function", "resource_id": fnARN, "arn": fnARN,
			}),
		}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), awsCloudImageIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1: retract-first must run even with zero current relationships, or a Zip-switched Lambda's prior edge stays stale forever", writer.retractCalls)
	}
	if writer.retractEvidence != awsCloudImageEvidenceSource {
		t.Fatalf("retract evidence = %q, want %q", writer.retractEvidence, awsCloudImageEvidenceSource)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0 (no relationship to project this generation)", writer.writeCalls)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0", result.CanonicalWrites)
	}
}

func TestAWSCloudImageMaterializationEmptyGenerationNoWrite(t *testing.T) {
	t.Parallel()

	writer := &recordingCloudResourceContainerImageEdgeWriter{}
	handler := AWSCloudImageMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: nil},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	result, err := handler.Handle(context.Background(), awsCloudImageIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 for empty generation", result.CanonicalWrites)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0 for empty generation", writer.writeCalls)
	}
	if writer.retractCalls != 0 {
		t.Fatalf("retractCalls = %d, want 0 (no prior generation, first attempt)", writer.retractCalls)
	}
}
