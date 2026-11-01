// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/preflight/picture"
)

const labelValueCanary = "Hunter2CanarySecret"

// stackedRegions builds one region per line in reading order, each on its own
// row, so no two regions share a row unless a test says so.
func stackedRegions(lines ...string) []Region {
	regions := make([]Region, 0, len(lines))
	for i, text := range lines {
		regions = append(regions, Region{
			RegionID:   fmt.Sprintf("r%d", i+1),
			Text:       text,
			Bounds:     Bounds{X: 0.05, Y: 0.02 + float64(i)*0.08, Width: 0.40, Height: 0.05},
			Confidence: 0.95,
		})
	}
	return regions
}

func extractRegions(t *testing.T, options Options, regions []Region) Result {
	t.Helper()
	engine := &fakeEngine{result: EngineResult{Regions: regions}}
	result, err := Extract(context.Background(), testRequestWithOptions("docs/form.png", encodePNG(t, 2, 2), engine, options))
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}
	return result
}

// persistedContents returns each section's content in order.
func persistedContents(result Result) []string {
	contents := make([]string, 0, len(result.Sections))
	for _, section := range result.Sections {
		contents = append(contents, section.Content)
	}
	return contents
}

func TestExtractRedactsValuesThatFollowALabelInReadingOrder(t *testing.T) {
	t.Parallel()

	const redacted = "[redacted]"
	tests := []struct {
		name    string
		regions []Region
		want    []string
	}{
		{
			name:    "value directly below label",
			regions: stackedRegions("Password:", labelValueCanary),
			want:    []string{redacted, redacted},
		},
		{
			name:    "label without a colon, as a boxed form renders it",
			regions: stackedRegions("Password", labelValueCanary),
			want:    []string{redacted, redacted},
		},
		{
			name:    "short label ending in a marker",
			regions: stackedRegions("Confirm password", labelValueCanary),
			want:    []string{redacted, redacted},
		},
		{
			name:    "long label that a colon marks as a label",
			regions: stackedRegions("Enter your new database password:", labelValueCanary),
			want:    []string{redacted, redacted},
		},
		{
			name:    "label with a note before the separator",
			regions: stackedRegions("Password (required):", labelValueCanary),
			want:    []string{redacted, redacted},
		},
		{
			name:    "upper case label with an equals sign",
			regions: stackedRegions("API_KEY =", labelValueCanary),
			want:    []string{redacted, redacted},
		},
		{
			name:    "whitespace-only region between label and value",
			regions: stackedRegions("Password:", "   ", labelValueCanary),
			want:    []string{redacted, redacted},
		},
		{
			name:    "blank regions do not use up the window",
			regions: stackedRegions("Password:", " ", "\t", labelValueCanary),
			want:    []string{redacted, redacted},
		},
		{
			name:    "punctuation noise does not use up the window",
			regions: stackedRegions("Password:", "|", "_", "--", labelValueCanary),
			want:    []string{redacted, redacted, redacted, redacted, redacted},
		},
		{
			name:    "a value of symbols only is withheld and keeps the window open",
			regions: stackedRegions("Password:", "*!#$%", "Release checklist", "Next step", "Rollback steps"),
			want:    []string{redacted, redacted, redacted, redacted, "Rollback steps"},
		},
		{
			name:    "label with a parenthetical note",
			regions: stackedRegions("Password (required)", labelValueCanary),
			want:    []string{redacted, redacted},
		},
		{
			name:    "label with a separator and a parenthetical note",
			regions: stackedRegions("Password: (min 12 characters)", labelValueCanary),
			want:    []string{redacted, redacted},
		},
		{
			name:    "one caption line between label and value",
			regions: stackedRegions("Password:", "at least 12 characters", labelValueCanary),
			want:    []string{redacted, redacted, redacted},
		},
		{
			name:    "two stacked labels then two values",
			regions: stackedRegions("Password:", "api_key:", labelValueCanary, "SecondCanaryValue"),
			want:    []string{redacted, redacted, redacted, redacted},
		},
		{
			name:    "label is the last region",
			regions: stackedRegions("Release checklist", "Password:"),
			want:    []string{"Release checklist", redacted},
		},
		{
			name:    "value is itself a marker and still uses the window",
			regions: stackedRegions("Password:", "alice@example.com", "Release checklist", "Rollback steps"),
			want:    []string{redacted, redacted, redacted, "Rollback steps"},
		},
		{
			name:    "window ends two regions after the label",
			regions: stackedRegions("Password:", labelValueCanary, "Release checklist", "Rollback steps"),
			want:    []string{redacted, redacted, redacted, "Rollback steps"},
		},
		{
			name:    "a second label adds to the window",
			regions: stackedRegions("Password:", labelValueCanary, "api_key:", "SecondCanaryValue", "Next line", "Rollback steps"),
			want:    []string{redacted, redacted, redacted, redacted, redacted, "Rollback steps"},
		},
		{
			name:    "three stacked labels then three values",
			regions: stackedRegions("Password:", "api_key:", "private_key:", labelValueCanary, "SecondCanaryValue", "ThirdCanaryValue", "Next line", "Rollback steps"),
			want:    []string{redacted, redacted, redacted, redacted, redacted, redacted, redacted, "Rollback steps"},
		},
		{
			name:    "region before the label stays",
			regions: stackedRegions("Release checklist", "Password:", labelValueCanary),
			want:    []string{"Release checklist", redacted, redacted},
		},
		{
			name:    "ordinary lines stay",
			regions: stackedRegions("Release checklist", "Rollback steps", "Ask the platform team"),
			want:    []string{"Release checklist", "Rollback steps", "Ask the platform team"},
		},
		{
			name:    "any marker opens the window, even with text after it",
			regions: stackedRegions("Password required", labelValueCanary, "Next step", "Rollback steps"),
			want:    []string{redacted, redacted, redacted, "Rollback steps"},
		},
		{
			name:    "marker with OCR garble after it opens the window",
			regions: stackedRegions("Password: I", labelValueCanary),
			want:    []string{redacted, redacted},
		},
		{
			name:    "marker inside parentheses opens the window",
			regions: stackedRegions("(Password)", labelValueCanary),
			want:    []string{redacted, redacted},
		},
		{
			name:    "marker in prose opens the same bounded window",
			regions: stackedRegions("Please rotate every staging password", "Next step", "Second step", "Rollback steps"),
			want:    []string{redacted, redacted, redacted, "Rollback steps"},
		},
		{
			name:    "value on the marker line opens the same bounded window",
			regions: stackedRegions("password: "+labelValueCanary, "Next step", "Second step", "Rollback steps"),
			want:    []string{redacted, redacted, redacted, "Rollback steps"},
		},
		{
			name:    "personal data pattern is not a label",
			regions: stackedRegions("alice@example.com:", "Release checklist"),
			want:    []string{redacted, "Release checklist"},
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

// TestExtractFollowsEngineRegionOrder pins that Extract never sorts: reading
// order is the engine's contract, because layout order beats a sort by Y on a
// multi-column page. Sections keep engine order, and the window follows it.
func TestExtractFollowsEngineRegionOrder(t *testing.T) {
	t.Parallel()

	regions := []Region{
		{RegionID: "low", Text: "Rollback steps", Bounds: Bounds{X: 0.05, Y: 0.80, Width: 0.40, Height: 0.05}},
		{RegionID: "high", Text: "Password:", Bounds: Bounds{X: 0.05, Y: 0.10, Width: 0.40, Height: 0.05}},
		{RegionID: "mid", Text: labelValueCanary, Bounds: Bounds{X: 0.05, Y: 0.50, Width: 0.40, Height: 0.05}},
	}
	result := extractRegions(t, Options{}, regions)

	got := persistedContents(result)
	want := []string{"Rollback steps", "[redacted]", "[redacted]"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("persisted content = %q, want %q", got, want)
	}
	if id := result.Sections[0].SectionID; id != "ocr:low" {
		t.Fatalf("first section = %q, want the first engine region", id)
	}
}

// TestExtractLabelValueRedactionStaysTruthful checks the facts a caller counts:
// a withheld value is one more redacted section, marked exactly like a marker
// hit, and the section and region counts do not change.
func TestExtractLabelValueRedactionStaysTruthful(t *testing.T) {
	t.Parallel()

	regions := stackedRegions("Release checklist", "Password:", labelValueCanary, "Next step", "Rollback steps")
	regions[2].Confidence = 0.30
	result := extractRegions(t, Options{}, regions)

	if got, want := len(result.Sections), len(regions); got != want {
		t.Fatalf("sections = %d, want %d", got, want)
	}
	if got := result.Document.SourceMetadata["ocr_region_count"]; got != strconv.Itoa(len(regions)) {
		t.Fatalf("ocr_region_count = %q, want %d", got, len(regions))
	}
	redactedCount := 0
	for _, section := range result.Sections {
		if section.SourceMetadata["redacted"] == "true" {
			redactedCount++
		}
	}
	if redactedCount != 3 {
		t.Fatalf("redacted sections = %d, want 3", redactedCount)
	}

	value := result.Sections[2]
	for key, want := range map[string]string{
		"redacted":        "true",
		"redaction_class": string(picture.WarningSensitiveValueRedacted),
	} {
		if got := value.SourceMetadata[key]; got != want {
			t.Fatalf("value source_metadata[%q] = %q, want %q", key, got, want)
		}
	}
	warning := value.SourceMetadata["warning"]
	if !strings.Contains(warning, string(picture.WarningSensitiveValueRedacted)) {
		t.Fatalf("warning = %q, want sensitive redaction", warning)
	}
	if !strings.Contains(warning, "ocr_low_confidence") {
		t.Fatalf("warning = %q, want the low confidence warning kept", warning)
	}
	if !value.ContainsWarnings {
		t.Fatal("ContainsWarnings = false, want true on a withheld value")
	}
	if value.TextHash == "" || value.ExcerptHash != hashText("[redacted]") {
		t.Fatalf("hashes = (%q, %q), want a text hash and the redacted excerpt hash", value.TextHash, value.ExcerptHash)
	}
	if got := result.Sections[4].SourceMetadata["redacted"]; got != "" {
		t.Fatalf("region past the window redacted = %q, want unset", got)
	}

	persisted, err := json.Marshal(result.Envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	if bytes.Contains(persisted, []byte(labelValueCanary)) {
		t.Fatal("persisted envelopes carry the canary value")
	}
}

// TestExtractRedactsAnOversizedValueInsteadOfTruncatingIt keeps a value that
// exceeds the section cap out of the truncation path, which would persist its
// first characters.
func TestExtractRedactsAnOversizedValueInsteadOfTruncatingIt(t *testing.T) {
	t.Parallel()

	result := extractRegions(t, Options{MaxSectionChars: 4}, stackedRegions("Password:", labelValueCanary))
	value := result.Sections[1]
	if value.Content != "[redacted]" {
		t.Fatalf("value content = %q, want [redacted]", value.Content)
	}
	if strings.Contains(value.SourceMetadata["warning"], "partial_extraction") {
		t.Fatalf("warning = %q, want no partial extraction on a redacted value", value.SourceMetadata["warning"])
	}
}

// BenchmarkBuildSectionsLabelHeavy is the worst case for the row clause: half
// the regions are labels, and the rest come before them, outside every window,
// so each one is compared with every label.
func BenchmarkBuildSectionsLabelHeavy(b *testing.B) {
	lines := make([]string, 500)
	for i := range lines {
		lines[i] = "Deploy the Checkout Service to staging"
		if i >= len(lines)/2 {
			lines[i] = "Password:"
		}
	}
	benchmarkBuildSections(b, stackedRegions(lines...))
}

// BenchmarkBuildSectionsNoLabels is the common case: ordinary text.
func BenchmarkBuildSectionsNoLabels(b *testing.B) {
	lines := make([]string, 500)
	for i := range lines {
		lines[i] = "Deploy the Checkout Service to staging"
	}
	benchmarkBuildSections(b, stackedRegions(lines...))
}

func benchmarkBuildSections(b *testing.B, regions []Region) {
	b.Helper()
	document := buildDocument(Request{DocumentID: "doc:bench"}, picture.Result{}, "sha256:bench")
	result := EngineResult{EngineName: "bench", EngineVersion: "1", Regions: regions}
	b.ReportAllocs()
	for b.Loop() {
		if sections := buildSections(Request{}, document, result, "sha256:bench"); len(sections) != len(regions) {
			b.Fatalf("sections = %d, want %d", len(sections), len(regions))
		}
	}
}
