// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// This file holds the ledger-halves machinery split out of
// materialized_edges_direct_family_blindness_test.go, which had grown past the
// 500-line cap CLAUDE.md sets for every file in this repository. The seam is a
// real one and not an arbitrary cut: everything here answers "which HALF of the
// split ledger does a row live in", while the blindness file answers "is this
// family claimed at all". Merging the two back together restores the cap
// violation and loses that distinction.

package materializededges

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// manifestSurfaceKey pulls the family out of a `surface: "materialized_edges:x"`
// line in the committed ledger.
var manifestSurfaceKey = regexp.MustCompile(`surface:\s*"` + regexp.QuoteMeta(MaterializedEdgeSurfacePrefix) + `([^"]+)"`)

// ledgerHalf pairs one file of the split ledger with the enumeration that owns
// the families whose rows belong in it.
//
// One table, read by BOTH the loader below and the half check at the end of
// this file, because two lists of the same thing drift apart quietly. Adding a
// third half to the loader alone would produce rows that no check examined --
// present, parsed, and asserted against nothing, which is the same shape of
// blindness this whole file exists to prevent, one level up. Wired here, a new
// half is read and owned in the same edit or not at all.
//
// It also replaces a `default:` case in the half check. Being precise about
// that branch, because being loose about "this cannot fire" is the error under
// review: it was unreachable for the code as written -- the file names came
// only from the loader's own two constants -- and reachable for exactly one
// future edit, someone adding a third file to the loader and not to the check,
// where it fired a t.Fatalf telling them to come back and finish the job.
//
// So it was neither dead code nor a real contract, and neither keeping nor
// deleting it was right. Keeping it left a branch no run can exercise;
// deleting it left the half-edit silently unchecked. The table removes the
// choice: there is one list, so a third half is read and owned in one edit,
// and there is no second place to forget.
type ledgerHalf struct {
	// File is the manifest's base name under specs/.
	File string
	// Owner enumerates the families whose rows belong in File.
	Owner func() []string
	// OwnerName is the source expression for Owner, so a failure names what a
	// maintainer has to go and read.
	OwnerName string
	// Misplaced is what to tell a maintainer holding a row this half does not
	// own: where it belongs, and why nothing else says so.
	Misplaced string
}

// materializedEdgeLedgerHalves returns the two halves of the claims ledger.
func materializedEdgeLedgerHalves() []ledgerHalf {
	return []ledgerHalf{
		{
			File:      MaterializedEdgeManifestFileName,
			Owner:     reducer.MaterializedEdgeFamilies,
			OwnerName: "reducer.MaterializedEdgeFamilies()",
			Misplaced: "If it is a direct-materialization family that row belongs in " + MaterializedEdgeDirectManifestFileName +
				" -- a row in the wrong half satisfies every union check in this file, and the family having its other rows in the right half hides the misplacement from anything asserting per family",
		},
		{
			File:      MaterializedEdgeDirectManifestFileName,
			Owner:     reducer.DirectMaterializedEdgeFamilies,
			OwnerName: "reducer.DirectMaterializedEdgeFamilies()",
			Misplaced: "If it reaches the graph through the shared-projection intent path that row belongs in " + MaterializedEdgeManifestFileName,
		},
	}
}

// ledgerSurfaceRow is one `surface:` key found in one half of the committed
// claims ledger: the family it names, and the file carrying it.
type ledgerSurfaceRow struct {
	// Family is the key with the "materialized_edges:" prefix stripped.
	Family string
	// File is the base name of the manifest the row was read from.
	File string
}

// loadMaterializedEdgeLedgerRows reads BOTH halves of the committed claims
// ledger as TEXT and returns one row per `surface:` key, from the coverage: and
// waivers: sections alike, in file order and then in the order each file
// declares them.
//
// Text rather than the typed loaders (replaycoverage.LoadManifest /
// LoadMaterializedEdgeWaivers) on purpose: those validate rows against the
// enumerated family set and would reject or drop a surface naming a family the
// enumeration does not return. That is exactly the row this test needs to be
// able to see — a ledger entry for a family the enumeration is blind to would
// be invisible to a typed read, and the orphaned-row check would then pass by
// construction.
//
// Both files, because reading one is the very failure being tested for: a
// family whose ledger half is never opened looks identical to a family nobody
// enumerated.
//
// EVERY (family, file) pairing, not the first. An earlier version returned
// family -> FIRST file, and the shared manifest is read first, so a family with
// rows in both halves resolved to "shared" and its rows in the direct half were
// never examined. Measured by moving rows in a throwaway tree: moving one of
// shell_exec's two shared-half coverage rows into the direct manifest left
// TestEachLedgerHalfHoldsOnlyItsOwnFamilies green, while moving one of
// built_from's two direct-half waiver rows the other way red it. The guard was
// direction-dependent, and the direction it missed is the silent one.
func loadMaterializedEdgeLedgerRows(t *testing.T, specsDir string) []ledgerSurfaceRow {
	t.Helper()

	var out []ledgerSurfaceRow
	for _, half := range materializedEdgeLedgerHalves() {
		path := filepath.Join(specsDir, half.File)
		raw, err := os.ReadFile(path) // #nosec G304 -- repo-relative path built from a package constant.
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		found := 0
		for _, m := range manifestSurfaceKey.FindAllStringSubmatch(string(raw), -1) {
			out = append(out, ledgerSurfaceRow{Family: m[1], File: half.File})
			found++
		}
		if found == 0 {
			t.Fatalf("no %q surface keys parsed from %s; the ledger format changed and this check went vacuous", MaterializedEdgeSurfacePrefix, path)
		}
	}
	return out
}

