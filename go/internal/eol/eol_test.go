// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package eol

import (
	"testing"
	"time"
)

// fixtureSnapshot is a fixed endoflife.date-shaped snapshot so status
// assertions never depend on the vendored data or the wall clock.
const fixtureSnapshotDate = "2026-05-01"

func fixtureFiles() map[string][]byte {
	return map[string][]byte{
		"nodejs": []byte(`[
			{"cycle":"22","releaseDate":"2025-10-15","eol":"2027-04-30","latest":"22.1.0","latestReleaseDate":"2026-04-01","support":"2026-12-31"},
			{"cycle":"21","releaseDate":"2025-04-15","eol":"2026-09-01","latest":"21.0.0","latestReleaseDate":"2025-05-01","support":"2026-04-01"},
			{"cycle":"20","releaseDate":"2024-04-15","eol":"2026-04-30","latest":"20.9.0","latestReleaseDate":"2025-03-01","support":"2025-10-01"}
		]`),
		"go": []byte(`[
			{"cycle":"1.24","releaseDate":"2025-02-11","eol":false,"latest":"1.24.3","latestReleaseDate":"2026-04-08"}
		]`),
		"python": []byte(`[
			{"cycle":"3.13","releaseDate":"2024-10-07","eol":"2029-10-31","latest":"3.13.3","latestReleaseDate":"2026-04-08","support":"2026-10-01"},
			{"cycle":"3.12","releaseDate":"2023-10-02","eol":"2028-10-31","latest":"3.12.9","latestReleaseDate":"2026-02-04","support":"2025-04-02"}
		]`),
	}
}

func fixtureNow() time.Time {
	return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
}

func mustParse(t *testing.T) Snapshot {
	t.Helper()
	sn, err := ParseSnapshot(fixtureFiles(), fixtureSnapshotDate)
	if err != nil {
		t.Fatalf("ParseSnapshot: %v", err)
	}
	return sn
}

func TestResolveSupported(t *testing.T) {
	sn := mustParse(t)
	ans := Resolve(sn, "node", 22, 0, false, fixtureNow())
	if ans.Status != StatusSupported {
		t.Fatalf("node 22 = %q, want %q", ans.Status, StatusSupported)
	}
	if ans.Cycle != "22" {
		t.Fatalf("Cycle = %q, want %q", ans.Cycle, "22")
	}
}

func TestResolveEOL(t *testing.T) {
	sn := mustParse(t)
	ans := Resolve(sn, "node", 20, 11, true, fixtureNow())
	if ans.Status != StatusEOL {
		t.Fatalf("node 20.11 = %q, want %q", ans.Status, StatusEOL)
	}
}

func TestResolveSupportEnding(t *testing.T) {
	sn := mustParse(t)
	// Cycle 21: support ended 2026-04-01, eol 2026-09-01, now 2026-06-01.
	ans := Resolve(sn, "node", 21, 0, false, fixtureNow())
	if ans.Status != StatusSupportEnding {
		t.Fatalf("node 21 = %q, want %q", ans.Status, StatusSupportEnding)
	}
}

func TestResolveRollingEOLFalseIsSupported(t *testing.T) {
	sn := mustParse(t)
	ans := Resolve(sn, "go", 1, 24, true, fixtureNow())
	if ans.Status != StatusSupported {
		t.Fatalf("go 1.24 = %q, want %q", ans.Status, StatusSupported)
	}
}

func TestResolveMinorMismatchIsUnknown(t *testing.T) {
	sn := mustParse(t)
	ans := Resolve(sn, "python", 3, 99, true, fixtureNow())
	if ans.Status != StatusUnknown {
		t.Fatalf("python 3.99 = %q, want %q", ans.Status, StatusUnknown)
	}
}

func TestResolveAmbiguousMajorOnlyIsUnknown(t *testing.T) {
	sn := mustParse(t)
	// "3" alone cannot pick between cycles 3.12 and 3.13.
	ans := Resolve(sn, "python", 3, 0, false, fixtureNow())
	if ans.Status != StatusUnknown {
		t.Fatalf("python 3 = %q, want %q", ans.Status, StatusUnknown)
	}
}

