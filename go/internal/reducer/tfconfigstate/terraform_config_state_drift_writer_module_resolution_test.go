// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfconfigstate

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
)

// Module-resolution-confidence outcome tests (issue #5572). Split from
// terraform_config_state_drift_writer_test.go to keep both files under the
// CLAUDE.md 500-line cap; same package, reuses exactDriftCandidate and the
// fakeWorkloadIdentityExecer / decodeBatchedVersionedFactCalls helpers from
// the sibling file.

// moduleResolutionDriftCandidate mirrors exactDriftCandidate but adds the
// EvidenceTypeModuleResolutionConfidence atom BuildCandidates attaches
// (go/internal/correlation/drift/tfconfigstate/candidate.go) when the
// config-side ResourceRow carried a non-empty ModuleResolutionReason.
func moduleResolutionDriftCandidate(address, driftKind, reason string) model.Candidate {
	c := exactDriftCandidate(address, driftKind)
	c.Evidence = append(c.Evidence, model.EvidenceAtom{
		ID:           c.ID + "/module_resolution",
		SourceSystem: "reducer/terraform_config_state_drift",
		EvidenceType: "terraform_module_resolution_confidence",
		ScopeID:      "state_snapshot:s3:hash-1",
		Key:          "module_resolution_reason",
		Value:        reason,
		Confidence:   1,
	})
	return c
}

// TestPostgresTerraformConfigStateDriftWriterDowngradesOutcomeToDerivedWhenModuleResolutionReasonPresent
// proves the writer downgrades a per-address finding's Outcome from "exact"
// to "derived" (issue #5572) whenever its candidate carries a
// EvidenceTypeModuleResolutionConfidence atom -- the config-side address
// depended on an unresolved module-prefix fallback (a Terraform-Registry-
// shorthand misclassification or a depth-exceeded module chain) and cannot
// be trusted as certainly correct. The specific reason must still be
// readable from the row's Evidence array, not just implied by the outcome
// value, so an operator can tell the two false-positive causes apart.
func TestPostgresTerraformConfigStateDriftWriterDowngradesOutcomeToDerivedWhenModuleResolutionReasonPresent(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresTerraformConfigStateDriftWriter{DB: db}

	write := TerraformConfigStateDriftWrite{
		IntentID:     "intent-drift-derived-1",
		ScopeID:      "state_snapshot:s3:hash-1",
		GenerationID: "generation-drift-1",
		SourceSystem: "collector/terraform-state",
		Cause:        "drift intent",
		BackendKind:  "s3",
		LocatorHash:  "hash-1",
		Candidates: []model.Candidate{
			moduleResolutionDriftCandidate("aws_instance.web", "added_in_config", "external_registry"),
			exactDriftCandidate("aws_s3_bucket.clean", "added_in_state"),
		},
	}

	result, err := writer.WriteTerraformConfigStateDriftFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteTerraformConfigStateDriftFindings() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 2; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}

	rows := decodeBatchedVersionedFactCalls(t, db.execs[:1])
	if got, want := len(rows), 2; got != want {
		t.Fatalf("decoded rows = %d, want %d", got, want)
	}

	var derivedRow, exactRow reducerderivedv1.TerraformConfigStateDriftFinding
	if err := json.Unmarshal([]byte(rows[0].Payload), &derivedRow); err != nil {
		t.Fatalf("row 0 unmarshal payload: %v", err)
	}
	if err := json.Unmarshal([]byte(rows[1].Payload), &exactRow); err != nil {
		t.Fatalf("row 1 unmarshal payload: %v", err)
	}

	if got, want := derivedRow.Outcome, "derived"; got != want {
		t.Fatalf("module-resolution row Outcome = %q, want %q", got, want)
	}
	if got, want := exactRow.Outcome, "exact"; got != want {
		t.Fatalf("clean row Outcome = %q, want %q (a candidate with no module-resolution atom must stay exact)", got, want)
	}

	foundReason := false
	for _, atom := range derivedRow.Evidence {
		if atom["evidence_type"] == "terraform_module_resolution_confidence" && atom["value"] == "external_registry" {
			foundReason = true
		}
	}
	if !foundReason {
		t.Fatalf("derived row Evidence = %v, want an atom carrying the external_registry reason", derivedRow.Evidence)
	}
}
