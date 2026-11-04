// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocr

import (
	"strings"
	"testing"
)

// TestExtractRedactsCellsBelowATableHeader covers the table-column residual:
// a header row such as "Service Password Owner" read as one cell per column
// leaves every cell below the Password header in clear past the two-region
// window. Each cell under the marker header is withheld by x-position, while
// the cells under the other headers stay.
func TestExtractRedactsCellsBelowATableHeader(t *testing.T) {
	t.Parallel()

	const redacted = "[redacted]"
	regions := []Region{
		{Text: "Service", Bounds: Bounds{X: 0.02, Y: 0.10, Width: 0.20, Height: 0.05}},
		{Text: "Password", Bounds: Bounds{X: 0.35, Y: 0.10, Width: 0.20, Height: 0.05}},
		{Text: "Owner", Bounds: Bounds{X: 0.68, Y: 0.10, Width: 0.20, Height: 0.05}},
		{Text: "checkout", Bounds: Bounds{X: 0.02, Y: 0.20, Width: 0.20, Height: 0.05}},
		{Text: "hunter2", Bounds: Bounds{X: 0.35, Y: 0.20, Width: 0.20, Height: 0.05}},
		{Text: "alice", Bounds: Bounds{X: 0.68, Y: 0.20, Width: 0.20, Height: 0.05}},
		{Text: "staging", Bounds: Bounds{X: 0.02, Y: 0.30, Width: 0.20, Height: 0.05}},
		{Text: "brav0key", Bounds: Bounds{X: 0.35, Y: 0.30, Width: 0.20, Height: 0.05}},
		{Text: "bob", Bounds: Bounds{X: 0.68, Y: 0.30, Width: 0.20, Height: 0.05}},
		{Text: "prod", Bounds: Bounds{X: 0.02, Y: 0.40, Width: 0.20, Height: 0.05}},
		{Text: "charlie9", Bounds: Bounds{X: 0.35, Y: 0.40, Width: 0.20, Height: 0.05}},
		{Text: "carol", Bounds: Bounds{X: 0.68, Y: 0.40, Width: 0.20, Height: 0.05}},
		{Text: "dev", Bounds: Bounds{X: 0.02, Y: 0.50, Width: 0.20, Height: 0.05}},
		{Text: "delta4qa", Bounds: Bounds{X: 0.35, Y: 0.50, Width: 0.20, Height: 0.05}},
		{Text: "dave", Bounds: Bounds{X: 0.68, Y: 0.50, Width: 0.20, Height: 0.05}},
	}
	// The reading-order window still withholds the two regions after the
	// Password header ("Owner" and "checkout"); that deliberate cost is
	// unchanged. The column rule withholds the Password cells past it,
	// while the Service and Owner cells below the window stay.
	want := []string{
		"Service", redacted, redacted,
		redacted, redacted, "alice",
		"staging", redacted, "bob",
		"prod", redacted, "carol",
		"dev", redacted, "dave",
	}

	got := persistedContents(extractRegions(t, Options{}, regions))
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("persisted content = %q, want %q", got, want)
	}
}

