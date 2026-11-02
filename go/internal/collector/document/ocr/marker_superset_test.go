// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocr

import (
	"strings"
	"testing"
	"time"
)

// TestContainsMarkerWordIsASupersetOfTheSourceNameList pins that the word
// rule never redacts less than the raw substring list it replaced: every
// source-name marker, in any case, with a letter, digit, or underscore glued
// to either side, is still a marker in OCR text.
func TestContainsMarkerWordIsASupersetOfTheSourceNameList(t *testing.T) {
	t.Parallel()

	prefixes := []string{"", "x", "my_", "9", "A", "STRIPE_", "-", "."}
	suffixes := []string{"", "1", "2", "s", "id", "ID", "_file", "Z", "_9", "file"}
	for _, marker := range identityMarkers {
		for _, form := range []string{marker, strings.ToUpper(marker), strings.ToTitle(marker[:1]) + marker[1:]} {
			for _, prefix := range prefixes {
				for _, suffix := range suffixes {
					text := prefix + form + suffix
					if !containsMarkerWord(text) {
						t.Errorf("containsMarkerWord(%q) = false, want true: the source-name list flags it", text)
					}
				}
			}
		}
	}
}

// TestContainsMarkerWordKeepsTheLabelsTheOldListCaught lists the probes a
// review found: a label with a digit or letters after key, or a case change
// inside it, that the word rule alone split away from its phrase.
func TestContainsMarkerWordKeepsTheLabelsTheOldListCaught(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"API_KEY2: sk_live_abc123", "STRIPE_API_KEY1=sk_live_abc123", "PRIVATE_KEY1:", "API_KEYID", "private_keyfile",
		"API_kEY", "API_keyS", "Private_KeyFile", "x_api_key_x",
	} {
		if !containsMarkerWord(text) {
			t.Errorf("containsMarkerWord(%q) = false, want true", text)
		}
	}
}

func TestExtractRedactsAPrivateKeyLabelWithADigitAndItsContinuation(t *testing.T) {
	t.Parallel()

	regions := stackedRegions("PRIVATE_KEY1:", "MIIEvQIBADANBgkqhkiG9w0BAQEF", "AASCBKcwggSjAgEAAoIBAQC7", "Rollback steps")
	want := []string{"[redacted]", "[redacted]", "[redacted]", "Rollback steps"}
	if got := persistedContents(extractRegions(t, Options{}, regions)); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("persisted content = %q, want %q", got, want)
	}
}

// TestContainsMarkerWordIsLinear pins the time bound on text that is all
// symbols. A run of @ or $ that is not inside a word was once rescanned from
// every position, so one hostile region cost seconds. A linear scan takes
// milliseconds on 1 MiB; the bound is far above that and far below quadratic.
func TestContainsMarkerWordIsLinear(t *testing.T) {
	t.Parallel()

	const size = 1 << 20
	inputs := map[string]string{
		"dollars":     strings.Repeat("$", size),
		"ats":         strings.Repeat("@", size),
		"alternating": strings.Repeat("$@", size/2),
		"after word":  "a" + strings.Repeat("$", size),
		"between":     "a" + strings.Repeat("@$", size/2) + "b",
		"spaced":      strings.Repeat("$ @ ", size/4),
		"one word":    strings.Repeat("a", size),
		"camel":       strings.Repeat("aB", size/2),
		"digits":      strings.Repeat("1", size),
		"glued key":   strings.Repeat("api_", size/4),
	}
	for name, text := range inputs {
		name, text := name, text
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			containsMarkerWord(text)
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Fatalf("containsMarkerWord on %d bytes of %s took %v, want linear time", len(text), name, elapsed)
			}
		})
	}
}