func TestResolveUnmappedRuntimesAreUnknown(t *testing.T) {
	sn := mustParse(t)
	for _, runtime := range []string{"java", "swift", "cobol", ""} {
		ans := Resolve(sn, runtime, 21, 0, false, fixtureNow())
		if ans.Status != StatusUnknown {
			t.Fatalf("%q = %q, want %q", runtime, ans.Status, StatusUnknown)
		}
		if ans.Cycle != "" {
			t.Fatalf("%q Cycle = %q, want empty", runtime, ans.Cycle)
		}
	}
}

func TestResolveMissingCycleIsUnknown(t *testing.T) {
	sn := mustParse(t)
	ans := Resolve(sn, "node", 99, 0, false, fixtureNow())
	if ans.Status != StatusUnknown {
		t.Fatalf("node 99 = %q, want %q", ans.Status, StatusUnknown)
	}
}

func TestAnswerCarriesSnapshotAge(t *testing.T) {
	sn := mustParse(t)
	ans := Resolve(sn, "node", 22, 0, false, fixtureNow())
	if ans.SnapshotDate != fixtureSnapshotDate {
		t.Fatalf("SnapshotDate = %q, want %q", ans.SnapshotDate, fixtureSnapshotDate)
	}
	// 2026-05-01 to 2026-06-01 is 31 days.
	if ans.SnapshotAgeDays != 31 {
		t.Fatalf("SnapshotAgeDays = %d, want 31", ans.SnapshotAgeDays)
	}
	if !ans.Stale {
		t.Fatal("31-day-old snapshot must be stale past the 30-day window")
	}
}

func TestFreshSnapshotIsNotStale(t *testing.T) {
	sn, err := ParseSnapshot(fixtureFiles(), "2026-05-20")
	if err != nil {
		t.Fatalf("ParseSnapshot: %v", err)
	}
	ans := Resolve(sn, "node", 22, 0, false, fixtureNow())
	if ans.SnapshotAgeDays != 12 {
		t.Fatalf("SnapshotAgeDays = %d, want 12", ans.SnapshotAgeDays)
	}
	if ans.Stale {
		t.Fatal("12-day-old snapshot must not be stale")
	}
}

func TestFutureSnapshotDateClampsAgeAtZero(t *testing.T) {
	sn, err := ParseSnapshot(fixtureFiles(), "2026-07-01")
	if err != nil {
		t.Fatalf("ParseSnapshot: %v", err)
	}
	ans := Resolve(sn, "node", 22, 0, false, fixtureNow())
	if ans.SnapshotAgeDays != 0 {
		t.Fatalf("SnapshotAgeDays = %d, want 0", ans.SnapshotAgeDays)
	}
	if ans.Stale {
		t.Fatal("future snapshot date must not read as stale")
	}
}

// TestResolveUsesNoNetwork proves the air-gapped contract: with every
// proxy variable pointed at an unroutable address, the read path still
// resolves from vendored data alone.
func TestResolveUsesNoNetwork(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:9")
	t.Setenv("NO_PROXY", "")
	sn := mustParse(t)
	ans := Resolve(sn, "node", 20, 11, true, fixtureNow())
	if ans.Status != StatusEOL {
		t.Fatalf("node 20.11 offline = %q, want %q", ans.Status, StatusEOL)
	}
}

func TestParseSnapshotRejectsBadDate(t *testing.T) {
	if _, err := ParseSnapshot(fixtureFiles(), "not-a-date"); err == nil {
		t.Fatal("bad snapshot_date must fail, not parse as unknown age")
	}
}

func TestParseSnapshotRejectsBadJSON(t *testing.T) {
	files := map[string][]byte{"nodejs": []byte(`not json`)}
	if _, err := ParseSnapshot(files, fixtureSnapshotDate); err == nil {
		t.Fatal("malformed product JSON must fail")
	}
}

func TestParseSnapshotNormalizesSchemaDrift(t *testing.T) {
	files := map[string][]byte{"nodejs": []byte(`[
		{"cycle":"26","eol":"2029-04-30","support":false},
		{"cycle":"25","eol":false},
		{"cycle":"24","support":"2026-10-20"}
	]`)}
	sn, err := ParseSnapshot(files, fixtureSnapshotDate)
	if err != nil {
		t.Fatalf("ParseSnapshot: %v", err)
	}
	got := sn.Products["nodejs"]
	if len(got) != 3 {
		t.Fatalf("cycles = %d, want 3", len(got))
	}
	if got[0].EOL != "2029-04-30" || got[0].Support != "" {
		t.Fatalf("support:false must normalize to empty, got %+v", got[0])
	}
	if !got[1].EOLFalse || got[1].EOL != "" {
		t.Fatalf("eol:false must mark rolling, got %+v", got[1])
	}
	if got[2].EOL != "" || got[2].Support != "2026-10-20" {
		t.Fatalf("absent eol must leave EOL empty, got %+v", got[2])
	}
	// Absent eol is unknown, never supported.
	ans := Resolve(sn, "node", 24, 0, false, fixtureNow())
	if ans.Status != StatusUnknown {
		t.Fatalf("missing eol = %q, want %q", ans.Status, StatusUnknown)
	}
}

