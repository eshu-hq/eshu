package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// recordingKubernetesCorrelationEdgeWriter captures RUNS_IMAGE edge writes and
// retracts so tests can assert the exact materialization request.
type recordingKubernetesCorrelationEdgeWriter struct {
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

func (w *recordingKubernetesCorrelationEdgeWriter) WriteKubernetesCorrelationEdges(
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

func (w *recordingKubernetesCorrelationEdgeWriter) RetractKubernetesCorrelationEdges(
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

func kubernetesCorrelationMaterializationIntent() Intent {
	return Intent{
		IntentID:     "intent-k8s-edge-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainKubernetesCorrelationMaterialization,
		EntityKeys:   []string{"kubernetes_workload_materialization:scope-1"},
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	}
}

// exactDigestEdgeFixture is the canonical positive case: one live workload whose
// image digest matches an active OCI manifest source node, which must materialize
// exactly one RUNS_IMAGE edge. The manifest fact carries both the
// registry+repository+digest (so the classifier resolves exact) and a
// descriptor_id (so the digest join index resolves the source node uid).
func exactDigestEdgeFixture() []facts.Envelope {
	imageRef := testK8sRegistry + "/" + testK8sRepository + "@" + testK8sDigest
	return []facts.Envelope{
		podTemplateFact("pod-1", "checkout", "uid-1", []string{imageRef}, map[string]string{"app": "checkout"}, false),
		k8sSourceManifestWithNode("oci-1", testK8sRegistry, testK8sRepository, testK8sDigest, testCheckoutDescriptorID(), false),
	}
}

func TestKubernetesCorrelationMaterializationRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := KubernetesCorrelationMaterializationHandler{
		FactLoader:      &stubKubernetesCorrelationFactLoader{},
		EdgeWriter:      &recordingKubernetesCorrelationEdgeWriter{},
		ReadinessLookup: readyLookup(true, true),
	}
	intent := kubernetesCorrelationMaterializationIntent()
	intent.Domain = DomainKubernetesCorrelation
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestKubernetesCorrelationMaterializationRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := KubernetesCorrelationMaterializationHandler{
		EdgeWriter:      &recordingKubernetesCorrelationEdgeWriter{},
		ReadinessLookup: readyLookup(true, true),
	}
	if _, err := handler.Handle(context.Background(), kubernetesCorrelationMaterializationIntent()); err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestKubernetesCorrelationMaterializationRequiresEdgeWriter(t *testing.T) {
	t.Parallel()

	handler := KubernetesCorrelationMaterializationHandler{
		FactLoader:      &stubKubernetesCorrelationFactLoader{},
		ReadinessLookup: readyLookup(true, true),
	}
	if _, err := handler.Handle(context.Background(), kubernetesCorrelationMaterializationIntent()); err == nil {
		t.Fatal("expected error when edge writer is nil")
	}
}

func TestKubernetesCorrelationMaterializationGatesOnWorkloadNodesPhase(t *testing.T) {
	t.Parallel()

	writer := &recordingKubernetesCorrelationEdgeWriter{}
	handler := KubernetesCorrelationMaterializationHandler{
		FactLoader:      &stubKubernetesCorrelationFactLoader{scopeFacts: exactDigestEdgeFixture()},
		EdgeWriter:      writer,
		ReadinessLookup: readyLookup(false, false), // workload nodes phase not yet committed
	}

	_, err := handler.Handle(context.Background(), kubernetesCorrelationMaterializationIntent())
	if err == nil {
		t.Fatal("expected a retryable error while the workload nodes phase is not ready")
	}
	if !IsRetryable(err) {
		t.Fatalf("error must be retryable so the intent re-enters the queue, got %v", err)
	}
	if writer.writeCalls != 0 || writer.retractCalls != 0 {
		t.Fatalf("no graph writes allowed before nodes commit: write=%d retract=%d", writer.writeCalls, writer.retractCalls)
	}
}

func TestKubernetesCorrelationMaterializationProjectsExactImageEdge(t *testing.T) {
	t.Parallel()

	writer := &recordingKubernetesCorrelationEdgeWriter{}
	handler := KubernetesCorrelationMaterializationHandler{
		FactLoader:           &stubKubernetesCorrelationFactLoader{scopeFacts: exactDigestEdgeFixture()},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), kubernetesCorrelationMaterializationIntent())
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
		t.Fatalf("written RUNS_IMAGE rows = %d, want 1", len(writer.writtenRows))
	}
	if writer.writeEvidence != kubernetesCorrelationEdgeEvidenceSource {
		t.Fatalf("write evidence = %q, want %q", writer.writeEvidence, kubernetesCorrelationEdgeEvidenceSource)
	}
	if writer.writeScopeID != "scope-1" {
		t.Fatalf("write scope id = %q, want scope-1", writer.writeScopeID)
	}
	if writer.writeGenerationID != "gen-1" {
		t.Fatalf("write generation id = %q, want gen-1", writer.writeGenerationID)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	row := writer.writtenRows[0]
	if got := anyToString(row["workload_uid"]); got != testCheckoutObjectID() {
		t.Fatalf("workload_uid = %q, want %q", got, testCheckoutObjectID())
	}
	if got := anyToString(row["source_uid"]); got != testCheckoutDescriptorID() {
		t.Fatalf("source_uid = %q, want %q", got, testCheckoutDescriptorID())
	}
}

func TestKubernetesCorrelationMaterializationProvenanceNotWritten(t *testing.T) {
	t.Parallel()

	// A derived tag->single-digest workload is provenance-only and must not
	// materialize an edge; the handler must succeed (graceful degrade).
	derivedRef := testK8sRegistry + "/" + testK8sRepository + ":v1.2.3"
	envelopes := append(
		exactDigestEdgeFixture(),
		podTemplateFact("pod-derived", "derived", "uid-d", []string{derivedRef}, map[string]string{"app": "d"}, false),
		k8sSourceTagFact("tag-derived", testK8sRegistry, testK8sRepository, "v1.2.3", testK8sDigest, "", false),
	)

	writer := &recordingKubernetesCorrelationEdgeWriter{}
	handler := KubernetesCorrelationMaterializationHandler{
		FactLoader:           &stubKubernetesCorrelationFactLoader{scopeFacts: envelopes},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), kubernetesCorrelationMaterializationIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	// Only the exact-digest checkout workload edges; the derived workload does not.
	if len(writer.writtenRows) != 1 {
		t.Fatalf("written RUNS_IMAGE rows = %d, want 1 (exact only, derived excluded)", len(writer.writtenRows))
	}
}

func TestKubernetesCorrelationMaterializationEmptyGenerationNoWrite(t *testing.T) {
	t.Parallel()

	writer := &recordingKubernetesCorrelationEdgeWriter{}
	handler := KubernetesCorrelationMaterializationHandler{
		FactLoader:           &stubKubernetesCorrelationFactLoader{scopeFacts: []facts.Envelope{}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	result, err := handler.Handle(context.Background(), kubernetesCorrelationMaterializationIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.writeCalls != 0 {
		t.Fatalf("writeCalls = %d, want 0 for empty generation", writer.writeCalls)
	}
	if writer.retractCalls != 0 {
		t.Fatalf("retractCalls = %d, want 0 on first empty generation", writer.retractCalls)
	}
}

func TestKubernetesCorrelationMaterializationRetractsPriorGenerationEdges(t *testing.T) {
	t.Parallel()

	writer := &recordingKubernetesCorrelationEdgeWriter{}
	handler := KubernetesCorrelationMaterializationHandler{
		FactLoader:           &stubKubernetesCorrelationFactLoader{scopeFacts: exactDigestEdgeFixture()},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), kubernetesCorrelationMaterializationIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("retractCalls = %d, want 1 when a prior generation exists", writer.retractCalls)
	}
	if writer.retractEvidence != kubernetesCorrelationEdgeEvidenceSource {
		t.Fatalf("retract evidence = %q, want %q", writer.retractEvidence, kubernetesCorrelationEdgeEvidenceSource)
	}
}

func TestKubernetesCorrelationMaterializationSkipsRetractOnFirstGeneration(t *testing.T) {
	t.Parallel()

	writer := &recordingKubernetesCorrelationEdgeWriter{}
	handler := KubernetesCorrelationMaterializationHandler{
		FactLoader:           &stubKubernetesCorrelationFactLoader{scopeFacts: exactDigestEdgeFixture()},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	if _, err := handler.Handle(context.Background(), kubernetesCorrelationMaterializationIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 0 {
		t.Fatalf("retractCalls = %d, want 0 on the first generation", writer.retractCalls)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("writeCalls = %d, want 1 (the exact image edge still materializes)", writer.writeCalls)
	}
}

func TestKubernetesCorrelationMaterializationReprojectionIsIdempotent(t *testing.T) {
	t.Parallel()

	// Two runs of the same generation (a reprojection/retry) must produce the same
	// single edge row each time: the extractor is deterministic and the write is
	// keyed on (workload_uid, RUNS_IMAGE, source_uid).
	loader := &stubKubernetesCorrelationFactLoader{scopeFacts: exactDigestEdgeFixture()}
	handler := KubernetesCorrelationMaterializationHandler{
		FactLoader:           loader,
		EdgeWriter:           &recordingKubernetesCorrelationEdgeWriter{},
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	first := &recordingKubernetesCorrelationEdgeWriter{}
	handler.EdgeWriter = first
	if _, err := handler.Handle(context.Background(), kubernetesCorrelationMaterializationIntent()); err != nil {
		t.Fatalf("first Handle error: %v", err)
	}
	second := &recordingKubernetesCorrelationEdgeWriter{}
	handler.EdgeWriter = second
	if _, err := handler.Handle(context.Background(), kubernetesCorrelationMaterializationIntent()); err != nil {
		t.Fatalf("second Handle error: %v", err)
	}
	if len(first.writtenRows) != 1 || len(second.writtenRows) != 1 {
		t.Fatalf("reprojection rows: first=%d second=%d, want 1 and 1", len(first.writtenRows), len(second.writtenRows))
	}
	if anyToString(first.writtenRows[0]["source_uid"]) != anyToString(second.writtenRows[0]["source_uid"]) {
		t.Fatal("reprojection produced a different edge source_uid; write is not deterministic")
	}
}
