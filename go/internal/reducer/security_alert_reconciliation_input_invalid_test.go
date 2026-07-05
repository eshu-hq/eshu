// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// securityAlertEnvelopeMissingRepositoryID builds a
// security_alert.repository_alert envelope whose payload deliberately omits the
// required repository_id identity anchor, so the typed decode seam dead-letters
// it as input_invalid. It intentionally does NOT route through
// securityAlertEnvelope, which always stamps repository_id.
func securityAlertEnvelopeMissingRepositoryID(factID string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          "security-alert:github:acme/api",
		GenerationID:     "generation-1",
		FactKind:         facts.SecurityAlertRepositoryAlertFactKind,
		SchemaVersion:    facts.SecurityAlertSchemaVersionV1,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC),
		Payload:          payload,
	}
}

// TestBuildSecurityAlertReconciliationsQuarantinesMissingRepositoryID is the
// flagship Wave 4e regression: a security_alert.repository_alert fact missing
// its required repository_id identity anchor dead-letters as a per-fact
// input_invalid quarantine on the RECONCILIATION path, while a valid sibling
// alert in the same batch still produces its reconciliation decision and NO
// empty-repository reconciliation decision is produced for the malformed fact.
//
// Before the typed decode, extractProviderSecurityAlerts read repository_id via
// payloadStr, which returned "" for the absent key and produced a
// blank-repository provider_only decision with no operator signal. This test
// fails against that pre-typing behavior (quarantine count 0, two decisions)
// and passes after: one quarantine naming repository_id, one decision for the
// valid sibling only.
func TestBuildSecurityAlertReconciliationsQuarantinesMissingRepositoryID(t *testing.T) {
	t.Parallel()

	validRepoID := "repo://github/eshu-hq/eshu"
	validPackageID := "npm://registry.npmjs.org/left-pad"
	valid := securityAlertEnvelope("alert-valid", validRepoID, map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(7),
		"provider_state":        "open",
		"package_id":            validPackageID,
		"package_name":          "left-pad",
		"ecosystem":             "npm",
		"manifest_path":         "package-lock.json",
		"cve_ids":               []string{"CVE-2026-0002"},
		"ghsa_ids":              []string{"GHSA-valid-0002"},
	})
	malformed := securityAlertEnvelopeMissingRepositoryID("alert-malformed", map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(9),
		"provider_state":        "open",
		"package_id":            "npm://registry.npmjs.org/other-pkg",
		"package_name":          "other-pkg",
		"ecosystem":             "npm",
		"cve_ids":               []string{"CVE-2026-0003"},
	})

	decisions, quarantined, err := BuildSecurityAlertReconciliationsWithQuarantine(
		[]facts.Envelope{valid, malformed},
	)
	if err != nil {
		t.Fatalf("BuildSecurityAlertReconciliationsWithQuarantine() error = %v, want nil", err)
	}

	if got, want := len(quarantined), 1; got != want {
		t.Fatalf("quarantined count = %d, want %d", got, want)
	}
	q := quarantined[0]
	if q.factID != "alert-malformed" {
		t.Fatalf("quarantined factID = %q, want alert-malformed", q.factID)
	}
	if q.factKind != facts.SecurityAlertRepositoryAlertFactKind {
		t.Fatalf("quarantined factKind = %q, want %q", q.factKind, facts.SecurityAlertRepositoryAlertFactKind)
	}
	if q.field != "repository_id" {
		t.Fatalf("quarantined field = %q, want repository_id", q.field)
	}

	// Exactly one decision — the valid sibling — and no empty-repository
	// decision for the malformed fact.
	if got, want := len(decisions), 1; got != want {
		t.Fatalf("decisions count = %d, want %d (valid sibling only, no empty-identity row)", got, want)
	}
	if got := decisions[0].RepositoryID; got != validRepoID {
		t.Fatalf("decision RepositoryID = %q, want %q", got, validRepoID)
	}
	for _, decision := range decisions {
		if decision.ProviderAlertFactID == "alert-malformed" {
			t.Fatalf("a reconciliation decision was produced for the malformed fact; it must be quarantined, not decided")
		}
		if decision.RepositoryID == "" {
			t.Fatal("a reconciliation decision has an empty RepositoryID; the malformed fact must not produce a blank-repository row")
		}
	}
}

