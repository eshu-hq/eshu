// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestCICDRunCorrelationHandlerQuarantinesRunMissingRunID is the flagship
// regression test for the ci_cd_run family's typed-decode migration (Contract
// System v1, Wave 4d, mirroring
// TestSBOMAttestationAttachmentQuarantinesMissingDocumentID and
// TestBuildSupplyChainImpactFindingsQuarantinesOSPackageMissingInstalledVersion).
// It proves the accuracy guarantee the migration exists to protect AND the
// per-fact isolation contract every prior wave established: a ci.run fact
// missing its required run_id key is QUARANTINED as a visible input_invalid
// dead-letter — never silently producing an empty-string run join key — while
// a VALID sibling ci.run fact in the same batch still produces a correlation
// decision.
//
// Before the migration this behavior was impossible: cicdRunKey read run_id
// with payloadString, which returns "" for the absent key, so a malformed
// ci.run fact either collapsed onto another run sharing an empty run_id
// segment or, more commonly, produced its own decision keyed by the empty
// string with no operator-visible signal that the fact was malformed.
func TestCICDRunCorrelationHandlerQuarantinesRunMissingRunID(t *testing.T) {
	t.Parallel()

	// A ci.run fact whose required run_id key is ABSENT (not merely empty).
	// Everything else is present so the ONLY reason to quarantine the fact is
	// the missing required field.
	malformed := facts.Envelope{
		FactID:   "malformed-run",
		FactKind: facts.CICDRunFactKind,
		Payload: map[string]any{
			// "run_id" intentionally absent.
			"provider":      "github_actions",
			"run_attempt":   "1",
			"repository_id": "repo-api",
			"commit_sha":    "abc123",
			"status":        "completed",
			"result":        "success",
		},
	}
	// A fully valid, independent run that must still produce a correlation
	// decision despite the malformed fact sharing the batch.
	valid := ciRunFact("run-valid", "github_actions", "repo-api", "def456")

	loader := &stubCICDRunCorrelationFactLoader{scopeFacts: []facts.Envelope{malformed, valid}}
	writer := &recordingCICDRunCorrelationWriter{}
	handler := CICDRunCorrelationHandler{
		FactLoader: loader,
		Writer:     writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-cicd-quarantine",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "run-valid:1",
		SourceSystem: "ci_cd_run",
		Domain:       DomainCICDRunCorrelation,
		Cause:        "ci run observed",
	})
	// Per-fact isolation: the malformed fact does NOT fail the whole intent.
	if err != nil {
		t.Fatalf("Handle returned error %v; a single malformed ci.run fact must be quarantined per-fact, not fail the whole intent", err)
	}

	// The malformed fact must be counted as an input_invalid quarantine in the
	// Result SubSignals so the operator sees it on the per-intent signal (each
	// quarantined fact is also on the eshu_dp_reducer_input_invalid_facts_total
	// counter and a structured error log).
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1; the missing-run_id fact must be recorded as one input_invalid quarantine", got)
	}

	// The batch's VALID run must still produce a correlation decision:
	// isolation means a poisoned sibling never suppresses valid evidence.
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1", writer.calls)
	}
	found := false
	for _, decision := range writer.write.Decisions {
		if decision.RunID == "run-valid" {
			found = true
		}
		if decision.RunID == "" {
			t.Fatalf("a decision was produced under the empty-string run id; the quarantined fact must never surface graph identity: %+v", decision)
		}
	}
	if !found {
		t.Fatalf("no decision produced for the valid sibling run run-valid; got %+v", writer.write.Decisions)
	}
}

