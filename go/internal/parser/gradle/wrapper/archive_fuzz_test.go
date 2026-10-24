// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package wrapper

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// TestScanSurvivesGeneratedDistributionURLs drives Scan with distributionUrl
// values built from the archive grammar's own pieces (prefix, suffix, dots,
// dashes, empty tokens, query and fragment cuts, path separators). It asserts
// the scan never panics, that the same bytes always give the same result, and
// that a row appears only for a name a reader can check by hand: a gradle-
// prefix, a -bin.zip or -all.zip suffix, and a non-empty valid token between.
func TestScanSurvivesGeneratedDistributionURLs(t *testing.T) {
	t.Parallel()
	pieces := []string{
		"gradle-", "gradle", "-bin.zip", "-all.zip", "-bin", "-all", ".zip", "bin.zip", "all.zip",
		"-", "--", ".", "..", "8", "8.5", "8.5.1", "9", "0", "rc-1", "+", "_", "abc", "",
		"/", "?", "#", "https\\://h/", "h/", "x", "é", "\\", " ", "$", "'",
	}
	rng := rand.New(rand.NewSource(8822))
	const cases = 20000
	for i := 0; i < cases; i++ {
		var url strings.Builder
		for n := rng.Intn(8); n >= 0; n-- {
			url.WriteString(pieces[rng.Intn(len(pieces))])
		}
		text := "distributionUrl=" + url.String() + "\n"
		first := safeScan(t, text)
		second := safeScan(t, text)
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("case %d %q: Scan is not deterministic: %+v vs %+v", i, text, first, second)
		}
		for _, row := range first.Rows {
			value, _ := row["value"].(string)
			if value == "" || !validVersion([]byte(value)) || !strings.Contains(url.String(), "gradle-"+value+"-") {
				t.Fatalf("case %d %q: row %v has a value that is not a checked archive version", i, text, row)
			}
		}
	}
}

// safeScan runs Scan and turns a panic into a test failure that names the input.
func safeScan(t *testing.T, text string) (result Result) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Scan(%q) panicked: %v", text, r)
		}
	}()
	return Scan([]byte(text))
}
