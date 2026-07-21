// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// kindConsumerGateRepoRoot resolves the repository root from this test
// file's location (mcp -> internal -> go -> repo root), the same pattern
// readSurfaceGateSpecsDir uses for the specs/ directory.
func kindConsumerGateRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// TestEveryRegistryKindHasConsumerOrDisclosure is the #5474 D2 per-kind
// consumer existence gate. It walks every fact kind in the generated registry
// (facts.FactKindRegistry()) and asserts that each kind either has a
// detectable REAL consumer (a decode<Kind> seam call site, a direct
// factschema.Decode<Kind> call, a literal fact_kind SQL predicate or
// facts.<Kind> identifier reference in the query layer, or a reducer-level
// case/equality dispatch — see kind_real_consumer.go) or an explicit entry
// in the grandfatheredUnconsumedKinds disclosure ledger.
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

	real, err := loadRealConsumerEvidence(kindConsumerGateRepoRoot(t))
	if err != nil {
		t.Fatalf("loadRealConsumerEvidence: %v", err)
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
		ok, reason := resolveKindConsumer(evidence, real)
		if !ok {
			t.Errorf("%s", reason)
			failures++
		}
	}

	if failures > 0 {
		t.Logf("%d of %d kinds have no detectable consumer and are not disclosed", failures, len(entries))
	}
}

