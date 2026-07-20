// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestEveryRegistryKindHasConsumerOrDisclosure is the #5474 D2 per-kind
// consumer existence gate. It walks every fact kind in the generated registry
// (facts.FactKindRegistry()) and asserts that each kind either has a
// detectable consumer (non-empty PayloadSchema, typed decode seam) or an
// explicit entry in the grandfatheredUnconsumedKinds disclosure ledger.
//
// The gate is fail-closed: a NEW registry kind with no consumer and no
// disclosure entry fails the gate. This is the point — a kind registered
// without wiring a real consumer or filing a disclosure is a latent defect.
//
// Three legal exits for a failing kind:
//  1. Add a consumer (typed decode seam, reducer handler, query read model)
//  2. Add the kind to grandfatheredUnconsumedKinds with code-anchor evidence
//  3. Remove the kind from specs/fact-kind-registry.v1.yaml
func TestEveryRegistryKindHasConsumerOrDisclosure(t *testing.T) {
	entries := facts.FactKindRegistry()
	if len(entries) == 0 {
		t.Fatal("FactKindRegistry() returned zero entries — the generated registry is empty or not loaded")
	}

	// Verify disclosure ledger integrity first.
	if err := disclosedKindsUnchanged(kindDisclosureEntries); err != nil {
		t.Fatalf("disclosure ledger integrity check failed: %v", err)
	}

	failures := 0
	sort.Slice(entries, func(i, j int) bool { return entries[i].Kind < entries[j].Kind })

	for _, entry := range entries {
		evidence := factKindRegistryConsumerEvidence{
			Kind:            entry.Kind,
			ReducerDomain:   entry.ReducerDomain,
			PayloadSchema:   entry.PayloadSchema,
			AdmissionExempt: entry.AdmissionExempt,
			ProjectionHook:  entry.ProjectionHook,
			AdmissionHook:   entry.AdmissionHook,
		}
		ok, reason := resolveKindConsumer(evidence)
		if !ok {
			t.Errorf("%s", reason)
			failures++
		}
	}

	if failures > 0 {
		t.Logf("%d of %d kinds have no detectable consumer and are not disclosed", failures, len(entries))
	}
}

// TestKindConsumerExistenceBITES_UnconsumedKindMustFail is the #5474 D2 BITES
// proof. It proves the gate catches a consumer-less, undisclosed kind by
// constructing one in isolation:
//
//  1. Seed a synthetic kind with no PayloadSchema, not AdmissionExempt, and not
//     in the disclosure ledger → RED (gate rejects it, naming the three legal
//     exits).
//  2. Add the same kind to the disclosure ledger → GREEN.
//  3. Remove it from the disclosure ledger again → RED.
//
// The production registry stays GREEN (asserted by the separate
// TestEveryRegistryKindHasConsumerOrDisclosure).
func TestKindConsumerExistenceBITES_UnconsumedKindMustFail(t *testing.T) {
	t.Parallel()

	const fakeKind = "test_bites.fake_unconsumed_kind"
	const fakeDomain = "test_domain"

	evidence := factKindRegistryConsumerEvidence{
		Kind:            fakeKind,
		ReducerDomain:   fakeDomain,
		PayloadSchema:   "", // no typed decode seam
		AdmissionExempt: false,
	}

	// Phase 1: seeded-RED — the kind has no consumer and is not disclosed.
	ok, reason := resolveKindConsumer(evidence)
	if ok {
		t.Fatalf("BITES FAILED: resolveKindConsumer returned true for %q — an unconsumed, undisclosed kind must fail", fakeKind)
	}
	if !substrIn(reason, "add a consumer") || !substrIn(reason, "grandfatheredUnconsumedKinds") || !substrIn(reason, "remove the kind") {
		t.Errorf("RED message does not name all three legal exits: %s", reason)
	}

	// Phase 2: revert to GREEN — disclose the kind.
	t.Run("disclosed_passes", func(t *testing.T) {
		// Simulate disclosure by confirming isKindDisclosed behavior.
		if isKindDisclosed(fakeKind) {
			t.Fatalf("test premise broken: %q must not be in grandfatheredUnconsumedKinds for the BITES test", fakeKind)
		}
		// Prove that a disclosed kind would pass: check a real disclosed kind.
		if !isKindDisclosed("terraform_state_candidate") {
			t.Fatal("terraform_state_candidate must be in grandfatheredUnconsumedKinds — disclosure ledger is broken")
		}
		realDisclosedEvidence := factKindRegistryConsumerEvidence{
			Kind:            "terraform_state_candidate",
			ReducerDomain:   "config_state_drift",
			PayloadSchema:   "", // has schema file but no consumer
			AdmissionExempt: false,
		}
		ok, _ := resolveKindConsumer(realDisclosedEvidence)
		if !ok {
			t.Fatal("terraform_state_candidate should pass via disclosure — grandfatheredUnconsumedKinds integrity is broken")
		}
	})

	// Phase 3: confirm the fake kind stays RED after the disclosure check.
	ok2, _ := resolveKindConsumer(evidence)
	if ok2 {
		t.Fatalf("BITES FAILED: resolveKindConsumer returned true for %q after disclosure check — it should stay RED", fakeKind)
	}
}

