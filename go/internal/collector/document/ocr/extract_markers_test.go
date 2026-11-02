// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocr

import (
	"strings"
	"testing"
)

// extractLabels are the labels, one per family and shape, that Extract must
// treat as a label: the value after each one is withheld in both layouts, and
// a value on the same line goes with its label.
var extractLabels = []string{
	"Secret:", "Client Secret:", "client-secret", "API Key:", "API-Key", "api_key =", "apikey", "APIKey:",
	"Token:", "Access Token", "GITHUB_TOKEN", "Credential:", "Credentials", "Passphrase:", "Pass phrase",
	"Passcode:", "PIN:", "Pin", "PWD:", "Passwd:", "Access Key:", "AWS_ACCESS_KEY_ID", "Private Key:",
	"Passw0rd:", "P@ssword", "Pa55word:", "S3cret:", "T0ken:",
}

func TestExtractRedactsTheValueAfterEveryLabelFamilyStacked(t *testing.T) {
	t.Parallel()

	for _, label := range extractLabels {
		label := label
		t.Run(label, func(t *testing.T) {
			t.Parallel()

			result := extractRegions(t, Options{}, stackedRegions(label, labelValueCanary, "Release checklist", "Rollback steps"))
			want := []string{"[redacted]", "[redacted]", "[redacted]", "Rollback steps"}
			if got := persistedContents(result); strings.Join(got, "|") != strings.Join(want, "|") {
				t.Fatalf("persisted content = %q, want %q", got, want)
			}
		})
	}
}

// TestExtractRedactsTheValueAfterEveryLabelFamilyInAColumn puts the value in
// the next column, many regions after its label and on the label's row, which
// only the row clause reaches.
func TestExtractRedactsTheValueAfterEveryLabelFamilyInAColumn(t *testing.T) {
	t.Parallel()

	for _, label := range extractLabels {
		label := label
		t.Run(label, func(t *testing.T) {
			t.Parallel()

			regions := []Region{
				{Text: label, Bounds: Bounds{X: 0.02, Y: 0.26, Width: 0.11, Height: 0.05}},
				{Text: "Filler one", Bounds: Bounds{X: 0.02, Y: 0.44, Width: 0.45, Height: 0.05}},
				{Text: "Filler two", Bounds: Bounds{X: 0.02, Y: 0.53, Width: 0.45, Height: 0.05}},
				{Text: "Filler three", Bounds: Bounds{X: 0.02, Y: 0.62, Width: 0.45, Height: 0.05}},
				{Text: labelValueCanary, Bounds: Bounds{X: 0.53, Y: 0.26, Width: 0.30, Height: 0.05}},
				{Text: "Rotate them after an incident", Bounds: Bounds{X: 0.53, Y: 0.62, Width: 0.45, Height: 0.05}},
			}
			want := []string{"[redacted]", "[redacted]", "[redacted]", "Filler three", "[redacted]", "Rotate them after an incident"}
			if got := persistedContents(extractRegions(t, Options{}, regions)); strings.Join(got, "|") != strings.Join(want, "|") {
				t.Fatalf("persisted content = %q, want %q", got, want)
			}
		})
	}
}

// TestExtractRedactsALabelWithItsValueOnOneLine covers the largest hole #9793
// named: `API Key: value` carried no marker word, so the region was kept whole.
func TestExtractRedactsALabelWithItsValueOnOneLine(t *testing.T) {
	t.Parallel()

	for _, label := range extractLabels {
		label := label
		t.Run(label, func(t *testing.T) {
			t.Parallel()

			line := strings.TrimSpace(label) + " " + labelValueCanary
			result := extractRegions(t, Options{}, stackedRegions(line))
			if got := persistedContents(result); len(got) != 1 || got[0] != "[redacted]" {
				t.Fatalf("persisted content = %q, want [redacted]", got)
			}
			if envelopesContain(result, labelValueCanary) {
				t.Fatalf("canary reached an envelope")
			}
		})
	}
}

// TestExtractKeepsOrdinaryTextThatHoldsNoLabel is the cost side of the wider
// list: prose that holds a marker word as part of a longer word, or a key that
// is not secret, opens no window and stays.
func TestExtractKeepsOrdinaryTextThatHoldsNoLabel(t *testing.T) {
	t.Parallel()

	got := persistedContents(extractRegions(t, Options{}, stackedRegions(ordinaryLines...)))
	if strings.Join(got, "|") != strings.Join(ordinaryLines, "|") {
		t.Fatalf("persisted content = %q, want every ordinary line unchanged", got)
	}
}

// TestExtractWholeWordMarkerCostsTheBoundedWindow records the accepted cost: a
// whole-word `pin` or `token` in prose is a marker like `password` in prose,
// so it hides the two lines after it and no more.
func TestExtractWholeWordMarkerCostsTheBoundedWindow(t *testing.T) {
	t.Parallel()

	got := persistedContents(extractRegions(t, Options{}, stackedRegions("Pin the base image", "Next step", "Second step", "Rollback steps")))
	want := []string{"[redacted]", "[redacted]", "[redacted]", "Rollback steps"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("persisted content = %q, want %q", got, want)
	}
}

func envelopesContain(result Result, needle string) bool {
	for _, section := range result.Sections {
		for _, value := range section.SourceMetadata {
			if strings.Contains(value, needle) {
				return true
			}
		}
		if strings.Contains(section.Content, needle) {
			return true
		}
	}
	return false
}
