// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cvss

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The digests below pin every base-metric vector of CVSS v3.0, v3.1, and v4.0
// to the score FIRST's own calculators report, so a changed table entry, a
// changed tie-break in the v4 algorithm, or a changed weight fails here and not
// only in a hand-picked example. They were generated from FIRST's code, not
// from this package:
//
//   - v4.0: FIRST's reference calculator
//     (https://github.com/FIRSTdotorg/cvss-v4-calculator, main at c5b0d409ae9f:
//     cvss_lookup.js, max_composed.js, max_severity.js, cvss_score.js) run over
//     all 104,976 base vectors with the threat and environmental metrics at X.
//   - v3.1 and v3.0: FIRST's published calculators
//     (https://www.first.org/cvss/calculator/cvsscalc31.js and cvsscalc30.js)
//     run over all 2,592 base vectors each.
//
// Each digest is the SHA-256 of the lines "<vector> <score>", sorted bytewise
// (LC_ALL=C), joined by "\n", with a trailing "\n". FIRST's v3 calculators
// print one decimal place (10.0, 0.0) and its v4 calculator prints the
// shortest decimal form (10, 0); the test formats each version the same way.
// Two independent runs of FIRST's code
// produced the same digests. Regenerate them from FIRST's code, never from
// BaseScore, if a specification revision changes a score.
const (
	digestV40 = "e016b5687126a9e684e3ea86f2676be1172b38528d46676636b501cbde757c2b"
	digestV31 = "a54e3a7c3f8082c9e873c6c630b8bea5516dbc996bc39dda86a6f62e6ded4b66"
	digestV30 = "2e581dabc566cde1d1b5739d064e24fcf9b6cb499c6415316635f1ec586b8db6"
)

type metricAxis struct {
	key    string
	values []string
}

var (
	v3BaseAxes = []metricAxis{
		{"AV", []string{"N", "A", "L", "P"}},
		{"AC", []string{"L", "H"}},
		{"PR", []string{"N", "L", "H"}},
		{"UI", []string{"N", "R"}},
		{"S", []string{"U", "C"}},
		{"C", []string{"H", "L", "N"}},
		{"I", []string{"H", "L", "N"}},
		{"A", []string{"H", "L", "N"}},
	}
	v4BaseAxes = []metricAxis{
		{"AV", []string{"N", "A", "L", "P"}},
		{"AC", []string{"L", "H"}},
		{"AT", []string{"N", "P"}},
		{"PR", []string{"N", "L", "H"}},
		{"UI", []string{"N", "P", "A"}},
		{"VC", []string{"H", "L", "N"}},
		{"VI", []string{"H", "L", "N"}},
		{"VA", []string{"H", "L", "N"}},
		{"SC", []string{"H", "L", "N"}},
		{"SI", []string{"H", "L", "N"}},
		{"SA", []string{"H", "L", "N"}},
	}
)

// baseSpaceDigest scores every base vector of a version and hashes the sorted
// "<vector> <score>" lines.
func baseSpaceDigest(t *testing.T, prefix string, axes []metricAxis, wantCount, decimals int) string {
	t.Helper()
	var lines []string
	var walk func(depth int, parts []string)
	walk = func(depth int, parts []string) {
		if depth == len(axes) {
			vector := prefix + strings.Join(parts, "/")
			result, err := BaseScore(vector)
			if err != nil {
				t.Fatalf("BaseScore(%q) error = %v", vector, err)
			}
			lines = append(lines, vector+" "+strconv.FormatFloat(result.Score, 'f', decimals, 64))
			return
		}
		for _, value := range axes[depth].values {
			walk(depth+1, append(parts, axes[depth].key+":"+value))
		}
	}
	walk(0, make([]string, 0, len(axes)))
	if len(lines) != wantCount {
		t.Fatalf("scored %d vectors, want %d", len(lines), wantCount)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n") + "\n"))
	return hex.EncodeToString(sum[:])
}

func TestBaseScoreMatchesFIRSTOverTheWholeBaseSpace(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		prefix string
		axes   []metricAxis
		count  int
		places int
		want   string
	}{
		{"v4.0", "CVSS:4.0/", v4BaseAxes, 104976, -1, digestV40},
		{"v3.1", "CVSS:3.1/", v3BaseAxes, 2592, 1, digestV31},
		{"v3.0", "CVSS:3.0/", v3BaseAxes, 2592, 1, digestV30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := baseSpaceDigest(t, tc.prefix, tc.axes, tc.count, tc.places); got != tc.want {
				t.Fatalf("%s base-space digest = %s, want FIRST's %s: some base vector scores differently from FIRST's calculator",
					tc.name, got, tc.want)
			}
		})
	}
}
