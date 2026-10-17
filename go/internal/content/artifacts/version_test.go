// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"fmt"
	"strings"
	"testing"
)

// extractorPin is one pinned triple of the extractor-version guard: an
// ExtractorVersion, the digest of the committed guard corpus, and the digest
// of the derive's output over that corpus at that version.
type extractorPin struct {
	Version      int
	CorpusDigest string
	OutputDigest string
}

// checkExtractorPins reports whether the computed corpus and output digests
// and the current ExtractorVersion agree with the pin history. pins is the
// full history, oldest first: versions 1, 2, 3 and so on, one row per bump,
// and the last row is the current triple. A nil error means it matches.
//
// The corpus is checked first, because an output digest over another corpus
// says nothing about the extractors. Then the version and the output digest
// are checked as a pair: a changed output at the pinned version is an
// unversioned output change, a changed version with the pinned output is a
// bump with no output change, and a changed version with a new output that
// the history does not hold is a bump whose pin was not appended.
func checkExtractorPins(corpusDigest, outputDigest string, version int, pins []extractorPin) error {
	if len(pins) == 0 {
		return fmt.Errorf("the extractor pin history is empty; pin {Version: %d, CorpusDigest: %q, OutputDigest: %q}", version, corpusDigest, outputDigest)
	}
	for i, pin := range pins {
		if pin.Version != i+1 {
			return fmt.Errorf("pin %d has version %d, want %d: the history holds one row per version, oldest first", i, pin.Version, i+1)
		}
		if i > 0 && pin.CorpusDigest == pins[i-1].CorpusDigest && pin.OutputDigest == pins[i-1].OutputDigest {
			return fmt.Errorf("pins at versions %d and %d hold the same output digest %s over the same corpus: a bump needs an output change", pins[i-1].Version, pin.Version, pin.OutputDigest)
		}
	}
	last := pins[len(pins)-1]
	switch {
	case corpusDigest != last.CorpusDigest:
		return fmt.Errorf("guard corpus changed: corpus digest %s != pinned %s; testdata/guard changes only through the re-snapshot procedure "+
			"(package README, \"Extractor version and its guard\"): restore the committed snapshot, run the guard on it, regenerate with "+
			"-update-extractor-corpus, then set the version %d row to {CorpusDigest: %q} and its new output digest, with no extractor change in the same commit",
			corpusDigest, last.CorpusDigest, last.Version, corpusDigest)
	case version == last.Version && outputDigest == last.OutputDigest:
		return nil
	case version == last.Version:
		return fmt.Errorf("extractor output changed: digest %s != pinned %s; bump ExtractorVersion (now %d) and update the pinned pair: "+
			"set ExtractorVersion to %d and append {Version: %d, CorpusDigest: %q, OutputDigest: %q}",
			outputDigest, last.OutputDigest, version, version+1, version+1, corpusDigest, outputDigest)
	case version < last.Version:
		return fmt.Errorf("ExtractorVersion is %d, below the pinned version %d: the pin was appended but the constant was not bumped; set ExtractorVersion to %d",
			version, last.Version, last.Version)
	case outputDigest == last.OutputDigest:
		return fmt.Errorf("ExtractorVersion is %d but the output digest %s equals the pin at version %d: the output did not change, so revert the bump (it would re-derive every repository for nothing)",
			version, outputDigest, last.Version)
	case version != last.Version+1:
		return fmt.Errorf("ExtractorVersion is %d, pinned version is %d: bump by one, to %d, and append {Version: %d, CorpusDigest: %q, OutputDigest: %q}",
			version, last.Version, last.Version+1, last.Version+1, corpusDigest, outputDigest)
	default:
		return fmt.Errorf("ExtractorVersion was bumped to %d without updating the pinned pair: append {Version: %d, CorpusDigest: %q, OutputDigest: %q}",
			version, version, corpusDigest, outputDigest)
	}
}

// TestCheckExtractorPins is the seeded RED/GREEN pair of the guard's check:
// each planted violation must be reported with the instruction a developer
// needs, and each consistent triple must pass.
func TestCheckExtractorPins(t *testing.T) {
	const c1, c2 = "corpus-one", "corpus-two"
	const d1, d2, d3 = "digest-one", "digest-two", "digest-three"
	history := []extractorPin{{Version: 1, CorpusDigest: c1, OutputDigest: d1}}
	bumped := []extractorPin{{Version: 1, CorpusDigest: c1, OutputDigest: d1}, {Version: 2, CorpusDigest: c1, OutputDigest: d2}}
	cases := []struct {
		name    string
		corpus  string
		digest  string
		version int
		pins    []extractorPin
		wantErr string
	}{
		{name: "green: unchanged output at the pinned version", corpus: c1, digest: d1, version: 1, pins: history},
		{name: "green: bump with the pin appended", corpus: c1, digest: d2, version: 2, pins: bumped},
		{
			name: "green: re-snapshot recorded on the current row", corpus: c2, digest: d3, version: 1,
			pins: []extractorPin{{Version: 1, CorpusDigest: c2, OutputDigest: d3}},
		},
		{
			name: "red: changed output without a bump", corpus: c1, digest: d2, version: 1, pins: history,
			wantErr: "extractor output changed: digest digest-two != pinned digest-one; bump ExtractorVersion (now 1) and update the pinned pair",
		},
		{
			name: "red: bump without updating the pin", corpus: c1, digest: d2, version: 2, pins: history,
			wantErr: "ExtractorVersion was bumped to 2 without updating the pinned pair",
		},
		{
			name: "red: bump with an unchanged output", corpus: c1, digest: d1, version: 2, pins: history,
			wantErr: "the output did not change, so revert the bump",
		},
		{
			name: "red: pin appended but the constant not bumped", corpus: c1, digest: d2, version: 1, pins: bumped,
			wantErr: "ExtractorVersion is 1, below the pinned version 2: the pin was appended but the constant was not bumped",
		},
		{
			name: "red: changed output after a bump, at the bumped version", corpus: c1, digest: d3, version: 2, pins: bumped,
			wantErr: "extractor output changed: digest digest-three != pinned digest-two; bump ExtractorVersion (now 2)",
		},
		{
			name: "red: version skipped", corpus: c1, digest: d3, version: 3, pins: history,
			wantErr: "bump by one, to 2",
		},
		{
			name: "red: hand-edited snapshot", corpus: c2, digest: d1, version: 1, pins: history,
			wantErr: "guard corpus changed: corpus digest corpus-two != pinned corpus-one",
		},
		{
			name: "red: hand-edited snapshot hides an output change", corpus: c2, digest: d2, version: 1, pins: history,
			wantErr: "guard corpus changed",
		},
		{
			name: "red: history edited in place", corpus: c1, digest: d2, version: 2,
			pins:    []extractorPin{{Version: 2, CorpusDigest: c1, OutputDigest: d2}},
			wantErr: "pin 0 has version 2, want 1",
		},
		{
			name: "red: two versions share an output over one corpus", corpus: c1, digest: d1, version: 2,
			pins:    []extractorPin{{Version: 1, CorpusDigest: c1, OutputDigest: d1}, {Version: 2, CorpusDigest: c1, OutputDigest: d1}},
			wantErr: "a bump needs an output change",
		},
		{
			name: "red: empty history", corpus: c1, digest: d1, version: 1, pins: nil,
			wantErr: "the extractor pin history is empty",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkExtractorPins(tc.corpus, tc.digest, tc.version, tc.pins)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("checkExtractorPins() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("checkExtractorPins() = nil, want an error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("checkExtractorPins() = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}