// TestKindConsumerExistenceBITES_TeethProof is the #5474 D2 BITES proof. It
// seeds every case from the PRODUCTION fact-kind registry
// (facts.FactKindRegistryEntryFor), never a hand-rolled struct, and proves
// the gate has teeth on two RED cases the pre-fix toothless signal
// (PayloadSchema non-empty, or ReducerDomain+ProjectionHook+AdmissionHook
// all non-empty) would have missed.
func TestKindConsumerExistenceBITES_TeethProof(t *testing.T) {
	t.Parallel()

	repoRoot := kindConsumerGateRepoRoot(t)
	real, err := loadRealConsumerEvidence(repoRoot)
	if err != nil {
		t.Fatalf("loadRealConsumerEvidence: %v", err)
	}

	// Teeth case 1: disclosures are load-bearing. A disclosed-unconsumed
	// kind that gains a real consumer must flip the gate RED, not stay
	// silently green forever. Seed with the real, production
	// terraform_state_candidate entry — currently and correctly disclosed —
	// and simulate the moment a consumer lands for it without the
	// disclosure being removed.
	t.Run("disclosed_kind_with_real_consumer_is_red", func(t *testing.T) {
		entry, ok := facts.FactKindRegistryEntryFor("terraform_state_candidate")
		if !ok {
			t.Fatal("production registry has no entry for terraform_state_candidate")
		}
		if !isKindDisclosed(entry.Kind) {
			t.Fatal("test premise broken: terraform_state_candidate must be disclosed in production")
		}

		// RED: disclosed (true, production state) AND a real consumer is
		// present (simulated — this kind has none today, but the check must
		// fire the moment one lands while the stale disclosure remains).
		okResult, reason := classifyKindConsumer(entry.Kind, entry.ReducerDomain, true /* hasConsumer */, true /* disclosed */, entry.AdmissionExempt)
		if okResult {
			t.Fatalf("BITES FAILED: a disclosed kind with a real consumer must be RED (stale disclosure), got GREEN")
		}
		if !substrIn(reason, "stale") || !substrIn(reason, "grandfatheredUnconsumedKinds") {
			t.Errorf("contradiction RED message doesn't name the stale-disclosure fix: %s", reason)
		}

		// GREEN: the disclosure is removed once the real consumer lands —
		// no contradiction, passes via the real consumer alone.
		okResult2, _ := classifyKindConsumer(entry.Kind, entry.ReducerDomain, true, false, entry.AdmissionExempt)
		if !okResult2 {
			t.Fatalf("a kind with a real consumer and no disclosure must pass")
		}

		// Honest production steady-state: today this kind has NO real
		// consumer (proven by the toothless-signal case below) and IS
		// disclosed — passes via disclosure alone, no contradiction.
		prodHasConsumer := kindHasConsumer(factKindRegistryConsumerEvidence{
			Kind: entry.Kind, ReducerDomain: entry.ReducerDomain, PayloadSchema: entry.PayloadSchema,
			AdmissionExempt: entry.AdmissionExempt, ProjectionHook: entry.ProjectionHook, AdmissionHook: entry.AdmissionHook,
		}, real)
		if prodHasConsumer {
			t.Fatalf("terraform_state_candidate must have no real consumer in production today — real-consumer signal or disclosure ledger has drifted")
		}
		okProd, _ := classifyKindConsumer(entry.Kind, entry.ReducerDomain, prodHasConsumer, isKindDisclosed(entry.Kind), entry.AdmissionExempt)
		if !okProd {
			t.Fatalf("production terraform_state_candidate should pass via its real, non-contradictory disclosure")
		}
	})

	// Teeth case 2: the pre-fix toothless signal (PayloadSchema non-empty)
	// passed terraform_state_candidate; the new real-consumer signal must
	// correctly classify it unconsumed absent its disclosure. Seed with the
	// real, production registry entry.
	t.Run("toothless_signal_would_pass_but_real_signal_fails", func(t *testing.T) {
		entry, ok := facts.FactKindRegistryEntryFor("terraform_state_candidate")
		if !ok {
			t.Fatal("production registry has no entry for terraform_state_candidate")
		}

		// The OLD false-green precondition: PayloadSchema is non-empty, so
		// the pre-#5474-fix kindHasConsumer (which returned true whenever
		// PayloadSchema != "") would have wrongly reported this kind
		// consumed.
		if strings.TrimSpace(entry.PayloadSchema) == "" {
			t.Fatal("test premise broken: terraform_state_candidate must have a non-empty PayloadSchema to prove the toothless-signal false-green")
		}

		// The NEW real-consumer signal correctly finds no decode seam, no
		// query-layer SQL/identifier reference, and no reducer dispatch for
		// this kind — go/internal/projector/tfstate_canonical.go:113-116
		// documents it as intentionally unhandled.
		hasRealConsumer := real.hasRealConsumer(entry.Kind)
		if hasRealConsumer {
			t.Fatalf("BITES FAILED: real-consumer signal reports a consumer for terraform_state_candidate — the false-green this gate exists to close has resurfaced")
		}

		// RED: without its disclosure, the correctly-computed "no real
		// consumer" classification must fail the gate — proving the fix has
		// teeth where the old PayloadSchema-only check did not.
		okResult, reason := classifyKindConsumer(entry.Kind, entry.ReducerDomain, hasRealConsumer, false /* disclosed */, entry.AdmissionExempt)
		if okResult {
			t.Fatalf("BITES FAILED: terraform_state_candidate without its disclosure must be RED")
		}
		if !substrIn(reason, "add a consumer") || !substrIn(reason, "grandfatheredUnconsumedKinds") || !substrIn(reason, "remove the kind") {
			t.Errorf("RED message does not name all three legal exits: %s", reason)
		}

		// GREEN: restoring the real, production disclosure passes it.
		okResult2, _ := classifyKindConsumer(entry.Kind, entry.ReducerDomain, hasRealConsumer, true, entry.AdmissionExempt)
		if !okResult2 {
			t.Fatalf("terraform_state_candidate with its real disclosure restored must pass")
		}
	})

	// Steady state: a genuinely consumed production kind (typed decode seam)
	// passes without any disclosure.
	t.Run("genuinely_consumed_kind_passes_without_disclosure", func(t *testing.T) {
		entry, ok := facts.FactKindRegistryEntryFor("aws_resource")
		if !ok {
			t.Fatal("production registry has no entry for aws_resource")
		}
		if isKindDisclosed(entry.Kind) {
			t.Fatal("test premise broken: aws_resource must not be in the disclosure ledger")
		}
		if !real.hasRealConsumer(entry.Kind) {
			t.Fatalf("aws_resource must have a real decode-seam consumer (go/internal/reducer/factschema_decode.go)")
		}
		ok2, _ := resolveKindConsumer(factKindRegistryConsumerEvidence{
			Kind: entry.Kind, ReducerDomain: entry.ReducerDomain, PayloadSchema: entry.PayloadSchema,
			AdmissionExempt: entry.AdmissionExempt, ProjectionHook: entry.ProjectionHook, AdmissionHook: entry.AdmissionHook,
		}, real)
		if !ok2 {
			t.Fatalf("aws_resource should pass via its real decode-seam consumer")
		}
	})
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
// resolveKindConsumer against known production registry entries.
func TestKindConsumerExistenceEdgeCases(t *testing.T) {
	t.Parallel()

	real, err := loadRealConsumerEvidence(kindConsumerGateRepoRoot(t))
	if err != nil {
		t.Fatalf("loadRealConsumerEvidence: %v", err)
	}

	tests := []struct {
		name    string
		kind    string
		wantOK  bool
		wantMsg string // expected substring in failure reason
	}{
		{name: "decode_seam_consumer_passes", kind: "kubernetes_live.pod_template", wantOK: true},
		{name: "admission_exempt_passes", kind: "file", wantOK: true},
		{name: "disclosed_passes", kind: "terraform_state_candidate", wantOK: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry, ok := facts.FactKindRegistryEntryFor(tc.kind)
			if !ok {
				t.Fatalf("production registry has no entry for %q", tc.kind)
			}
			evidence := factKindRegistryConsumerEvidence{
				Kind: entry.Kind, ReducerDomain: entry.ReducerDomain, PayloadSchema: entry.PayloadSchema,
				AdmissionExempt: entry.AdmissionExempt, ProjectionHook: entry.ProjectionHook, AdmissionHook: entry.AdmissionHook,
			}
			okResult, reason := resolveKindConsumer(evidence, real)
			if okResult != tc.wantOK {
				t.Errorf("resolveKindConsumer(%q) = %v, want %v (reason: %s)", tc.kind, okResult, tc.wantOK, reason)
			}
			if tc.wantMsg != "" && !substrIn(strings.ToLower(reason), strings.ToLower(tc.wantMsg)) {
				t.Errorf("expected reason to contain %q, got: %s", tc.wantMsg, reason)
			}
		})
	}

	// The "unconsumed and undisclosed fails" edge case cannot be seeded from
	// a production registry entry (every production entry is either
	// consumed or disclosed, by construction of the gate this test file
	// asserts). classifyKindConsumer's pure signature lets this case be
	// proven without a synthetic factKindRegistryConsumerEvidence: pass
	// hasConsumer=false, disclosed=false directly.
	t.Run("unconsumed_undisclosed_fails", func(t *testing.T) {
		okResult, reason := classifyKindConsumer("totally_made_up_kind", "some_domain", false, false, false)
		if okResult {
			t.Errorf("classifyKindConsumer(hasConsumer=false, disclosed=false) = true, want false")
		}
		if !substrIn(strings.ToLower(reason), "no detectable consumer") {
			t.Errorf("expected reason to contain %q, got: %s", "no detectable consumer", reason)
		}
	})
}
