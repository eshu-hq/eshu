// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"strings"
	"testing"
)

// The supports write-on-change guard ignores the two per-pass payload keys, intent_id and
// cause, on both sides of its payload compare (#10880 B3a, arbiter ruling 2026-11-24).
// An identical replay carries a new intent id and cause, so the verbatim compare rewrote
// every row (10,000 of 10,000 in the lab). These tests pin that the statement differs from
// the pre-B3a (C1) statement ONLY by that two-key strip on both sides, so the supports
// contract version 1 is re-pinned, not bumped.

// supportsContractVersion1C1Pin is the version-1 contract hash at the C1 head (the pin
// before B3a): the shipped statements rendered without the guard strip hash to it.
const supportsContractVersion1C1Pin = "691ac3c4f55c30599005a87ae4db68165228f7f9a3da3ba9e49f87534dde8eca"

// guardStripSides are the two guard terms B3a changes, as the shipped statement renders them.
var guardStripSides = [2]string{
	"(supports.payload - 'intent_id' - 'cause')",
	"(EXCLUDED.payload - 'intent_id' - 'cause')",
}

// guardDifference reports why shipped is not the c1 statement plus exactly the two-key
// strip on both sides of the payload compare; nil means the differential holds. It undoes
// the strip on the shipped text and requires the result to equal c1 byte for byte, so a
// stripped extra key, a one-sided strip, or any other edit leaves a difference.
func guardDifference(shipped, c1 string) error {
	restored := shipped
	for i, side := range guardStripSides {
		raw := "supports.payload"
		if i == 1 {
			raw = "EXCLUDED.payload"
		}
		if strings.Count(restored, side) != 1 {
			return fmt.Errorf("the shipped statement does not carry the guard strip %q exactly once", side)
		}
		restored = strings.Replace(restored, side, raw, 1)
	}
	if restored != c1 {
		return fmt.Errorf("the shipped statement differs from the C1 statement by more than the two-key strip on both sides of the payload compare")
	}
	return nil
}

func TestSupportsGuardDiffersFromC1OnlyByTheTwoKeyStrip(t *testing.T) {
	t.Parallel()
	selectSQL := supplyChainImpactSupportsSelectSQL("fact.fact_id = ANY($1::text[])")
	shipped := upsertSupplyChainImpactSupportsSQL
	c1 := supplyChainImpactUpsertSupportsStatementGuarded(selectSQL, supplyChainImpactSupportFillOnlyColumns, nil)
	if err := guardDifference(shipped, c1); err != nil {
		t.Fatal(err)
	}
	if shipped == c1 {
		t.Fatal("the shipped upsert equals the C1 upsert: the guard strip is not in it")
	}
	// The stored payload stays verbatim: the strip is only in the compare tuples, never in the
	// SELECT list or the SET list.
	if strings.Contains(strings.Split(shipped, "ON CONFLICT")[0], " - 'intent_id'") {
		t.Fatal("the strip reached the selected or inserted payload: the stored payload would no longer be verbatim")
	}
	if !strings.Contains(shipped, "payload = EXCLUDED.payload,") && !strings.Contains(shipped, "payload = EXCLUDED.payload\n") {
		t.Fatal("the upsert no longer sets the payload from the incoming row verbatim")
	}
	// Non-vacuity: the compare still names every other derived column, and the keys that
	// carry real change stay compared.
	for _, kept := range []string{"supports.canonical_key", "supports.priority_score", "supports.detection_profile"} {
		if !strings.Contains(shipped, kept) {
			t.Errorf("the guard no longer compares %s", kept)
		}
	}
	for _, key := range []string{"evidence_fact_ids", "generation_id", "content_digest", "provenance"} {
		if strings.Contains(shipped, "- '"+key+"'") {
			t.Errorf("the guard strips %q: that key carries real change and must stay compared", key)
		}
	}
}

// TestSupportsGuardDifferentialRejectsOtherEdits is the seeded violation: statements that
// strip another key, strip only one side, or strip nothing must fail the differential.
func TestSupportsGuardDifferentialRejectsOtherEdits(t *testing.T) {
	t.Parallel()
	selectSQL := supplyChainImpactSupportsSelectSQL("fact.fact_id = ANY($1::text[])")
	c1 := supplyChainImpactUpsertSupportsStatementGuarded(selectSQL, supplyChainImpactSupportFillOnlyColumns, nil)
	shipped := upsertSupplyChainImpactSupportsSQL
	for name, bad := range map[string]string{
		"an extra stripped key (generation_id)": supplyChainImpactUpsertSupportsStatementGuarded(
			selectSQL, supplyChainImpactSupportFillOnlyColumns, []string{"intent_id", "cause", "generation_id"}),
		"evidence_fact_ids stripped as well": supplyChainImpactUpsertSupportsStatementGuarded(
			selectSQL, supplyChainImpactSupportFillOnlyColumns, []string{"intent_id", "cause", "evidence_fact_ids"}),
		"only intent_id stripped": supplyChainImpactUpsertSupportsStatementGuarded(
			selectSQL, supplyChainImpactSupportFillOnlyColumns, []string{"intent_id"}),
		"only the stored side stripped":   strings.Replace(shipped, guardStripSides[1], "EXCLUDED.payload", 1),
		"only the incoming side stripped": strings.Replace(shipped, guardStripSides[0], "supports.payload", 1),
		"no strip":                        c1,
	} {
		if err := guardDifference(bad, c1); err == nil {
			t.Errorf("%s: the differential accepted it", name)
		}
	}
}

// TestSupportsGuardDoesNotChangeTheVersion1Statements: rendering the shipped statements with
// the guard strip removed hashes to the C1 pin; the shipped ones hash to the new pin. A bump
// would run the backfill again behind a 503; this changes the guard only, so the pin moves
// and the version does not.
func TestSupportsGuardDoesNotChangeTheVersion1Statements(t *testing.T) {
	t.Parallel()
	statements := supplyChainImpactSupportsContractStatements()
	// The C1-era fill-only list: this test's subject is the guard strip, and
	// the key_presence fill-only column (#9975 round 6) rides outside it.
	c1Upsert := supplyChainImpactUpsertSupportsStatementGuarded(
		supplyChainImpactSupportsSelectSQLWith("fact.fact_id = ANY($1::text[])", stableKeyOnlyFillColumns()), stableKeyOnlyFillColumns(), nil)
	c1 := append([]string{c1Upsert}, statements[1:6]...)
	if got := supplyChainImpactSupportsContractHashOf(c1); got != supportsContractVersion1C1Pin {
		t.Fatalf("the statements without the guard strip hash to %s, want the C1 version-1 pin %s: B3a changed more than the guard", got, supportsContractVersion1C1Pin)
	}
	if got := supplyChainImpactSupportsContractHashOf(statements); got == supportsContractVersion1C1Pin {
		t.Fatal("the shipped statements still hash to the C1 pin: the pin was not re-derived")
	}
	if SupplyChainImpactSupportsContractVersion != 1 {
		t.Fatalf("the supports contract version is %d, want 1: B3a re-pins, it does not bump", SupplyChainImpactSupportsContractVersion)
	}
}