// TestCICDRunCorrelationHandlerQuarantinesWorkflowImageEvidenceMissingRepositoryID
// proves the same accuracy/isolation contract for
// ci.workflow_image_evidence: a fact missing its required repository_id join
// key is quarantined per-fact rather than silently failing to attach to any
// run, while a valid sibling run's own correlation decision is unaffected.
func TestCICDRunCorrelationHandlerQuarantinesWorkflowImageEvidenceMissingRepositoryID(t *testing.T) {
	t.Parallel()

	malformedWorkflowImage := facts.Envelope{
		FactID:   "malformed-workflow-image",
		FactKind: facts.CICDWorkflowImageEvidenceFactKind,
		Payload: map[string]any{
			// "repository_id" intentionally absent.
			"workflow_path":  ".github/workflows/build.yml",
			"evidence_class": "workflow_image_ref",
			"image_ref":      "registry.example.com/team/api:prod",
		},
	}
	validRun := ciRunFact("run-workflow-valid", "github_actions", "repo-api", "abc123")

	loader := &stubCICDRunCorrelationFactLoader{scopeFacts: []facts.Envelope{malformedWorkflowImage, validRun}}
	writer := &recordingCICDRunCorrelationWriter{}
	handler := CICDRunCorrelationHandler{
		FactLoader: loader,
		Writer:     writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-cicd-workflow-image-quarantine",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "run-workflow-valid:1",
		SourceSystem: "ci_cd_run",
		Domain:       DomainCICDRunCorrelation,
		Cause:        "ci run observed",
	})
	if err != nil {
		t.Fatalf("Handle returned error %v; a single malformed ci.workflow_image_evidence fact must be quarantined per-fact, not fail the whole intent", err)
	}
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1; the missing-repository_id workflow image fact must be recorded as one input_invalid quarantine", got)
	}
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1", writer.calls)
	}
	found := false
	for _, decision := range writer.write.Decisions {
		if decision.RunID == "run-workflow-valid" {
			found = true
			if decision.Outcome != CICDRunCorrelationDerived {
				t.Fatalf("decision.Outcome = %q, want %q; the quarantined workflow image evidence must not attach to the valid run", decision.Outcome, CICDRunCorrelationDerived)
			}
		}
	}
	if !found {
		t.Fatalf("no decision produced for the valid sibling run run-workflow-valid; got %+v", writer.write.Decisions)
	}
}

// TestCICDRunCorrelationHandlerQuarantineReplayIsIdempotent proves replaying
// the exact same batch (including the quarantined malformed ci.run fact)
// through Handle twice converges on the same quarantine count and the same
// decision for the valid sibling run each time — the typed-decode migration
// introduces no new source of nondeterminism into the reducer's
// at-least-once delivery / idempotent-convergence contract
// (docs/internal/design/contract-system-v1.md §3.4).
func TestCICDRunCorrelationHandlerQuarantineReplayIsIdempotent(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "malformed-run",
		FactKind: facts.CICDRunFactKind,
		Payload: map[string]any{
			// "run_id" intentionally absent.
			"provider":      "github_actions",
			"repository_id": "repo-api",
			"commit_sha":    "abc123",
		},
	}
	valid := ciRunFact("run-valid-replay", "github_actions", "repo-api", "def456")

	intent := Intent{
		IntentID:     "intent-cicd-replay",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "run-valid-replay:1",
		SourceSystem: "ci_cd_run",
		Domain:       DomainCICDRunCorrelation,
		Cause:        "ci run observed",
	}

	var results []Result
	for i := 0; i < 2; i++ {
		loader := &stubCICDRunCorrelationFactLoader{scopeFacts: []facts.Envelope{malformed, valid}}
		writer := &recordingCICDRunCorrelationWriter{}
		handler := CICDRunCorrelationHandler{
			FactLoader: loader,
			Writer:     writer,
		}
		result, err := handler.Handle(context.Background(), intent)
		if err != nil {
			t.Fatalf("replay %d: Handle returned error %v, want nil (per-fact quarantine, never a whole-intent failure)", i, err)
		}
		results = append(results, result)
		if len(writer.write.Decisions) != 1 {
			t.Fatalf("replay %d: len(Decisions) = %d, want 1", i, len(writer.write.Decisions))
		}
	}

	if results[0].SubSignals["input_invalid_facts"] != results[1].SubSignals["input_invalid_facts"] {
		t.Fatalf("input_invalid_facts differs across replays: %v vs %v; the quarantine decision must be deterministic",
			results[0].SubSignals["input_invalid_facts"], results[1].SubSignals["input_invalid_facts"])
	}
	if results[0].CanonicalWrites != results[1].CanonicalWrites {
		t.Fatalf("CanonicalWrites differs across replays: %d vs %d", results[0].CanonicalWrites, results[1].CanonicalWrites)
	}
	if results[0].Status != ResultStatusSucceeded || results[1].Status != ResultStatusSucceeded {
		t.Fatalf("both replays must succeed despite the quarantined fact: got %v and %v", results[0].Status, results[1].Status)
	}
}