// ledgerSurfaceHalves folds rows into family -> every half carrying a row for
// it, sorted, for the union questions ("is this family claimed anywhere") that
// do not care which file the claim sits in.
//
// Every half rather than one, so a failure about a family named in both can say
// both instead of sending the reader to whichever file happened to be read
// first.
func ledgerSurfaceHalves(rows []ledgerSurfaceRow) map[string][]string {
	out := map[string][]string{}
	for _, row := range rows {
		if !slices.Contains(out[row.Family], row.File) {
			out[row.Family] = append(out[row.Family], row.File)
		}
	}
	for _, files := range out {
		sort.Strings(files)
	}
	return out
}

// TestEachLedgerHalfHoldsOnlyItsOwnFamilies pins WHICH file each ledger ROW
// lives in, not merely that some file holds one for its family.
//
// Every other check here reads the union of the two halves. That is the right
// shape for asking "is this family claimed anywhere", and the wrong shape for
// the split itself: a row in the wrong half satisfies every union check in this
// file. The two-file split is what keeps each half readable and under the
// 500-line cap, and #6181 treats it as load-bearing — so it needs an assertion
// rather than a convention.
//
// Per (family, half), not per family, because a family-level assertion is
// direction-dependent. Measured by moving rows in a throwaway tree against the
// version that resolved a family to the FIRST file carrying it: moving one of
// built_from's two direct-half waiver rows into the shared manifest red it,
// and moving one of shell_exec's two shared-half coverage rows into the direct
// manifest did NOT — the family still resolved to the shared half it belongs
// to, so the misplaced row was never examined. Both red now.
//
// It is NOT the only thing that reds on a misplacement, and claiming so would
// be the same overreach this change removes one file over. Measured the same
// way: move ALL of a family's rows and the coverage/waiver reconciliation reds
// too, in both directions, because the moved rows become a dangling waiver or a
// lost coverage row against the family set its caller passes. What it does NOT
// do is say the row is in the wrong HALF — it reports "stale waiver" or
// "uncovered", which sends the maintainer to the wrong question. On a PARTIAL
// move it is quieter still: the only other red was the fixture that reconciles
// the SHARED half alone, and everything reading the merged
// LoadMaterializedEdgeLedger stayed green, because the merge puts the row back
// where the reconciliation expects it. This check names the actual mistake, and
// on the partial case it is the only check that names anything.
//
// The direction that matters is misplacement, not absence: absence is already
// caught by the coverage gate, which requires every (surface, proof_gate) pair
// to be covered or waived.
func TestEachLedgerHalfHoldsOnlyItsOwnFamilies(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootDir(t)
	rows := loadMaterializedEdgeLedgerRows(t, filepath.Join(repoRoot, "specs"))
	if len(rows) == 0 {
		t.Fatal("ledger parsed to zero rows; this check would assert nothing")
	}

	// Disjoint enumerations are the PRECONDITION of everything below, not a
	// nicety. A family in both would be owned by whichever half its row happens
	// to sit in, so a misplaced row would satisfy the owner test on either
	// side and this check would assert nothing for it -- vacuous in exactly the
	// way a guard that cannot fail is. materialized_edges_waiver_issue_test.go
	// records the same property as an observation its resolution order leans
	// on; here it is the thing being leaned on, so it fails fast instead.
	sharedFamilies := setOf(reducer.MaterializedEdgeFamilies())
	var both []string
	for _, family := range reducer.DirectMaterializedEdgeFamilies() {
		if _, clash := sharedFamilies[family]; clash {
			both = append(both, family)
		}
	}
	if len(both) > 0 {
		sort.Strings(both)
		t.Fatalf("%d famil(ies) are enumerated as BOTH shared-projection and direct-materialization: %v. Every check below goes vacuous for them -- a row in either half satisfies the half that owns it -- so this stops before asserting anything",
			len(both), both)
	}

	// Every row is examined; the report is deduplicated to one line per
	// (family, half). A family with a dozen misplaced rows in one file is one
	// mistake, and repeating the line a dozen times buries the other findings.
	reported := make(map[ledgerSurfaceRow]struct{}, len(rows))
	for _, half := range materializedEdgeLedgerHalves() {
		owned := setOf(half.Owner())
		if len(owned) == 0 {
			t.Fatalf("%s enumerates zero families; every row in %s would report as misplaced for the wrong reason", half.OwnerName, half.File)
		}
		for _, row := range rows {
			if row.File != half.File {
				continue
			}
			if _, dup := reported[row]; dup {
				continue
			}
			reported[row] = struct{}{}
			if _, ok := owned[row.Family]; !ok {
				t.Errorf("%s carries a row for %q, which %s does not enumerate. %s", half.File, row.Family, half.OwnerName, half.Misplaced)
			}
		}
	}
	t.Logf("checked %d ledger row(s) across %d (family, half) pairing(s)", len(rows), len(reported))
}
