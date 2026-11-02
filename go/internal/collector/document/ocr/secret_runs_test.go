// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocr

import (
	"strings"
	"testing"
	"time"
)

// runLine and runLineB are 16-character tokens with a digit: the shortest line
// that continues a run.
const (
	runLine  = "Ab3dEf6hIj9lMn2p"
	runLineB = "Qr5tUv8xYz1bCd4e"
	// seedLine is 40 characters with a capital, a lowercase letter, and a digit.
	seedLine = "Ab3dEf6hIj9lMn2pQr5tUv8xYz1bCd4eFg7hJk0m"
)

func TestExtractContinuesASecretRunPastTheWindow(t *testing.T) {
	t.Parallel()

	const redacted = "[redacted]"
	tests := []struct {
		name    string
		regions []Region
		want    []string
	}{
		{
			name:    "run tokens after the window are withheld, prose after them is not",
			regions: stackedRegions("Password:", "x1", "y2", runLine, runLineB, "Rollback steps"),
			want:    []string{redacted, redacted, redacted, redacted, redacted, "Rollback steps"},
		},
		{
			name:    "a prose line ends the run, and a token after it stays",
			regions: stackedRegions("Password:", "x1", "y2", runLine, "Release notes here", runLineB),
			want:    []string{redacted, redacted, redacted, redacted, "Release notes here", runLineB},
		},
		{
			name:    "the short last line of a body is withheld, and the line after it is not",
			regions: stackedRegions("Password:", runLine, runLineB, "Qk9x==", "Rollback"),
			want:    []string{redacted, redacted, redacted, redacted, "Rollback"},
		},
		{
			name:    "a tail slot holds one region only",
			regions: stackedRegions("Password:", runLine, runLineB, "Qk9x==", "Rollback", "Notes"),
			want:    []string{redacted, redacted, redacted, redacted, "Rollback", "Notes"},
		},
		{
			name:    "one withheld value does not hide the single word under it",
			regions: stackedRegions("Password:", "x1", "y2", runLine, "platform", "Rollback steps"),
			want:    []string{redacted, redacted, redacted, redacted, "platform", "Rollback steps"},
		},
		{
			name:    "a noise region inside the run does not end it",
			regions: stackedRegions("Password:", "x1", "y2", runLine, "|", runLineB, "Rollback steps"),
			want:    []string{redacted, redacted, redacted, redacted, redacted, redacted, "Rollback steps"},
		},
		{
			name:    "a noise region between tokens inside the window does not end the run",
			regions: stackedRegions("Password:", runLine, "|", runLineB, "_", "Ab3dEf6hIj9lMn2pQ", "Rollback steps"),
			want:    []string{redacted, redacted, redacted, redacted, redacted, redacted, "Rollback steps"},
		},
		{
			name:    "a blank region inside the run does not end it",
			regions: stackedRegions("Password:", "x1", "y2", runLine, "   ", runLineB, "Rollback steps"),
			want:    []string{redacted, redacted, redacted, redacted, redacted, "Rollback steps"},
		},
		{
			name:    "a line with a misread character is still a line of the key",
			regions: stackedRegions("Password:", "x1", "y2", "Ab3d|f6hIj9lMn2pQr/", runLineB, "Rollback steps"),
			want:    []string{redacted, redacted, redacted, redacted, redacted, "Rollback steps"},
		},
		{
			name:    "a token one character short does not continue the run",
			regions: stackedRegions("Password:", "x1", "y2", runLine[:15], "Rollback steps"),
			want:    []string{redacted, redacted, redacted, runLine[:15], "Rollback steps"},
		},
		{
			name:    "a long word with no digit or key symbol does not continue the run",
			regions: stackedRegions("Password:", "x1", "y2", "abcdefghijklmnop", "Rollback steps"),
			want:    []string{redacted, redacted, redacted, "abcdefghijklmnop", "Rollback steps"},
		},
		{
			name:    "a line with a space does not continue the run",
			regions: stackedRegions("Password:", "x1", "y2", "Ab3dEf6h Ij9lMn2p", "Rollback steps"),
			want:    []string{redacted, redacted, redacted, "Ab3dEf6h Ij9lMn2p", "Rollback steps"},
		},
		{
			name:    "a long token with no label before it stays",
			regions: stackedRegions("Release checklist", runLine, runLineB, "Rollback steps"),
			want:    []string{"Release checklist", runLine, runLineB, "Rollback steps"},
		},
		{
			name:    "a seed line starts a run with no label",
			regions: stackedRegions("Release checklist", seedLine, runLine, "Qk9x==", "Rollback steps"),
			want:    []string{"Release checklist", redacted, redacted, redacted, "Rollback steps"},
		},
		{
			name:    "a seed line alone takes the short line under it",
			regions: stackedRegions("Release checklist", seedLine, "Qk9x==", "Rollback steps"),
			want:    []string{"Release checklist", redacted, redacted, "Rollback steps"},
		},
		{
			name:    "a symbol region after a kept line is kept",
			regions: stackedRegions("Release checklist", "|", runLine),
			want:    []string{"Release checklist", "|", runLine},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := persistedContents(extractRegions(t, Options{}, tt.regions))
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("persisted contents = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestExtractContinuesAColumnValueThatWrapsBelowItsRow covers a value block in
// the column beside its label. The row clause withholds the first line, which
// shares the label's row, and the later lines sit on rows of their own. The window withholds the two
// labels after Token:, and Region stays.
func TestExtractContinuesAColumnValueThatWrapsBelowItsRow(t *testing.T) {
	t.Parallel()

	const redacted = "[redacted]"
	left := func(text string, y float64) Region {
		return Region{Text: text, Bounds: Bounds{X: 0.05, Y: y, Width: 0.20, Height: 0.04}, Confidence: 0.95}
	}
	right := func(text string, y float64) Region {
		return Region{Text: text, Bounds: Bounds{X: 0.40, Y: y, Width: 0.50, Height: 0.04}, Confidence: 0.95}
	}
	regions := []Region{
		left("Token:", 0.10), left("Service", 0.20), left("Owner", 0.30), left("Region", 0.40),
		right(runLine, 0.10), right(runLineB, 0.14), right(runLine+"x", 0.18), right("Qk9x==", 0.22), right("Rollback steps", 0.26),
	}
	got := persistedContents(extractRegions(t, Options{}, regions))
	want := []string{redacted, redacted, redacted, "Region", redacted, redacted, redacted, redacted, "Rollback steps"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("persisted contents = %q, want %q", got, want)
	}
}

func TestSecretTokenShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		run  bool
		seed bool
	}{
		{name: "16 characters with a digit", text: runLine, run: true},
		{name: "15 characters", text: runLine[:15]},
		{name: "plus sign in place of a digit", text: "AbcdEfghIjklMn+p", run: true},
		{name: "slash in place of a digit", text: "AbcdEfghIjklMn/p", run: true},
		{name: "equals sign in place of a digit", text: "AbcdEfghIjklMnp=", run: true},
		{name: "letters only", text: "AbcdEfghIjklMnop"},
		{name: "space inside", text: "Ab3dEf6h Ij9lMn2p"},
		{name: "40 characters of all three classes", text: seedLine, run: true, seed: true},
		{name: "39 characters", text: seedLine[:39], run: true},
		{name: "a dot inside, as in a JWT", text: seedLine[:16] + "." + seedLine[17:], run: true, seed: true},
		{name: "a hyphen inside, as in base64url", text: seedLine[:16] + "-" + seedLine[17:], run: true, seed: true},
		{name: "an underscore inside, as in base64url", text: seedLine[:16] + "_" + seedLine[17:], run: true, seed: true},
		{name: "a plus sign inside, as in base64", text: seedLine[:16] + "+" + seedLine[17:], run: true, seed: true},
		{name: "a slash inside, as in base64", text: seedLine[:16] + "/" + seedLine[17:], run: true, seed: true},
		{name: "an equals sign inside, as in base64", text: seedLine[:16] + "=" + seedLine[17:], run: true, seed: true},
		{name: "40 characters without a digit", text: strings.Repeat("Ab", 20)},
		{name: "40 characters without a capital", text: strings.ToLower(seedLine), run: true},
		{name: "40 characters without a lowercase letter", text: strings.ToUpper(seedLine), run: true},
		{name: "40 characters with a character outside the set", text: seedLine[:39] + "!", run: true},
		{name: "a long path has no capital", text: "docs/internal/design/9799-ocr-multiline.md", run: true},
		{name: "a hex digest has no capital", text: strings.Repeat("0f9bba8d", 8), run: true},
		{name: "an upper case variable has no lowercase letter", text: "ESHU_DR_COMPOSE_PROJECT_9799_REBUILD_PROOF", run: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isRunToken(tt.text); got != tt.run {
				t.Errorf("isRunToken(%q) = %v, want %v", tt.text, got, tt.run)
			}
			if got := isSeedToken(tt.text); got != tt.seed {
				t.Errorf("isSeedToken(%q) = %v, want %v", tt.text, got, tt.seed)
			}
		})
	}
}

// TestExtendSecretRunsIsLinear pins the time of the run scan on a page of
// 200,000 token lines after one label, and on one 1 MiB token. Each region is
// read a fixed number of times, so both finish in milliseconds. The bound is far
// above that and far below quadratic.
func TestExtendSecretRunsIsLinear(t *testing.T) {
	t.Parallel()

	const count = 200_000
	regions := make([]Region, 0, count+1)
	regions = append(regions, Region{Text: "Password:"})
	for range count {
		regions = append(regions, Region{Text: runLine})
	}
	start := time.Now()
	values := labelValueRegions(regions)
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("labelValueRegions on %d token lines took %v, want linear time", count, elapsed)
	}
	if !values[count] {
		t.Fatalf("last token line was kept, want the run to reach it")
	}

	big := strings.Repeat("Ab3d", 1<<18)
	start = time.Now()
	_, _ = isRunToken(big), isSeedToken(big)
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("token shape checks on %d bytes took %v, want linear time", len(big), elapsed)
	}
}
