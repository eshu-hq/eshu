// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"testing"
)

// TestIfaFamilyRegistryWaitKeyIsKnownDomain proves each family's
// IFA_FAMILY_WAIT_KEY row -- parsed live from
// scripts/lib/ifa_family_registry/rows/ by parseIfaFamilyRegistryWaitKeys,
// never a Go-side copy of it -- names a real reducer Domain constant, checked
// against knownDomains through the same Domain.Validate() production code
// path ParseDomain uses, never a hand-copied constant list. Each row is
// otherwise guarded only by a hand-typed pin in the fault-injection wait
// helper comparing one hand-typed string to another: both sides can rename
// together and still agree on a value neither of them is real. That drift
// would surface only as a live Docker shard timing out in
// ifa_fault_wait_for_claimed after tens of minutes -- loud, but enormously
// expensive for something this test catches in milliseconds.
func TestIfaFamilyRegistryWaitKeyIsKnownDomain(t *testing.T) {
	t.Parallel()

	rowsDir := ifaFamilyRegistryRowsDir(t)
	waitKeys := parseIfaFamilyRegistryWaitKeys(t, rowsDir)
	waitStages := parseIfaFamilyRegistryWaitStages(t, rowsDir)
	if len(waitKeys) == 0 {
		t.Fatal("parsed zero IFA_FAMILY_WAIT_KEY rows -- registry format changed or the rows were emptied")
	}
	for family, raw := range waitKeys {
		family, raw := family, raw
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			// knownDomains is the fact_work_items keyspace, so this check only
			// applies to handler-stage rows. A runner-stage wait_key is a
			// projection domain from allProjectionDomains, a DISJOINT keyspace
			// -- validating it here would reject a correct row for the wrong
			// reason the moment the first runner-stage family lands, which is
			// the explicit purpose of the enabler this test ships with.
			// TestIfaFamilyRegistryWaitStageAndKeyCohere owns the runner half.
			// A MISSING wait_stage must not take the runner-stage exit. Skipping
			// on absence would leave such a row validated by nothing: it would
			// be waved through here AND absent from
			// TestIfaFamilyRegistryWaitStageAndKeyCohere, which ranges over
			// waitStages. Unreachable while each row file declares exactly one
			// family -- nothing enforces that -- so fail loudly rather than
			// rely on a property no gate asserts.
			stage, ok := waitStages[family]
			if !ok {
				t.Fatalf("family %q has an IFA_FAMILY_WAIT_KEY but no IFA_FAMILY_WAIT_STAGE row; it would then be checked by neither this test nor TestIfaFamilyRegistryWaitStageAndKeyCohere", family)
			}
			if stage != "handler" {
				t.Skipf("family %q declares wait_stage=%q; its wait_key lives in allProjectionDomains, not knownDomains, and is checked by TestIfaFamilyRegistryWaitStageAndKeyCohere", family, stage)
			}
			if err := Domain(raw).Validate(); err != nil {
				t.Fatalf("family %q: IFA_FAMILY_WAIT_KEY=%q is not a real reducer Domain constant (%v) -- scripts/lib/ifa_family_registry.sh's row and the fault-injection wait helper's hand-typed pin could rename together and still agree on a dead string here", family, raw, err)
			}
		})
	}
}