// TestExtractTableColumnLimits pins what the column rule does not cover. A
// header row that tesseract reads as one wide line has no cell to map by
// x-position, so only the reading-order window applies. A value above its
// header stays, as it does for the window and row clauses.
func TestExtractTableColumnLimits(t *testing.T) {
	t.Parallel()

	const redacted = "[redacted]"
	t.Run("one wide header line opens no column", func(t *testing.T) {
		t.Parallel()

		regions := []Region{
			{Text: "Service Password Owner", Bounds: Bounds{X: 0.02, Y: 0.10, Width: 0.90, Height: 0.05}},
			{Text: "checkout hunter2 alice", Bounds: Bounds{X: 0.02, Y: 0.20, Width: 0.90, Height: 0.05}},
			{Text: "staging brav0key bob", Bounds: Bounds{X: 0.02, Y: 0.30, Width: 0.90, Height: 0.05}},
			{Text: "prod charlie9 carol", Bounds: Bounds{X: 0.02, Y: 0.40, Width: 0.90, Height: 0.05}},
			{Text: "dev delta4qa dave", Bounds: Bounds{X: 0.02, Y: 0.50, Width: 0.90, Height: 0.05}},
		}
		want := []string{redacted, redacted, redacted, "prod charlie9 carol", "dev delta4qa dave"}

		got := persistedContents(extractRegions(t, Options{}, regions))
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("persisted content = %q, want %q", got, want)
		}
	})
	t.Run("a value above its header stays", func(t *testing.T) {
		t.Parallel()

		regions := []Region{
			{Text: "Service", Bounds: Bounds{X: 0.02, Y: 0.10, Width: 0.20, Height: 0.05}},
			{Text: "hunter2", Bounds: Bounds{X: 0.35, Y: 0.10, Width: 0.20, Height: 0.05}},
			{Text: "Owner", Bounds: Bounds{X: 0.68, Y: 0.10, Width: 0.20, Height: 0.05}},
			{Text: "Release notes", Bounds: Bounds{X: 0.02, Y: 0.20, Width: 0.20, Height: 0.05}},
			{Text: "Password", Bounds: Bounds{X: 0.35, Y: 0.20, Width: 0.20, Height: 0.05}},
			{Text: "Rotation dates", Bounds: Bounds{X: 0.68, Y: 0.20, Width: 0.20, Height: 0.05}},
		}
		// "hunter2" sits above the Password header, so the column does
		// not reach it; the window withholds the two regions after the
		// header instead.
		want := []string{"Service", "hunter2", "Owner", "Release notes", redacted, redacted}

		got := persistedContents(extractRegions(t, Options{}, regions))
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("persisted content = %q, want %q", got, want)
		}
	})
	t.Run("a pattern header opens no column", func(t *testing.T) {
		t.Parallel()

		regions := []Region{
			{Text: "alice@example.com", Bounds: Bounds{X: 0.35, Y: 0.10, Width: 0.20, Height: 0.05}},
			{Text: "Owner", Bounds: Bounds{X: 0.68, Y: 0.10, Width: 0.20, Height: 0.05}},
			{Text: "Release notes", Bounds: Bounds{X: 0.02, Y: 0.20, Width: 0.60, Height: 0.05}},
			{Text: "Rotation dates", Bounds: Bounds{X: 0.35, Y: 0.30, Width: 0.20, Height: 0.05}},
			{Text: "Second review", Bounds: Bounds{X: 0.35, Y: 0.40, Width: 0.20, Height: 0.05}},
		}
		// The email redacts only itself: it is a value, not a word that
		// introduces one, so no window, row, or column opens under it.
		want := []string{redacted, "Owner", "Release notes", "Rotation dates", "Second review"}

		got := persistedContents(extractRegions(t, Options{}, regions))
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("persisted content = %q, want %q", got, want)
		}
	})
}

// TestExtractTableColumnTies keeps the column geometry deterministic on exact
// edges and unusable bounds. A probe always sits past the marker's window, so
// only the column clause can withhold it.
func TestExtractTableColumnTies(t *testing.T) {
	t.Parallel()

	header := Bounds{X: 0.35, Y: 0.10, Width: 0.20, Height: 0.05}
	sibling := Region{Text: "Owner", Bounds: Bounds{X: 0.68, Y: 0.10, Width: 0.20, Height: 0.05}}
	below := header.Y + header.Height
	tests := []struct {
		name         string
		header       Bounds
		probe        Bounds
		wantRedacted bool
	}{
		{name: "cell touching the header bottom joins", probe: Bounds{X: 0.35, Y: below, Width: 0.20, Height: 0.05}, wantRedacted: true},
		{name: "cell overlapping half the shorter width joins", probe: Bounds{X: 0.45, Y: 0.30, Width: 0.20, Height: 0.05}, wantRedacted: true},
		{name: "narrow cell fully inside the header joins", probe: Bounds{X: 0.40, Y: 0.30, Width: 0.05, Height: 0.05}, wantRedacted: true},
		{name: "cell overlapping under half the shorter width stays", probe: Bounds{X: 0.46, Y: 0.30, Width: 0.20, Height: 0.05}},
		{name: "cell above the header stays", probe: Bounds{X: 0.35, Y: 0.02, Width: 0.20, Height: 0.05}},
		{name: "cell on the header row stays past the window", probe: Bounds{X: 0.35, Y: 0.11, Width: 0.20, Height: 0.05}},
		{name: "zero probe width has no column", probe: Bounds{X: 0.35, Y: 0.30, Height: 0.05}},
		{name: "zero probe height has no column", probe: Bounds{X: 0.35, Y: 0.30, Width: 0.20}},
		{name: "zero header width opens no column", header: Bounds{X: 0.35, Y: 0.10, Height: 0.05}, probe: Bounds{X: 0.35, Y: 0.30, Width: 0.20, Height: 0.05}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			header := tt.header
			if header == (Bounds{}) {
				header = Bounds{X: 0.35, Y: 0.10, Width: 0.20, Height: 0.05}
			}
			regions := []Region{
				{Text: "Service", Bounds: Bounds{X: 0.02, Y: 0.10, Width: 0.20, Height: 0.05}},
				{Text: "Password", Bounds: header},
				sibling,
				{Text: "Next step"},
				{Text: "Second step"},
				{Text: "Probe text", Bounds: tt.probe},
			}
			got := extractRegions(t, Options{}, regions).Sections[5].Content
			if want := tt.wantRedacted; want != (got == "[redacted]") {
				t.Fatalf("probe content = %q, want redacted %v", got, want)
			}
		})
	}
}

