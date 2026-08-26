// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// checkSharedIntentLockRoutedDomainIsUnique fails, naming both families, when
// two families in materializedEdgeFamilyBlockerExpectations share the same
// routedDomain and BOTH declare blocker_kind=shared_intent_lock.
//
// This is deliberately NOT a duplicate of
// TestIfaFamilyRegistryHandlerWaitKeysAreExclusive
// (ifa_family_registry_wait_key_coherence_test.go). That test catches
// two handler-stage families declaring the SAME wait_key -- an accounting
// error visible directly from the registry's own self-reported strings. This
// check closes the evasion path between the two: blocker_kind=shared_intent_lock
// always locks the single shared_projection_intents table
// (ifa_fault_start_shared_intent_lock, ifa_fault_injection_common.sh:210-247)
// regardless of which wait_key the family claims to prove against, so a
// sibling family sharing the SAME routed domain (and therefore the same
// underlying handler and the same IntentWriter.UpsertIntents call) can
// declare a DIFFERENT, valid, non-colliding wait_key and sail past the
// wait_key-exclusivity check while its shared_intent_lock cell still locks
// the exact table out from under the exact handler another family's cell
// already covers -- double-counting one interruption as two families' proofs
// under different names. routedDomain here is ground truth taken from the Go
// production wiring (materializedEdgeFamilyBlockerExpectations, populated by
// reflecting on implementedDefaultDomainDefinitions -- see that map's own doc
// comment), never from either family's self-reported wait_key, so a family
// that lies about its wait_key cannot evade this check the way it evades
// wait_key string equality.
//
// Scoped to materializedEdgeFamilyBlockerExpectations only (the 8 families
// with a single reflectable routed handler), the same scope
// checkFamilyBlockerLockstep itself uses -- a family in
// materializedEdgeFamilyBlockerLockstepExclusions (e.g. repo_dependency, with
// three separate producer handlers) has no single well-defined routedDomain
// for this test's reflection to compare, for the same reason it is excluded
// from checkFamilyBlockerLockstep.
func checkSharedIntentLockRoutedDomainIsUnique(expectations map[string]familyBlockerExpectation, declaredBlockerKinds map[string]string) error {
	owner := make(map[Domain]string, len(expectations))
	families := make([]string, 0, len(expectations))
	for family := range expectations {
		families = append(families, family)
	}
	sort.Strings(families)

	for _, family := range families {
		raw, hasRow := declaredBlockerKinds[family]
		if !hasRow {
			continue
		}
		kind, known := classifyBlockerKind(raw)
		if !known || kind != blockerSharedIntentLock {
			continue
		}

		routedDomain := expectations[family].routedDomain
		if prior, clash := owner[routedDomain]; clash {
			return fmt.Errorf("families %q and %q share routed domain %q and BOTH declare blocker_kind=shared_intent_lock -- locking shared_projection_intents genuinely intercepts whichever family's handler writes through that routed domain, so both families' proofs are the same one interruption counted twice under different names; only one of them is real evidence, the other must prove its own seam", prior, family, routedDomain)
		}
		owner[routedDomain] = family
	}
	return nil
}

// TestSharedIntentLockRoutedDomainIsUniqueCatchesCollision is the
// deliberate-break tooth: it proves checkSharedIntentLockRoutedDomainIsUnique
// itself fires, and names both families, on a synthetic pair sharing one
// routedDomain with DIFFERENT (non-colliding) wait_keys -- the exact evasion
// path TestIfaFamilyRegistryHandlerWaitKeysAreExclusive cannot see, since
// that test only compares wait_key strings for equality. Independent of file
// I/O and of materializedEdgeFamilyBlockerExpectations' real contents, so it
// stays fast, deterministic, and exercised on every run regardless of
// whether the real registry ever actually collides.
func TestSharedIntentLockRoutedDomainIsUniqueCatchesCollision(t *testing.T) {
	t.Parallel()

	synthetic := map[string]familyBlockerExpectation{
		"family_a": {routedDomain: DomainCodeCallMaterialization},
		"family_b": {routedDomain: DomainCodeCallMaterialization}, // same routed domain, different name
	}
	declared := map[string]string{
		"family_a": "shared_intent_lock",
		"family_b": "shared_intent_lock",
	}

	err := checkSharedIntentLockRoutedDomainIsUnique(synthetic, declared)
	if err == nil {
		t.Fatal("checkSharedIntentLockRoutedDomainIsUnique(two families sharing a routed domain, both shared_intent_lock) = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "family_a") || !strings.Contains(err.Error(), "family_b") {
		t.Errorf("error %q does not name both colliding families", err.Error())
	}
}

// TestSharedIntentLockRoutedDomainIsUniqueIgnoresDifferentDomains proves the
// check does not fire on two shared_intent_lock families that genuinely
// route through different domains -- the ordinary, correct shape every
// covered family has today. Guards against an over-broad implementation that
// flags any two shared_intent_lock families regardless of routedDomain,
// which would make the check indistinguishable from (and strictly worse
// than) simply banning a second shared_intent_lock declaration entirely.
func TestSharedIntentLockRoutedDomainIsUniqueIgnoresDifferentDomains(t *testing.T) {
	t.Parallel()

	synthetic := map[string]familyBlockerExpectation{
		"family_a": {routedDomain: DomainCodeCallMaterialization},
		"family_b": {routedDomain: DomainShellExecMaterialization},
	}
	declared := map[string]string{
		"family_a": "shared_intent_lock",
		"family_b": "shared_intent_lock",
	}

	if err := checkSharedIntentLockRoutedDomainIsUnique(synthetic, declared); err != nil {
		t.Fatalf("checkSharedIntentLockRoutedDomainIsUnique(two families with distinct routed domains) = %v, want nil", err)
	}
}

// TestSharedIntentLockRoutedDomainIsUnique runs
// checkSharedIntentLockRoutedDomainIsUnique against the real production
// routedDomain wiring (materializedEdgeFamilyBlockerExpectations) and the
// real, live-parsed registry blocker-kind rows. Passes vacuously today: no
// two of the 8 covered families share a routedDomain (each maps to a
// distinct DomainXxxMaterialization / DomainXxx constant -- see
// materializedEdgeFamilyBlockerExpectations), so this is strictly
// strengthening rather than a fix for a live collision.
func TestSharedIntentLockRoutedDomainIsUnique(t *testing.T) {
	t.Parallel()

	rowsDir := ifaFamilyRegistryRowsDir(t)
	declared := parseIfaFamilyRegistryBlockerKinds(t, rowsDir)

	if err := checkSharedIntentLockRoutedDomainIsUnique(materializedEdgeFamilyBlockerExpectations, declared); err != nil {
		t.Fatal(err)
	}
}
