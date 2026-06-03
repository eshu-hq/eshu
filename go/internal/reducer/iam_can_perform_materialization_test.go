package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// recordingIAMCanPerformWriter captures the edge and retract calls so tests assert
// the exact materialization request the handler issues.
type recordingIAMCanPerformWriter struct {
	edgeCalls       int
	edgeRows        []map[string]any
	scopeID         string
	generationID    string
	evidenceSource  string
	retractCalls    int
	retractScopeIDs []string
	retractEvidence string
	edgeErr         error
}

func (w *recordingIAMCanPerformWriter) WriteIAMCanPerformEdges(
	_ context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string,
) error {
	w.edgeCalls++
	w.edgeRows = append(w.edgeRows, rows...)
	w.scopeID = scopeID
	w.generationID = generationID
	w.evidenceSource = evidenceSource
	return w.edgeErr
}

func (w *recordingIAMCanPerformWriter) RetractIAMCanPerformEdges(
	_ context.Context, scopeIDs []string, _, evidenceSource string,
) error {
	w.retractCalls++
	w.retractScopeIDs = append(w.retractScopeIDs, scopeIDs...)
	w.retractEvidence = evidenceSource
	return nil
}

func iamCanPerformIntent() Intent {
	return Intent{
		IntentID:     "intent-iam-canperform-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainIAMCanPerformMaterialization,
		EntityKeys:   []string{"aws_resource_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

// iamCanPerformFacts builds a scope with one scanned principal, one scanned S3
// bucket, and one complete s3:GetObject grant — exactly one edge.
func iamCanPerformFacts() []facts.Envelope {
	return []facts.Envelope{
		iamNodeEnvelope(iamResourceTypeUser, attackerUserARN),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
	}
}

func iamCanPerformResourcePolicyFacts() []facts.Envelope {
	return []facts.Envelope{
		iamNodeEnvelope(iamResourceTypeUser, attackerUserARN),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
		canPerformResourcePolicyEnvelope(
			canPerformBucketARN,
			iamCanPerformResourceTypeS3Bucket,
			"Allow",
			[]string{"s3:getobject"},
			[]string{attackerUserARN},
		),
	}
}

func TestIAMCanPerformHandlerRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := IAMCanPerformMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		Writer:          &recordingIAMCanPerformWriter{},
		ReadinessLookup: allKeyspacesReady(),
	}
	intent := iamCanPerformIntent()
	intent.Domain = DomainSQLRelationshipMaterialization
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestIAMCanPerformHandlerRequiresFactLoaderAndWriter(t *testing.T) {
	t.Parallel()

	if _, err := (IAMCanPerformMaterializationHandler{
		Writer:          &recordingIAMCanPerformWriter{},
		ReadinessLookup: allKeyspacesReady(),
	}).Handle(context.Background(), iamCanPerformIntent()); err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
	if _, err := (IAMCanPerformMaterializationHandler{
		FactLoader:      &stubFactLoader{},
		ReadinessLookup: allKeyspacesReady(),
	}).Handle(context.Background(), iamCanPerformIntent()); err == nil {
		t.Fatal("expected error when writer is nil")
	}
}

// TestIAMCanPerformHandlerGatesOnCloudResourceUID proves the edge domain blocks
// (with a retryable error and zero writes) until the cloud_resource_uid
// canonical-nodes phase is committed.
func TestIAMCanPerformHandlerGatesOnCloudResourceUID(t *testing.T) {
	t.Parallel()

	writer := &recordingIAMCanPerformWriter{}
	handler := IAMCanPerformMaterializationHandler{
		FactLoader:      &stubFactLoader{envelopes: iamCanPerformFacts()},
		Writer:          writer,
		ReadinessLookup: readyExceptKeyspace(GraphProjectionKeyspaceCloudResourceUID),
	}
	_, err := handler.Handle(context.Background(), iamCanPerformIntent())
	if err == nil {
		t.Fatal("expected a not-ready error while cloud_resource_uid is uncommitted")
	}
	var retryable interface{ Retryable() bool }
	if !errors.As(err, &retryable) || !retryable.Retryable() {
		t.Fatalf("not-ready error must be retryable, got %v", err)
	}
	if writer.edgeCalls != 0 || writer.retractCalls != 0 {
		t.Fatalf("no writes allowed before the gate opens: %+v", writer)
	}
}

// TestIAMCanPerformHandlerProjectsResolvedEdge proves the post-gate happy path
// writes exactly one CAN_PERFORM edge with the right scope/evidence, action set,
// and identity_policy_only scope.
func TestIAMCanPerformHandlerProjectsResolvedEdge(t *testing.T) {
	t.Parallel()

	writer := &recordingIAMCanPerformWriter{}
	handler := IAMCanPerformMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: iamCanPerformFacts()},
		Writer:               writer,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	result, err := handler.Handle(context.Background(), iamCanPerformIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.edgeCalls != 1 || len(writer.edgeRows) != 1 {
		t.Fatalf("can_perform edge writes wrong: calls=%d rows=%d", writer.edgeCalls, len(writer.edgeRows))
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if writer.evidenceSource != iamCanPerformEvidenceSource {
		t.Fatalf("edge evidence = %q, want %q", writer.evidenceSource, iamCanPerformEvidenceSource)
	}
	row := writer.edgeRows[0]
	got := row["actions"].([]string)
	if len(got) != 1 || got[0] != "s3:getobject" {
		t.Fatalf("edge actions = %v, want [s3:getobject]", got)
	}
	if row["evaluation_scope"] != iamCanPerformEvaluationScope {
		t.Fatalf("edge evaluation_scope = %v, want %q", row["evaluation_scope"], iamCanPerformEvaluationScope)
	}
}

// TestIAMCanPerformHandlerProjectsResourcePolicyEdge proves the handler loads
// aws_resource_policy_permission facts and passes resource-policy CAN_PERFORM
// rows through the same idempotent writer path.
func TestIAMCanPerformHandlerProjectsResourcePolicyEdge(t *testing.T) {
	t.Parallel()

	writer := &recordingIAMCanPerformWriter{}
	handler := IAMCanPerformMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: iamCanPerformResourcePolicyFacts()},
		Writer:               writer,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	result, err := handler.Handle(context.Background(), iamCanPerformIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.edgeCalls != 1 || len(writer.edgeRows) != 1 {
		t.Fatalf("resource-policy can_perform edge writes wrong: calls=%d rows=%d", writer.edgeCalls, len(writer.edgeRows))
	}
	row := writer.edgeRows[0]
	if row["evaluation_scope"] != iamCanPerformEvaluationScopeResourcePolicyOnly {
		t.Fatalf("edge evaluation_scope = %v, want %q", row["evaluation_scope"], iamCanPerformEvaluationScopeResourcePolicyOnly)
	}
	if got := row["grant_sources"].([]string); len(got) != 1 || got[0] != iamCanPerformGrantSourceResourcePolicy {
		t.Fatalf("grant_sources = %v, want [resource_policy]", got)
	}
}

// TestIAMCanPerformHandlerIdempotentReprojection proves re-running the same
// generation issues the same edge and retracts the prior generation first.
func TestIAMCanPerformHandlerIdempotentReprojection(t *testing.T) {
	t.Parallel()

	newHandler := func(writer *recordingIAMCanPerformWriter) IAMCanPerformMaterializationHandler {
		return IAMCanPerformMaterializationHandler{
			FactLoader:           &stubFactLoader{envelopes: iamCanPerformFacts()},
			Writer:               writer,
			ReadinessLookup:      allKeyspacesReady(),
			PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
		}
	}

	first := &recordingIAMCanPerformWriter{}
	if _, err := newHandler(first).Handle(context.Background(), iamCanPerformIntent()); err != nil {
		t.Fatalf("first projection error: %v", err)
	}
	second := &recordingIAMCanPerformWriter{}
	if _, err := newHandler(second).Handle(context.Background(), iamCanPerformIntent()); err != nil {
		t.Fatalf("second projection error: %v", err)
	}
	if first.edgeRows[0]["resource_uid"] != second.edgeRows[0]["resource_uid"] {
		t.Fatal("reprojection must produce the same edge identity (idempotent MERGE)")
	}
	if second.retractCalls != 1 {
		t.Fatalf("reprojection with a prior generation must retract first, got %d", second.retractCalls)
	}
	if second.retractEvidence != iamCanPerformEvidenceSource {
		t.Fatalf("retract evidence = %q, want %q", second.retractEvidence, iamCanPerformEvidenceSource)
	}
}

// TestIAMCanPerformHandlerSkipsFirstGenerationRetract proves the first generation
// does not retract (no prior edges) but still writes the edge.
func TestIAMCanPerformHandlerSkipsFirstGenerationRetract(t *testing.T) {
	t.Parallel()

	writer := &recordingIAMCanPerformWriter{}
	handler := IAMCanPerformMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: iamCanPerformFacts()},
		Writer:               writer,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}
	if _, err := handler.Handle(context.Background(), iamCanPerformIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 0 {
		t.Fatalf("first generation must not retract, got %d", writer.retractCalls)
	}
	if writer.edgeCalls != 1 {
		t.Fatalf("first generation must still write the edge, got %d", writer.edgeCalls)
	}
}

// TestIAMCanPerformHandlerWildcardTargetIsGracefulNoEdge proves a wildcard-resource
// grant produces no edge but still succeeds (graceful degradation), and the
// skipped_ambiguous counter records it.
func TestIAMCanPerformHandlerWildcardTargetIsGracefulNoEdge(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}

	envs := []facts.Envelope{
		iamNodeEnvelope(iamResourceTypeUser, attackerUserARN),
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{"*"}),
	}
	writer := &recordingIAMCanPerformWriter{}
	handler := IAMCanPerformMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: envs},
		Writer:               writer,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
		Instruments:          instruments,
	}
	result, err := handler.Handle(context.Background(), iamCanPerformIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.edgeCalls != 0 {
		t.Fatalf("wildcard target must not write an edge, got %d", writer.edgeCalls)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	if !metricHasAttrs(rm, "eshu_dp_iam_can_perform_skipped_total", map[string]string{"skip_reason": iamCanPerformSkipAmbiguous}) {
		t.Fatal("expected eshu_dp_iam_can_perform_skipped_total{skip_reason=skipped_ambiguous}")
	}
}

// iamCanPerformBoundaryAllowedFacts builds a scope where the principal carries a
// permission boundary that ALSO allows the catalogued action on the resolved
// bucket, so the identity allow survives intersection (PR4c) and the edge records
// the boundary was evaluated.
func iamCanPerformBoundaryAllowedFacts() []facts.Envelope {
	return []facts.Envelope{
		iamNodeEnvelope(iamResourceTypeUser, attackerUserARN),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		boundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
	}
}

// TestIAMCanPerformHandlerProjectsBoundaryIntersectedEdge proves the handler writes
// an edge for a bounded principal whose boundary allows the action, stamping
// boundary_evaluated=true and the identity_policy_and_boundary scope through the
// real writer path (reducer graph truth agrees with the extractor).
func TestIAMCanPerformHandlerProjectsBoundaryIntersectedEdge(t *testing.T) {
	t.Parallel()

	writer := &recordingIAMCanPerformWriter{}
	handler := IAMCanPerformMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: iamCanPerformBoundaryAllowedFacts()},
		Writer:               writer,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	result, err := handler.Handle(context.Background(), iamCanPerformIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.edgeCalls != 1 || len(writer.edgeRows) != 1 {
		t.Fatalf("boundary-intersected edge writes wrong: calls=%d rows=%d", writer.edgeCalls, len(writer.edgeRows))
	}
	row := writer.edgeRows[0]
	if got, ok := row["boundary_evaluated"].(bool); !ok || !got {
		t.Fatalf("edge boundary_evaluated = %v, want true", row["boundary_evaluated"])
	}
	if row["evaluation_scope"] != iamCanPerformEvaluationScopeIdentityPolicyBoundary {
		t.Fatalf("edge evaluation_scope = %v, want %q", row["evaluation_scope"], iamCanPerformEvaluationScopeIdentityPolicyBoundary)
	}
}

// TestIAMCanPerformHandlerBoundarySuppressionIsGracefulNoEdge proves a bounded
// principal whose boundary does NOT allow the catalogued action produces no edge
// but still succeeds, and the bounded skip_reason=skipped_boundary_no_allow counter
// records the suppression so the boundary intersection is operator-visible.
func TestIAMCanPerformHandlerBoundarySuppressionIsGracefulNoEdge(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}

	envs := []facts.Envelope{
		iamNodeEnvelope(iamResourceTypeUser, attackerUserARN),
		canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
		escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		// Boundary allows a different action only: s3:getobject is non-effective.
		boundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:putobject"}, []string{canPerformBucketARN}),
	}
	writer := &recordingIAMCanPerformWriter{}
	handler := IAMCanPerformMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: envs},
		Writer:               writer,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
		Instruments:          instruments,
	}
	result, err := handler.Handle(context.Background(), iamCanPerformIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.edgeCalls != 0 {
		t.Fatalf("boundary suppression must write no edge, got %d", writer.edgeCalls)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	if !metricHasAttrs(rm, "eshu_dp_iam_can_perform_skipped_total", map[string]string{"skip_reason": iamCanPerformSkipBoundaryNoAllow}) {
		t.Fatal("expected eshu_dp_iam_can_perform_skipped_total{skip_reason=skipped_boundary_no_allow}")
	}
}

// TestIAMCanPerformHandlerRecordsEdgeResolutionMode proves the resolved happy path
// records eshu_dp_iam_can_perform_edges_total{resolution_mode=exact_arn}.
func TestIAMCanPerformHandlerRecordsEdgeResolutionMode(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}

	handler := IAMCanPerformMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: iamCanPerformFacts()},
		Writer:               &recordingIAMCanPerformWriter{},
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
		Instruments:          instruments,
	}
	if _, err := handler.Handle(context.Background(), iamCanPerformIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	if !metricHasAttrs(rm, "eshu_dp_iam_can_perform_edges_total", map[string]string{"resolution_mode": iamCanPerformResolutionExactARN}) {
		t.Fatal("expected eshu_dp_iam_can_perform_edges_total{resolution_mode=exact_arn}")
	}
}