// TestExtractTableColumnHalfBoundaryExact pins the >= half rule with
// exactly-representable geometry: every coordinate is a power of two, so the
// overlap (0.125) and the half-shorter-width bound (0.125) compare exactly
// and the test distinguishes >= from >. A probe one full step right overlaps
// by 0 and stays.
func TestExtractTableColumnHalfBoundaryExact(t *testing.T) {
	t.Parallel()

	header := Bounds{X: 0.25, Y: 0.10, Width: 0.25, Height: 0.05}
	sibling := Region{Text: "Owner", Bounds: Bounds{X: 0.75, Y: 0.10, Width: 0.25, Height: 0.05}}
	tests := []struct {
		name         string
		probe        Bounds
		wantRedacted bool
	}{
		{name: "overlap exactly half the shorter width joins", probe: Bounds{X: 0.375, Y: 0.30, Width: 0.25, Height: 0.05}, wantRedacted: true},
		{name: "zero overlap stays", probe: Bounds{X: 0.50, Y: 0.30, Width: 0.25, Height: 0.05}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			regions := []Region{
				{Text: "Service", Bounds: Bounds{X: 0.00, Y: 0.10, Width: 0.25, Height: 0.05}},
				{Text: "Password", Bounds: header},
				sibling,
				{Text: "Next step"},
				{Text: "Second step"},
				{Text: "Probe text", Bounds: tt.probe},
			}
			got := extractRegions(t, Options{}, regions).Sections[5].Content
			if want := tt.wantRedacted; want != (got == "[redacted]") {
				t.Fatalf("probe content = %q, want redacted %v", got, want)
			}
		})
	}
}

// TestExtractTableColumnNeedsALeftNeighbor pins the header gate: a marker
// that opens its row is the form shape, with its value to the right, so it
// keeps the two-region window below it and opens no column, even with a cell
// waiting in its band past the window.
func TestExtractTableColumnNeedsALeftNeighbor(t *testing.T) {
	t.Parallel()

	regions := []Region{
		{Text: "Password", Bounds: Bounds{X: 0.02, Y: 0.26, Width: 0.11, Height: 0.05}},
		{Text: "Filler one", Bounds: Bounds{X: 0.02, Y: 0.44, Width: 0.45, Height: 0.05}},
		{Text: "Filler two", Bounds: Bounds{X: 0.02, Y: 0.53, Width: 0.45, Height: 0.05}},
		{Text: "Filler three", Bounds: Bounds{X: 0.02, Y: 0.62, Width: 0.45, Height: 0.05}},
		{Text: labelValueCanary, Bounds: Bounds{X: 0.53, Y: 0.26, Width: 0.30, Height: 0.05}},
		{Text: "Rotate them after an incident", Bounds: Bounds{X: 0.53, Y: 0.62, Width: 0.45, Height: 0.05}},
	}
	want := []string{"[redacted]", "[redacted]", "[redacted]", "Filler three", "[redacted]", "Rotate them after an incident"}

	got := persistedContents(extractRegions(t, Options{}, regions))
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("persisted content = %q, want %q", got, want)
	}
}

// TestExtractTableColumnFeedsNoSecretRun pins the pass order: the column pass
// runs after extendSecretRuns, so a withheld column cell never chains a token
// line beside the column into a run.
func TestExtractTableColumnFeedsNoSecretRun(t *testing.T) {
	t.Parallel()

	const redacted = "[redacted]"
	regions := []Region{
		{Text: "Service", Bounds: Bounds{X: 0.02, Y: 0.10, Width: 0.20, Height: 0.05}},
		{Text: "Password", Bounds: Bounds{X: 0.35, Y: 0.10, Width: 0.20, Height: 0.05}},
		{Text: "Owner", Bounds: Bounds{X: 0.68, Y: 0.10, Width: 0.20, Height: 0.05}},
		{Text: "First filler line here", Bounds: Bounds{X: 0.02, Y: 0.20, Width: 0.60, Height: 0.05}},
		{Text: "Second filler line here", Bounds: Bounds{X: 0.02, Y: 0.30, Width: 0.60, Height: 0.05}},
		{Text: "hunter2", Bounds: Bounds{X: 0.35, Y: 0.40, Width: 0.20, Height: 0.05}},
		{Text: "vaultcellar7door3cabinet9drawer2shelf5lock1bin4box6", Bounds: Bounds{X: 0.02, Y: 0.50, Width: 0.20, Height: 0.05}},
	}
	// The token line sits outside the Password column and follows a cell
	// the run pass saw as kept, so it stays even though the column pass
	// withholds the cell after it. The line is lowercase letters and
	// digits, so it is a run token but never a seed on its own.
	want := []string{
		"Service", redacted, redacted, redacted, redacted, redacted,
		"vaultcellar7door3cabinet9drawer2shelf5lock1bin4box6",
	}

	got := persistedContents(extractRegions(t, Options{}, regions))
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("persisted content = %q, want %q", got, want)
	}
}
