// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"sync"
	"testing"
)

// TestEvaluateSnapshotExportVerdicts is the #7957 verdict table: deny every
// build below the PostgreSQL 18.7 floor unless its exact server_version is
// declared, and deny every other major version unless declared.
func TestEvaluateSnapshotExportVerdicts(t *testing.T) {
	t.Parallel()
	declared := []SnapshotExportBuild{{ServerVersion: "18.3", Evidence: "sha256:abc"}}
	for _, tc := range []struct {
		name          string
		version       string
		versionNum    int
		builds        []SnapshotExportBuild
		wantQualified bool
		wantReason    string
		wantEvidence  string
	}{
		{name: "18.3 undeclared", version: "18.3", versionNum: 180003, wantReason: snapshotExportReasonNoDeclaration},
		{name: "18.3 declared exact", version: "18.3", versionNum: 180003, builds: declared, wantQualified: true, wantReason: snapshotExportReasonDeclaredBuild, wantEvidence: "sha256:abc"},
		{name: "declared with leading whitespace", version: " 18.3", versionNum: 180003, builds: declared, wantReason: snapshotExportReasonUndeclared},
		{name: "declared with trailing whitespace", version: "18.3 ", versionNum: 180003, builds: declared, wantReason: snapshotExportReasonUndeclared},
		{
			name: "declared with case difference", version: "18.3 (Debian 18.3-1.PGDG)", versionNum: 180003,
			builds: []SnapshotExportBuild{{ServerVersion: "18.3 (debian 18.3-1.pgdg)", Evidence: "x"}}, wantReason: snapshotExportReasonUndeclared,
		},
		{name: "declared prefix is not a match", version: "18.3 (Debian 18.3-1.PGDG)", versionNum: 180003, builds: declared, wantReason: snapshotExportReasonUndeclared},
		{name: "18.6 undeclared", version: "18.6", versionNum: 180006, wantReason: snapshotExportReasonNoDeclaration},
		{name: "18.7 undeclared", version: "18.7", versionNum: 180007, wantQualified: true, wantReason: snapshotExportReasonVersionFloor},
		{name: "18.12 undeclared", version: "18.12", versionNum: 180012, wantQualified: true, wantReason: snapshotExportReasonVersionFloor},
		{name: "17.10 undeclared", version: "17.10", versionNum: 170010, wantReason: snapshotExportReasonNoDeclaration},
		{name: "17.10 with other declaration", version: "17.10", versionNum: 170010, builds: declared, wantReason: snapshotExportReasonUndeclared},
		{name: "19.0 undeclared", version: "19.0", versionNum: 190000, wantReason: snapshotExportReasonNoDeclaration},
		{
			name: "19.0 declared", version: "19.0", versionNum: 190000,
			builds: []SnapshotExportBuild{{ServerVersion: "19.0", Evidence: "y"}}, wantQualified: true, wantReason: snapshotExportReasonDeclaredBuild, wantEvidence: "y",
		},
		{name: "unknown build", version: "", versionNum: 0, wantReason: snapshotExportReasonNoDeclaration},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			qualified, reason, evidence := evaluateSnapshotExport(tc.version, tc.versionNum, tc.builds)
			if qualified != tc.wantQualified || reason != tc.wantReason || evidence != tc.wantEvidence {
				t.Fatalf("evaluateSnapshotExport(%q, %d) = %t %q %q, want %t %q %q",
					tc.version, tc.versionNum, qualified, reason, evidence, tc.wantQualified, tc.wantReason, tc.wantEvidence)
			}
		})
	}
}

// TestSnapshotExportFloorCoversBothUpstreamFixes guards the compiled floor:
// PostgreSQL 18.6 was tagged on 2026-08-11, before REL_18_STABLE received
// 74c9832ca610 (2026-08-26, standby export subxip[] fix, backpatch of
// ef89c839cdc8) and 3977cde272bc (2026-08-20, import xmin/VACUUM race fix,
// backpatch of ca99016f939d). The first release that can carry both is 18.7,
// so the floor must never drop below 180007.
func TestSnapshotExportFloorCoversBothUpstreamFixes(t *testing.T) {
	t.Parallel()
	const (
		exportDuringRecoveryFix = "74c9832ca610"
		importXminRaceFix       = "3977cde272bc"
		firstReleaseWithBoth    = 180007
	)
	if snapshotExportQualifiedVersionNum < firstReleaseWithBoth {
		t.Fatalf("floor %d admits a release without %s and %s", snapshotExportQualifiedVersionNum, exportDuringRecoveryFix, importXminRaceFix)
	}
	if snapshotExportQualifiedVersionNum/10000 != snapshotExportQualifiedMajor {
		t.Fatalf("floor %d is not on major %d", snapshotExportQualifiedVersionNum, snapshotExportQualifiedMajor)
	}
}

// TestSnapshotExportQualificationHolderFlipsAtomically proves the per-pool and
// per-member verdict holder: nil and unobserved holders deny, an unchanged
// build publishes no new verdict, a changed build flips it both ways, and
// concurrent observers and readers are race-free under -race.
func TestSnapshotExportQualificationHolderFlipsAtomically(t *testing.T) {
	t.Parallel()
	var missing *snapshotExportQualification
	if missing.qualified() || missing.current() != nil || missing.denialReason() != snapshotExportReasonNoDeclaration {
		t.Fatal("nil holder must deny with no_declaration")
	}
	holder := newSnapshotExportQualification([]SnapshotExportBuild{{ServerVersion: "18.6", Evidence: "ci"}})
	if holder.qualified() || holder.denialReason() != snapshotExportReasonUndeclared {
		t.Fatal("unobserved holder must deny")
	}
	if !holder.observe("18.6", 180006, true) || !holder.qualified() {
		t.Fatal("declared build did not qualify")
	}
	first := holder.current()
	holder.observe("18.6", 180006, true)
	if holder.current() != first {
		t.Fatal("unchanged build republished its verdict")
	}
	if holder.observe("18.5", 180005, true) || holder.qualified() {
		t.Fatal("undeclared build did not flip the verdict to denied")
	}
	if got := holder.current(); got.reason != snapshotExportReasonUndeclared || got.serverVersion != "18.5" || got.serverVersionNum != 180005 || !got.inRecovery {
		t.Fatalf("denied verdict = %+v", got)
	}

	var workers sync.WaitGroup
	for i := range 16 {
		workers.Add(2)
		go func() {
			defer workers.Done()
			for range 200 {
				if i%2 == 0 {
					holder.observe("18.6", 180006, true)
				} else {
					holder.observe("18.5", 180005, true)
				}
			}
		}()
		go func() {
			defer workers.Done()
			for range 200 {
				if verdict := holder.current(); verdict != nil && verdict.qualified != (verdict.serverVersion == "18.6") {
					t.Errorf("torn verdict %+v", verdict)
					return
				}
				_ = holder.qualified()
			}
		}()
	}
	workers.Wait()
}