// TestIfaFamilyRegistryWaitStageAndKeyCohere proves each family's declared
// (wait_stage, wait_key) pair names a queue that exists and a key that queue
// can actually contain, and that shared_intent_lock only ever pairs with the
// stage whose proof it depends on.
//
// The three rules, and the failure each one prevents:
//
//   - wait_stage=handler => wait_key must validate as a fact_work_items claim
//     Domain. That is what ifa_fault_wait_for_claimed polls; a key that is not
//     a real Domain waits for a row that can never appear and the cell dies on
//     a timeout tens of minutes into a live shard.
//   - wait_stage=runner => wait_key must be a member of allProjectionDomains.
//     That is what the runner-stage predicate polls; the two keyspaces are
//     disjoint, and a projection domain in a handler row (or the reverse) is
//     the same never-appears failure wearing the other queue's name.
//   - blocker_kind=shared_intent_lock => wait_stage MUST be handler. The
//     mandatory retry-above-baseline proof dereferences
//     fact_work_items.attempt_count scoped to wait_key, and
//     shared_projection_intents has no attempt_count column at all, so a
//     runner wait_key can never satisfy it. The cell would fail claiming the
//     family never retried, which is a statement about the reducer rather than
//     about the row that is actually wrong.
//
// TestIfaFamilyRegistryWaitKeyIsKnownDomain validated EVERY row against
// knownDomains regardless of stage. That was correct while every registered
// family was handler-stage and becomes wrong the moment a runner-stage family
// lands -- it would reject a correct row for the wrong reason. That test is
// gated on wait_stage == handler in this same change so the deference is real
// rather than asserted here; it keeps the handler half, this test owns the
// runner half and the blocker/stage pairing.
func TestIfaFamilyRegistryWaitStageAndKeyCohere(t *testing.T) {
	t.Parallel()

	rowsDir := ifaFamilyRegistryRowsDir(t)
	waitKeys := parseIfaFamilyRegistryWaitKeys(t, rowsDir)
	waitStages := parseIfaFamilyRegistryWaitStages(t, rowsDir)
	blockerKinds := parseIfaFamilyRegistryBlockerKinds(t, rowsDir)
	if len(waitKeys) == 0 || len(waitStages) == 0 {
		t.Fatal("parsed zero wait_key or wait_stage rows -- registry format changed or the rows were emptied")
	}

	projectionDomains := make(map[Domain]struct{}, len(allProjectionDomains))
	for _, d := range allProjectionDomains {
		projectionDomains[d] = struct{}{}
	}

	for family, stage := range waitStages {
		family, stage := family, stage
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			raw, ok := waitKeys[family]
			if !ok {
				t.Fatalf("family %q declares IFA_FAMILY_WAIT_STAGE=%q but no IFA_FAMILY_WAIT_KEY -- a stage with no key polls nothing", family, stage)
			}
			switch stage {
			case "handler":
				if err := Domain(raw).Validate(); err != nil {
					t.Fatalf("family %q: wait_stage=handler but wait_key %q is not a real reducer Domain (%v) -- ifa_fault_wait_for_claimed would poll fact_work_items for a domain that never appears", family, raw, err)
				}
			case "runner":
				if _, ok := projectionDomains[Domain(raw)]; !ok {
					t.Fatalf("family %q: wait_stage=runner but wait_key %q is not in allProjectionDomains -- the runner predicate polls shared_projection_intents.projection_domain, a disjoint keyspace from fact_work_items.domain", family, raw)
				}
			default:
				t.Fatalf("family %q declares IFA_FAMILY_WAIT_STAGE=%q, which is neither handler nor runner", family, stage)
			}
			if blockerKinds[family] == "shared_intent_lock" && stage != "handler" {
				t.Fatalf("family %q declares blocker_kind=shared_intent_lock with wait_stage=%q -- that blocker's mandatory retry-above-baseline proof reads fact_work_items.attempt_count scoped to wait_key, and shared_projection_intents has no attempt_count, so the proof could never pass and the cell would fail blaming the reducer", family, stage)
			}
		})
	}
}

// TestIfaFamilyRegistryHandlerWaitKeysAreExclusive proves no two handler-stage
// families claim the same wait_key.
//
// The failure this prevents is not false evidence, which is why it is worth
// stating precisely. Two families sharing one handler share one intent-write
// call, so killing that handler mid-write genuinely interrupts both -- the
// evidence is real. What is not real is the ACCOUNTING: the registry would
// claim two per-family kill proofs from one mechanism run, while the second
// family's own surface, the one that distinguishes it from the first, stays
// unexercised. That is a check agreeing with itself, and uniqueness makes it
// unrepresentable rather than merely discouraged.
//
// The tie-break when two families genuinely contest one handler domain is that
// the handler's namesake owns the handler-stage wait and every other family
// must prove its own seam. This test never applies that rule automatically --
// it fails naming both rows and a human decides, because auto-resolving would
// silently pick a winner and re-create the accounting error it exists to stop.
func TestIfaFamilyRegistryHandlerWaitKeysAreExclusive(t *testing.T) {
	t.Parallel()

	rowsDir := ifaFamilyRegistryRowsDir(t)
	waitKeys := parseIfaFamilyRegistryWaitKeys(t, rowsDir)
	waitStages := parseIfaFamilyRegistryWaitStages(t, rowsDir)

	owner := make(map[string]string, len(waitKeys))
	families := make([]string, 0, len(waitStages))
	for family := range waitStages {
		families = append(families, family)
	}
	sort.Strings(families)

	checked := 0
	for _, family := range families {
		if waitStages[family] != "handler" {
			continue
		}
		key := waitKeys[family]
		if key == "" {
			continue
		}
		if prior, clash := owner[key]; clash {
			t.Fatalf("families %q and %q both declare wait_stage=handler with wait_key=%q -- one kill of that handler cannot be counted as a per-family proof for both. The handler's namesake owns the handler-stage wait; the other family must prove its own seam (a runner-stage wait on its own projection domain, or a blocker that engages a write only it performs).", prior, family, key)
		}
		owner[key] = family
		checked++
	}
	if checked == 0 {
		t.Fatal("no handler-stage family was checked for wait_key exclusivity -- the parse produced no handler rows, so this test proved nothing")
	}
}
