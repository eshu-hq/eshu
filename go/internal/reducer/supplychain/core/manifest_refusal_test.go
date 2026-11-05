// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// manifestRefusedStubError stands in for the store's counted non-retryable
// refusal: the handler branches on the refused signal, not the error type,
// so any error with refused=true exercises the partial-evidence path. The
// store's own tests pin the production refusal type.
type manifestRefusedStubError struct{}

func (manifestRefusedStubError) Error() string {
	return "manifest dependency plain read refused delta-active scope repository:r_api (union_base_missing): failing before the consumer's retract"
}

func (s *manifestBackedSupplyChainImpactLoader) ListActivePackageManifestDependencyFactsWithRefusals(
	_ context.Context,
	ecosystems []string,
	packageNames []string,
) ([]facts.Envelope, bool, error) {
	s.manifestRefusingCalls++
	s.manifestEcosystem = append([]string(nil), ecosystems...)
	s.manifestNames = append([]string(nil), packageNames...)
	if s.manifestRefusalErr != nil {
		return nil, true, s.manifestRefusalErr
	}
	return append([]facts.Envelope(nil), s.manifestFacts...), false, nil
}

// TestSupplyChainImpactHandlerTreatsAManifestRefusalAsPartialEvidence proves
// #10109: a refused manifest base marks the pass partial (upsert only,
// retract nothing) instead of failing the intent or retracting on delta-only
// rows. The writer's PartialEvidence contract already pins the no-retraction
// half; this test pins the load half through the real Handle path.
func TestSupplyChainImpactHandlerTreatsAManifestRefusalAsPartialEvidence(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 26, 8, 0, 0, 0, time.UTC)
	loader := &manifestBackedSupplyChainImpactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-undici", "CVE-2026-0001", 7.5),
			vulnerabilityAffectedPackageFact(
				"affected-undici",
				"CVE-2026-0001",
				"npm://registry.npmjs.org/undici",
				"npm",
				"undici",
				"6.23.0",
				"6.23.1",
			),
		},
		manifestFacts: []facts.Envelope{
			packageManifestDependencyFactWithMetadata(
				testImpactRepositoryID,
				"api",
				"package-lock.json",
				"undici",
				"npm",
				"6.23.0",
				observedAt,
				map[string]any{
					"section":           "package-lock",
					"lockfile":          true,
					"dependency_path":   []any{"fetch-client", "undici"},
					"dependency_depth":  2,
					"direct_dependency": false,
				},
			),
		},
		manifestRefusalErr: manifestRefusedStubError{},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FencingTokenIssuer: newTestImpactFencingTokenIssuer(), FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-impact-undici-refused",
		ScopeID:      "vuln-intel://osv/npm/undici@6.23.0",
		GenerationID: "generation-impact-undici",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil: a refusal degrades to partial, not a failure", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want %q", result.Status, reducercontract.ResultStatusSucceeded)
	}
	if !writer.write.PartialEvidence {
		t.Fatal("PartialEvidence = false, want true: a refused manifest base is incomplete evidence")
	}
	if got := result.SubSignals["evidence_partial"]; got != 1 {
		t.Fatalf("evidence_partial sub-signal = %v, want 1", got)
	}
	if got := result.SubSignals["evidence_truncated_manifest_refused"]; got != 1 {
		t.Fatalf("evidence_truncated_manifest_refused sub-signal = %v, want 1", got)
	}
	if !strings.Contains(result.EvidenceSummary, "active_evidence_truncated=true") {
		t.Fatalf("EvidenceSummary = %q, want the active_evidence_truncated marker", result.EvidenceSummary)
	}
	if got := result.SubSignals["findings_retracted"]; got != 0 {
		t.Fatalf("findings_retracted sub-signal = %v, want 0: a partial pass retracts nothing", got)
	}
	if loader.manifestRefusingCalls != 1 {
		t.Fatalf("ListActivePackageManifestDependencyFactsWithRefusals() calls = %d, want 1", loader.manifestRefusingCalls)
	}
}