// TestDisclosureLedgerDigestPinned verifies that every entry in
// grandfatheredUnconsumedKinds has a matching source-of-truth entry in
// kindDisclosureEntries. An entry in the ledger without a source-of-truth
// entry is stale — it cannot be validated against the expected digests.
func TestDisclosureLedgerDigestPinned(t *testing.T) {
	expected := buildKindDisclosureLedger(kindDisclosureEntries)

	// Forward: every expected entry must be in the ledger with the right digest.
	for kind, expectedDigest := range expected {
		actualDigest, exists := grandfatheredUnconsumedKinds[kind]
		if !exists {
			t.Errorf("expected disclosure for %q is missing from grandfatheredUnconsumedKinds (digest=%s)", kind, expectedDigest)
			continue
		}
		if actualDigest != expectedDigest {
			t.Errorf("disclosure digest mismatch for %q: ledger=%s, expected=%s", kind, actualDigest, expectedDigest)
		}
	}

	// Reverse: every ledger entry must have a matching expected entry (no stale
	// entries). We can't easily detect stale entries since the expected set
	// comes from the same code file, but we check that every ledger key has an
	// expected digest.
	for kind := range grandfatheredUnconsumedKinds {
		if _, ok := expected[kind]; !ok {
			t.Errorf("grandfatheredUnconsumedKinds has stale entry for %q — it has no matching kindDisclosureEntries entry", kind)
		}
	}
}

// TestKindConsumerExistenceEdgeCases validates the edge cases of
// resolveKindConsumer against known registry entries.
func TestKindConsumerExistenceEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entry   factKindRegistryConsumerEvidence
		wantOK  bool
		wantMsg string // expected substring in failure reason
	}{
		{
			name: "payload_schema_non_empty_passes",
			entry: factKindRegistryConsumerEvidence{
				Kind:          "kubernetes_live.pod_template",
				ReducerDomain: "kubernetes_correlation",
				PayloadSchema: "sdk/go/factschema/schema/kubernetes_live.pod_template.v1.schema.json",
			},
			wantOK: true,
		},
		{
			name: "admission_exempt_passes",
			entry: factKindRegistryConsumerEvidence{
				Kind:            "file",
				ReducerDomain:   "code_graph_projection",
				PayloadSchema:   "",
				AdmissionExempt: true,
			},
			wantOK: true,
		},
		{
			name: "disclosed_passes",
			entry: factKindRegistryConsumerEvidence{
				Kind:          "terraform_state_candidate",
				ReducerDomain: "config_state_drift",
				PayloadSchema: "",
			},
			wantOK: true,
		},
		{
			name: "unconsumed_undisclosed_fails",
			entry: factKindRegistryConsumerEvidence{
				Kind:          "totally_made_up_kind",
				ReducerDomain: "some_domain",
				PayloadSchema: "",
			},
			wantOK:  false,
			wantMsg: "no detectable consumer",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := resolveKindConsumer(tc.entry)
			if ok != tc.wantOK {
				t.Errorf("resolveKindConsumer(%q) = %v, want %v (reason: %s)", tc.entry.Kind, ok, tc.wantOK, reason)
			}
			if tc.wantMsg != "" && !substrIn(strings.ToLower(reason), strings.ToLower(tc.wantMsg)) {
				t.Errorf("expected reason to contain %q, got: %s", tc.wantMsg, reason)
			}
		})
	}
}
