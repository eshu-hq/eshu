// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocr

import (
	"math"
	"strings"
	"testing"
)

// TestExtractRedactsValueInTheColumnBesideALabel covers a layout the real
// engine produces: tesseract reads each column as its own block, so the value
// sits past the reading-order window but on the label's row.
func TestExtractRedactsValueInTheColumnBesideALabel(t *testing.T) {
	t.Parallel()

	label := Region{Text: "Password:", Bounds: Bounds{X: 0.02, Y: 0.26, Width: 0.11, Height: 0.05}}
	leftColumn := []Region{
		{Text: "Credentials for the staging cluster", Bounds: Bounds{X: 0.02, Y: 0.08, Width: 0.45, Height: 0.05}},
		{Text: "live in the shared vault", Bounds: Bounds{X: 0.02, Y: 0.17, Width: 0.45, Height: 0.05}},
	}
	rightColumn := []Region{
		{Text: "The checkout team owns these", Bounds: Bounds{X: 0.53, Y: 0.08, Width: 0.45, Height: 0.05}},
		{Text: "and they expire each quarter", Bounds: Bounds{X: 0.53, Y: 0.17, Width: 0.45, Height: 0.05}},
		{Text: "Rotate them after an incident", Bounds: Bounds{X: 0.53, Y: 0.35, Width: 0.45, Height: 0.05}},
	}
	value := Region{Text: labelValueCanary, Bounds: Bounds{X: 0.53, Y: 0.26, Width: 0.30, Height: 0.05}}

	tests := []struct {
		name    string
		regions []Region
		want    []string
	}{
		{
			name: "value column read after the label column",
			regions: []Region{
				leftColumn[0], leftColumn[1], label,
				rightColumn[0], rightColumn[1], value, rightColumn[2],
			},
			want: []string{
				"Credentials for the staging cluster", "live in the shared vault", "[redacted]",
				"[redacted]", "[redacted]", "[redacted]", "Rotate them after an incident",
			},
		},
		{
			name:    "value column read before the label column",
			regions: []Region{value, leftColumn[0], leftColumn[1], label},
			want:    []string{"[redacted]", "Credentials for the staging cluster", "live in the shared vault", "[redacted]"},
		},
		{
			name: "taller value on the label row",
			regions: []Region{label, leftColumn[0], leftColumn[1], rightColumn[2], {
				Text: labelValueCanary, Bounds: Bounds{X: 0.53, Y: 0.25, Width: 0.30, Height: 0.09},
			}},
			want: []string{"[redacted]", "[redacted]", "[redacted]", "Rotate them after an incident", "[redacted]"},
		},
		{
			name: "region left of the label stays",
			regions: []Region{
				{Text: "Field one", Bounds: Bounds{X: 0.53, Y: 0.26, Width: 0.11, Height: 0.05}},
				leftColumn[0], leftColumn[1],
				{Text: "Password:", Bounds: Bounds{X: 0.70, Y: 0.26, Width: 0.11, Height: 0.05}},
			},
			want: []string{"Field one", "Credentials for the staging cluster", "live in the shared vault", "[redacted]"},
		},
		{
			name: "region that only grazes the label row stays",
			regions: []Region{label, leftColumn[0], leftColumn[1], {
				Text: "Rotate them after an incident", Bounds: Bounds{X: 0.53, Y: 0.30, Width: 0.30, Height: 0.05},
			}},
			want: []string{"[redacted]", "[redacted]", "[redacted]", "Rotate them after an incident"},
		},
		{
			name: "engine without bounds has no row match",
			regions: []Region{
				{Text: "Password:"}, {Text: "line two"}, {Text: "line three"}, {Text: "line four"},
			},
			want: []string{"[redacted]", "[redacted]", "[redacted]", "line four"},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := extractRegions(t, Options{}, tt.regions)
			got := persistedContents(result)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("persisted content = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestExtractPatternRegionOpensNoRow pins that an email, URL, path, or ticket
// redacts only its own region. It is a value, not a word that introduces one,
// so the text beside it on the row stays.
func TestExtractPatternRegionOpensNoRow(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{"alice@example.com", "https://internal.example.com/x", "/home/alice/notes", "OPS-1234"} {
		pattern := pattern
		t.Run(pattern, func(t *testing.T) {
			t.Parallel()

			regions := []Region{
				{Text: pattern, Bounds: Bounds{X: 0.02, Y: 0.26, Width: 0.11, Height: 0.05}},
				{Text: "Release checklist", Bounds: Bounds{X: 0.53, Y: 0.26, Width: 0.30, Height: 0.05}},
			}
			got := persistedContents(extractRegions(t, Options{}, regions))
			if want := "[redacted]|Release checklist"; strings.Join(got, "|") != want {
				t.Fatalf("persisted content = %q, want %q", got, want)
			}
		})
	}
}

// TestExtractRowClauseHandlesInvalidAndTiedGeometry keeps the row clause
// deterministic on bounds an engine should never send and on exact ties. Bad
// geometry gets no row match, so the region falls back to the reading-order
// window, and a tie resolves the same way on every run. The region under test
// always sits past the marker's window.
func TestExtractRowClauseHandlesInvalidAndTiedGeometry(t *testing.T) {
	t.Parallel()

	const probe = "Probe text"
	nan, inf := math.NaN(), math.Inf(1)
	marker := Bounds{X: 0, Y: 0.25, Width: 0.25, Height: 0.5}
	onRow := Bounds{X: 0.5, Y: 0.25, Width: 0.25, Height: 0.5}
	tests := []struct {
		name         string
		marker       Bounds
		probe        Bounds
		wantRedacted bool
	}{
		{name: "valid row match", marker: marker, probe: onRow, wantRedacted: true},
		{name: "probe starts exactly at the marker's right edge", marker: marker, probe: Bounds{X: 0.25, Y: 0.25, Width: 0.25, Height: 0.5}, wantRedacted: true},
		{name: "probe overlaps exactly half the shorter height", marker: marker, probe: Bounds{X: 0.5, Y: 0.5, Width: 0.25, Height: 0.5}, wantRedacted: true},
		{name: "probe overlaps under half the shorter height", marker: marker, probe: Bounds{X: 0.5, Y: 0.625, Width: 0.25, Height: 0.5}},
		{name: "tall value over half the shorter height, under half the taller", marker: Bounds{Y: 0.3, Width: 0.25, Height: 0.1}, probe: Bounds{X: 0.5, Y: 0.25, Width: 0.25, Height: 0.5}, wantRedacted: true},
		{name: "probe overlaps just under half the shorter height", marker: marker, probe: Bounds{X: 0.5, Y: 0.5025, Width: 0.25, Height: 0.5}},
		{name: "probe starts inside the marker's horizontal span", marker: marker, probe: Bounds{X: 0.125, Y: 0.25, Width: 0.25, Height: 0.5}},
		{name: "negative marker width does not reach a region left of the marker", marker: Bounds{X: 0.5, Y: 0.25, Width: -0.45, Height: 0.5}, probe: Bounds{X: 0.2, Y: 0.25, Width: 0.1, Height: 0.5}},
		{name: "zero marker width", marker: Bounds{X: 0.1, Y: 0.25, Height: 0.5}, probe: onRow},
		{name: "negative probe width", marker: marker, probe: Bounds{X: 0.5, Y: 0.25, Width: -0.25, Height: 0.5}},
		{name: "zero probe width", marker: marker, probe: Bounds{X: 0.5, Y: 0.25, Height: 0.5}},
		{name: "zero probe height", marker: marker, probe: Bounds{X: 0.5, Y: 0.25, Width: 0.25}},
		{name: "negative probe height", marker: marker, probe: Bounds{X: 0.5, Y: 0.25, Width: 0.25, Height: -0.5}},
		{name: "zero marker height", marker: Bounds{Width: 0.25, Y: 0.25}, probe: onRow},
		{name: "NaN probe height", marker: marker, probe: Bounds{X: 0.5, Y: 0.25, Width: 0.25, Height: nan}},
		{name: "NaN probe X", marker: marker, probe: Bounds{X: nan, Y: 0.25, Width: 0.25, Height: 0.5}},
		{name: "NaN marker Y", marker: Bounds{Y: nan, Width: 0.25, Height: 0.5}, probe: onRow},
		{name: "infinite marker height", marker: Bounds{Y: 0.25, Width: 0.25, Height: inf}, probe: onRow},
		{name: "infinite probe height", marker: marker, probe: Bounds{X: 0.5, Y: 0.25, Width: 0.25, Height: inf}},
		{name: "infinite probe X", marker: marker, probe: Bounds{X: inf, Y: 0.25, Width: 0.25, Height: 0.5}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			regions := []Region{
				{Text: "Password:", Bounds: tt.marker},
				{Text: "Next step"},
				{Text: "Second step"},
				{Text: probe, Bounds: tt.probe},
			}
			got := extractRegions(t, Options{}, regions).Sections[3].Content
			if want := tt.wantRedacted; want != (got == "[redacted]") {
				t.Fatalf("probe content = %q, want redacted %v", got, want)
			}
		})
	}
}