func TestEmbeddedSnapshotLoadsEightProducts(t *testing.T) {
	sn, err := LoadSnapshot()
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	for _, slug := range []string{"nodejs", "go", "python", "ruby", "php", "rust", "terraform", "dotnet"} {
		if len(sn.Products[slug]) == 0 {
			t.Fatalf("embedded snapshot has no cycles for %q", slug)
		}
	}
	if sn.Date == "" {
		t.Fatal("embedded snapshot has no snapshot_date")
	}
	if _, err := time.Parse("2006-01-02", sn.Date); err != nil {
		t.Fatalf("snapshot_date %q is not YYYY-MM-DD: %v", sn.Date, err)
	}
}

// TestResolveEOLDayBoundary pins the eol-day edge: the eol day itself keeps
// its pre-eol status and only the day after resolves eol, so dropping the
// +24h grace from the eol comparison must fail here.
func TestResolveEOLDayBoundary(t *testing.T) {
	sn := mustParse(t)
	// Cycle 20 ends 2026-04-30 with support ended 2025-10-01.
	eolDay := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	if ans := Resolve(sn, "node", 20, 0, false, eolDay); ans.Status != StatusSupportEnding {
		t.Fatalf("node 20 on eol day = %q, want %q", ans.Status, StatusSupportEnding)
	}
	dayAfter := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if ans := Resolve(sn, "node", 20, 0, false, dayAfter); ans.Status != StatusEOL {
		t.Fatalf("node 20 day after eol = %q, want %q", ans.Status, StatusEOL)
	}
}

// TestResolveSupportDayBoundary pins the support-day edge: the support day
// itself is still supported and only the day after opens support-ending, so
// dropping the +24h grace from the support comparison must fail here.
func TestResolveSupportDayBoundary(t *testing.T) {
	sn := mustParse(t)
	// Cycle 21 support ends 2026-04-01, eol 2026-09-01.
	supportDay := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	if ans := Resolve(sn, "node", 21, 0, false, supportDay); ans.Status != StatusSupported {
		t.Fatalf("node 21 on support day = %q, want %q", ans.Status, StatusSupported)
	}
	dayAfter := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	if ans := Resolve(sn, "node", 21, 0, false, dayAfter); ans.Status != StatusSupportEnding {
		t.Fatalf("node 21 day after support = %q, want %q", ans.Status, StatusSupportEnding)
	}
}

// TestSnapshotAgeBoundaryIsFreshAtThirtyDays pins the refresh-window edge:
// exactly RefreshWindowDays is still fresh (31 days is stale per
// TestAnswerCarriesSnapshotAge), so a >= comparison must fail here.
func TestSnapshotAgeBoundaryIsFreshAtThirtyDays(t *testing.T) {
	sn, err := ParseSnapshot(fixtureFiles(), "2026-05-02")
	if err != nil {
		t.Fatalf("ParseSnapshot: %v", err)
	}
	// 2026-05-02 to 2026-06-01 is exactly 30 days.
	ans := Resolve(sn, "node", 22, 0, false, fixtureNow())
	if ans.SnapshotAgeDays != 30 {
		t.Fatalf("SnapshotAgeDays = %d, want 30", ans.SnapshotAgeDays)
	}
	if ans.Stale {
		t.Fatal("30-day-old snapshot must not be stale")
	}
}

func TestEmbeddedSnapshotResolvesWithoutNetwork(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:9")
	t.Setenv("NO_PROXY", "")
	sn, err := LoadSnapshot()
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	now := time.Now().UTC()
	for _, runtime := range []string{"java", "swift", "cobol"} {
		if ans := Resolve(sn, runtime, 21, 0, false, now); ans.Status != StatusUnknown {
			t.Fatalf("%q embedded = %q, want unknown", runtime, ans.Status)
		}
	}
}
