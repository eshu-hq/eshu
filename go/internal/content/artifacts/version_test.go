// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"fmt"
	"strings"
	"testing"
)

// hydrationWindow is the file window a derive reads: the list window
// (ListLimit) and the content allowance (AllowanceWholeWindow). A change to
// either alters which files a large repository contributes. A change that cuts
// no guard corpus repository moves no digest, so the pin records the window
// constants themselves.
type hydrationWindow struct {
	ListLimit            int
	AllowanceWholeWindow int
}

// currentHydrationWindow returns the window the derive runs with now.
func currentHydrationWindow() hydrationWindow {
	return hydrationWindow{ListLimit: ListLimit, AllowanceWholeWindow: AllowanceWholeWindow}
}

// extractorPin is one pinned row of the extractor-version guard: an
// ExtractorVersion, the digest of the committed guard corpus, the digest of
// the derive's output over that corpus at that version, and the hydration
// window the version ran with.
type extractorPin struct {
	Version      int
	CorpusDigest string
	OutputDigest string
	Window       hydrationWindow
}

// literal renders the row as the Go literal a developer appends to
// extractorPins.
func (p extractorPin) literal() string {
	return fmt.Sprintf("{Version: %d, CorpusDigest: %q, OutputDigest: %q, Window: hydrationWindow{ListLimit: %d, AllowanceWholeWindow: %d}}",
		p.Version, p.CorpusDigest, p.OutputDigest, p.Window.ListLimit, p.Window.AllowanceWholeWindow)
}

// checkExtractorPins reports whether the computed corpus and output digests,
// the hydration window and the current ExtractorVersion agree with the pin
// history. pins is the full history, oldest first: versions 1, 2, 3 and so on,
// one row per bump, and the last row is the current row. A nil error means it
// matches.
//
// The corpus is checked first, because an output digest over another corpus
// says nothing about the extractors. Then the version, the window and the
// output digest are checked together: a moved window at the pinned version is
// an unversioned window change (the corpus holds no repository large enough
// to move a digest), a changed output at the pinned version is an unversioned
// output change, a changed version with the pinned output and window is a bump
// with no change, and a changed version with a new output or window that the
// history does not hold is a bump whose pin was not appended.
func checkExtractorPins(corpusDigest, outputDigest string, version int, window hydrationWindow, pins []extractorPin) error {
	if len(pins) == 0 {
		return fmt.Errorf("the extractor pin history is empty; pin %s", extractorPin{Version: version, CorpusDigest: corpusDigest, OutputDigest: outputDigest, Window: window}.literal())
	}
	for i, pin := range pins {
		if pin.Version != i+1 {
			return fmt.Errorf("pin %d has version %d, want %d: the history holds one row per version, oldest first", i, pin.Version, i+1)
		}
		if i > 0 && pin.CorpusDigest == pins[i-1].CorpusDigest && pin.OutputDigest == pins[i-1].OutputDigest && pin.Window == pins[i-1].Window {
			return fmt.Errorf("pins at versions %d and %d hold the same output digest %s and window over the same corpus: a bump needs an output or window change", pins[i-1].Version, pin.Version, pin.OutputDigest)
		}
	}
	last := pins[len(pins)-1]
	next := extractorPin{Version: last.Version + 1, CorpusDigest: corpusDigest, OutputDigest: outputDigest, Window: window}
	switch {
	case corpusDigest != last.CorpusDigest:
		return fmt.Errorf("guard corpus changed: corpus digest %s != pinned %s; testdata/guard changes only by regeneration "+
			"(package README, \"Extractor version and its guard\"). With no extractor change in this commit, restore the committed snapshot, "+
			"run the guard on it, regenerate with -update-extractor-corpus, then set the version %d row to {CorpusDigest: %q} and its new output digest; "+
			"with an extractor change that the new input shows, set ExtractorVersion to %d and append %s",
			corpusDigest, last.CorpusDigest, last.Version, corpusDigest, next.Version, next.literal())
	case version == last.Version && outputDigest == last.OutputDigest && window == last.Window:
		return nil
	case version == last.Version && window != last.Window:
		return fmt.Errorf("hydration window moved: ListLimit %d and AllowanceWholeWindow %d, pinned %d and %d; "+
			"a window that cuts no guard corpus repository moves no digest (one that cuts a corpus repository moves the output digest too), "+
			"but the stored rows of a repository it cuts change. "+
			"Set ExtractorVersion to %d and append %s",
			window.ListLimit, window.AllowanceWholeWindow, last.Window.ListLimit, last.Window.AllowanceWholeWindow, next.Version, next.literal())
	case version == last.Version:
		return fmt.Errorf("extractor output changed: digest %s != pinned %s; bump ExtractorVersion (now %d) and update the pinned pair: "+
			"set ExtractorVersion to %d and append %s",
			outputDigest, last.OutputDigest, version, next.Version, next.literal())
	case version < last.Version:
		return fmt.Errorf("ExtractorVersion is %d, below the pinned version %d: the pin was appended but the constant was not bumped; set ExtractorVersion to %d",
			version, last.Version, last.Version)
	case outputDigest == last.OutputDigest && window == last.Window:
		return fmt.Errorf("ExtractorVersion is %d but the output digest %s equals the pin at version %d: "+
			"the output over the guard corpus did not change; if the change alters rows for an input the corpus lacks, "+
			"add that input to the generator's supplement and regenerate in this change, otherwise revert the bump "+
			"(it would re-derive every repository for nothing); package README, \"Extractor version and its guard\"",
			version, outputDigest, last.Version)
	case version != last.Version+1:
		return fmt.Errorf("ExtractorVersion is %d, pinned version is %d: bump by one, to %d, and append %s",
			version, last.Version, next.Version, next.literal())
	default:
		return fmt.Errorf("ExtractorVersion was bumped to %d without updating the pinned pair: append %s",
			version, extractorPin{Version: version, CorpusDigest: corpusDigest, OutputDigest: outputDigest, Window: window}.literal())
	}
}