// TestSecurityAlertReconciliationHandlerQuarantinesMissingRepositoryID proves the
// per-fact isolation end to end through the handler: a malformed alert is
// recorded on Result.SubSignals["input_invalid_facts"], the valid sibling's
// decision is still written, and the intent still succeeds.
func TestSecurityAlertReconciliationHandlerQuarantinesMissingRepositoryID(t *testing.T) {
	t.Parallel()

	validRepoID := "repo://github/eshu-hq/eshu"
	loader := &recordingSecurityAlertReconciliationFactLoader{
		scopeFacts: []facts.Envelope{
			securityAlertEnvelope("alert-valid", validRepoID, map[string]any{
				"provider":              "github_dependabot",
				"provider_alert_number": int64(7),
				"provider_state":        "open",
				"package_id":            "npm://registry.npmjs.org/left-pad",
				"package_name":          "left-pad",
				"ecosystem":             "npm",
				"cve_ids":               []string{"CVE-2026-0002"},
			}),
			securityAlertEnvelopeMissingRepositoryID("alert-malformed", map[string]any{
				"provider":              "github_dependabot",
				"provider_alert_number": int64(9),
				"provider_state":        "open",
				"package_id":            "npm://registry.npmjs.org/other-pkg",
				"package_name":          "other-pkg",
				"ecosystem":             "npm",
			}),
		},
	}
	writer := &recordingSecurityAlertReconciliationWriter{}
	handler := SecurityAlertReconciliationHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-quarantine",
		ScopeID:      validRepoID,
		GenerationID: "generation-1",
		SourceSystem: "security_alert",
		Domain:       DomainSecurityAlertReconciliation,
		Cause:        "provider alert observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Handle() status = %q, want succeeded", result.Status)
	}
	if got, want := result.SubSignals["input_invalid_facts"], float64(1); got != want {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want %v", got, want)
	}
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1 (valid sibling still written)", writer.calls)
	}
	for _, decision := range writer.write.Decisions {
		if decision.ProviderAlertFactID == "alert-malformed" || decision.RepositoryID == "" {
			t.Fatalf("writer received a decision for the malformed/blank-repository fact: %+v", decision)
		}
	}
	if len(writer.write.Decisions) != 1 {
		t.Fatalf("writer decisions = %d, want 1 (valid sibling only)", len(writer.write.Decisions))
	}
}

// TestNormalizeSecurityAlertStringMapInPlace locks the accept / trim / drop /
// re-key contract of the in-place string-map normalizer that reproduces the
// pre-typing securityAlertStringMap output at one allocation. A padded key is
// re-inserted under its trimmed form after the range (never during it), an empty
// key or value is dropped, and an all-empty map normalizes to nil.
func TestNormalizeSecurityAlertStringMapInPlace(t *testing.T) {
	t.Parallel()

	t.Run("trims values and drops empty entries", func(t *testing.T) {
		t.Parallel()
		got := normalizeSecurityAlertStringMapInPlace(map[string]string{
			"percentage": " 0.0123 ",
			"percentile": "",
			"":           "orphan",
		})
		want := map[string]string{"percentage": "0.0123"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("normalized = %v, want %v", got, want)
		}
	})

	t.Run("re-keys a padded key after the range", func(t *testing.T) {
		t.Parallel()
		got := normalizeSecurityAlertStringMapInPlace(map[string]string{
			"  cwe_id  ": "  CWE-400  ",
		})
		want := map[string]string{"cwe_id": "CWE-400"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("normalized = %v, want %v", got, want)
		}
	})

	t.Run("all-empty map normalizes to nil", func(t *testing.T) {
		t.Parallel()
		if got := normalizeSecurityAlertStringMapInPlace(map[string]string{"a": "", "": "b"}); got != nil {
			t.Fatalf("normalized = %v, want nil", got)
		}
		if got := normalizeSecurityAlertStringMapInPlace(nil); got != nil {
			t.Fatalf("normalized(nil) = %v, want nil", got)
		}
	})
}
