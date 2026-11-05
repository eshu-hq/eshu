// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// manifestRefusedStubError stands in for the store's counted non-retryable
// refusal: the handler must propagate it opaquely, so any non-retryable
// error exercises the fail-before-emit path. The store's own tests pin the
// production refusal type's Retryable and FailureClass.
type manifestRefusedStubError struct{}

func (manifestRefusedStubError) Error() string {
	return "manifest dependency plain read refused delta-active scope repository:repo-consumer (union_base_missing): failing before the consumer's retract"
}

func (manifestRefusedStubError) Retryable() bool { return false }

func (s *stubPackageSourceFactLoader) ListActivePackageManifestDependencyFactsWithRefusals(
	_ context.Context,
	ecosystems []string,
	packageNames []string,
) ([]facts.Envelope, bool, error) {
	s.manifestRefusingCalls++
	s.manifestEcosystems = append([]string(nil), ecosystems...)
	s.manifestPackageNames = append([]string(nil), packageNames...)
	if s.manifestRefusalErr != nil {
		return nil, true, s.manifestRefusalErr
	}
	return append([]facts.Envelope(nil), s.manifestDependencies...), false, nil
}

// TestPackageSourceHandlerFailsBeforeEmitOnAManifestRefusal proves #10109:
// a refused manifest base fails the whole intent before the handler writes
// correlations or emits repo-dependency intents, so the shared lane never
// retracts edges on delta-only rows. A silent partial retract is forbidden.
func TestPackageSourceHandlerFailsBeforeEmitOnAManifestRefusal(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	loader := &stubPackageSourceFactLoader{
		scopeFacts: []facts.Envelope{
			packageRegistryPackageFact(
				"pkg:npm://registry.example/orphan-api",
				"npm",
				"orphan-api",
				"",
				observedAt,
			),
			packageSourceHintFact(
				"pkg:npm://registry.example/orphan-api",
				"repository",
				"https://github.com/acme/orphan-api",
				observedAt,
			),
		},
		manifestDependencies: []facts.Envelope{
			packageManifestDependencyFact(
				"repo-consumer",
				"consumer",
				"package.json",
				"orphan-api",
				"npm",
				"^1.0.0",
				observedAt,
			),
		},
		manifestRefusalErr: manifestRefusedStubError{},
	}
	intentWriter := &recordingRepoDependencyIntentWriter{}
	writer := &recordingPackageWriter{}
	handler := PackageSourceHandler{
		FactLoader:                 loader,
		Writer:                     writer,
		RepoDependencyIntentWriter: intentWriter,
		Now:                        func() time.Time { return observedAt },
	}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:        "intent-orphan-refused",
		ScopeID:         "package-registry:npm:orphan-api",
		GenerationID:    "generation-2",
		SourceSystem:    "package_registry",
		Domain:          reducercontract.DomainPackageSourceCorrelation,
		Cause:           "package registry source hints observed",
		RelatedScopeIDs: []string{"package-registry:npm:orphan-api"},
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want the manifest refusal before any emit")
	}
	if reducercontract.IsRetryable(err) {
		t.Fatalf("IsRetryable(%v) = true, want false: a retry reads the same refused base", err)
	}
	if len(intentWriter.rows) != 0 {
		t.Fatalf("RepoDependencyIntentWriter calls = %d, want 0: no refresh/retract on a refusal", len(intentWriter.rows))
	}
	if writer.calls != 0 {
		t.Fatalf("WriteCorrelations calls = %d, want 0: no write on a refusal", writer.calls)
	}
	if loader.manifestCalls != 0 {
		t.Fatalf("ListActivePackageManifestDependencyFacts() calls = %d, want 0: the refusing read replaces it", loader.manifestCalls)
	}
	if loader.manifestRefusingCalls != 1 {
		t.Fatalf("ListActivePackageManifestDependencyFactsWithRefusals() calls = %d, want 1", loader.manifestRefusingCalls)
	}
}