// TestCheckExtractorPins is the seeded RED/GREEN pair of the guard's check:
// each planted violation must be reported with the instruction a developer
// needs, and each consistent triple must pass.
func TestCheckExtractorPins(t *testing.T) {
	const c1, c2 = "corpus-one", "corpus-two"
	const d1, d2, d3 = "digest-one", "digest-two", "digest-three"
	// w1 is the window every pin holds unless a case plants another; w2 is a
	// ListLimit change, w3 an allowance change.
	w1 := hydrationWindow{ListLimit: 5000, AllowanceWholeWindow: -1}
	w2 := hydrationWindow{ListLimit: 5001, AllowanceWholeWindow: -1}
	w3 := hydrationWindow{ListLimit: 5000, AllowanceWholeWindow: 0}
	pin := func(version int, corpus, digest string) extractorPin {
		return extractorPin{Version: version, CorpusDigest: corpus, OutputDigest: digest, Window: w1}
	}
	history := []extractorPin{pin(1, c1, d1)}
	bumped := []extractorPin{pin(1, c1, d1), pin(2, c1, d2)}
	cases := []struct {
		name    string
		corpus  string
		digest  string
		version int
		window  hydrationWindow // zero means w1, the window of every default pin
		pins    []extractorPin
		wantErr string
	}{
		{name: "green: unchanged output at the pinned version", corpus: c1, digest: d1, version: 1, pins: history},
		{name: "green: bump with the pin appended", corpus: c1, digest: d2, version: 2, pins: bumped},
		{
			name: "green: re-snapshot recorded on the current row", corpus: c2, digest: d3, version: 1,
			pins: []extractorPin{pin(1, c2, d3)},
		},
		{
			name: "green: bump with a new corpus", corpus: c2, digest: d2, version: 2,
			pins: []extractorPin{pin(1, c1, d1), pin(2, c2, d2)},
		},
		{
			name: "green: window bump with the pin appended, output unchanged", corpus: c1, digest: d1, version: 2, window: w2,
			pins: []extractorPin{pin(1, c1, d1), {Version: 2, CorpusDigest: c1, OutputDigest: d1, Window: w2}},
		},
		{
			name: "green: allowance bump with the pin appended, output unchanged", corpus: c1, digest: d1, version: 2, window: w3,
			pins: []extractorPin{pin(1, c1, d1), {Version: 2, CorpusDigest: c1, OutputDigest: d1, Window: w3}},
		},
		{
			name: "red: ListLimit changed without a bump", corpus: c1, digest: d1, version: 1, window: w2, pins: history,
			wantErr: "Set ExtractorVersion to 2 and append " +
				`{Version: 2, CorpusDigest: "corpus-one", OutputDigest: "digest-one", Window: hydrationWindow{ListLimit: 5001, AllowanceWholeWindow: -1}}`,
		},
		{
			name: "red: AllowanceWholeWindow changed without a bump", corpus: c1, digest: d1, version: 1, window: w3, pins: history,
			wantErr: "hydration window moved: ListLimit 5000 and AllowanceWholeWindow 0, pinned 5000 and -1",
		},
		{
			name: "red: window changed with the output, without a bump", corpus: c1, digest: d2, version: 1, window: w2, pins: history,
			wantErr: "hydration window moved: ListLimit 5001 and AllowanceWholeWindow -1, pinned 5000 and -1",
		},
		{
			name: "red: window bump without updating the pin", corpus: c1, digest: d1, version: 2, window: w2, pins: history,
			wantErr: "ExtractorVersion was bumped to 2 without updating the pinned pair: append " +
				`{Version: 2, CorpusDigest: "corpus-one", OutputDigest: "digest-one", Window: hydrationWindow{ListLimit: 5001, AllowanceWholeWindow: -1}}`,
		},
		{
			name: "red: window bump whose pin row keeps the old window", corpus: c1, digest: d1, version: 2, window: w2,
			pins:    []extractorPin{pin(1, c1, d1), pin(2, c1, d1)},
			wantErr: "a bump needs an output or window change",
		},
		{
			name: "red: bump with an unchanged output and an unchanged window", corpus: c1, digest: d1, version: 2, pins: history,
			wantErr: "the output over the guard corpus did not change",
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
			wantErr: "the output over the guard corpus did not change; if the change alters rows for an input the corpus lacks, " +
				"add that input to the generator's supplement and regenerate in this change, otherwise revert the bump",
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
			name: "red: bump with a new corpus without the pin row", corpus: c2, digest: d2, version: 2, pins: history,
			wantErr: `with an extractor change that the new input shows, set ExtractorVersion to 2 and append ` +
				`{Version: 2, CorpusDigest: "corpus-two", OutputDigest: "digest-two", Window: hydrationWindow{ListLimit: 5000, AllowanceWholeWindow: -1}}`,
		},
		{
			name: "red: history edited in place", corpus: c1, digest: d2, version: 2,
			pins:    []extractorPin{pin(2, c1, d2)},
			wantErr: "pin 0 has version 2, want 1",
		},
		{
			name: "red: two versions share an output over one corpus", corpus: c1, digest: d1, version: 2,
			pins:    []extractorPin{pin(1, c1, d1), pin(2, c1, d1)},
			wantErr: "a bump needs an output or window change",
		},
		{
			name: "red: empty history", corpus: c1, digest: d1, version: 1, pins: nil,
			wantErr: "the extractor pin history is empty",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			window := tc.window
			if window == (hydrationWindow{}) {
				window = w1
			}
			err := checkExtractorPins(tc.corpus, tc.digest, tc.version, window, tc.pins)
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
