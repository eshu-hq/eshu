// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package media

import (
	"math/rand"
	"strings"
	"testing"
)

// TestFoldASCIIAgreesWithFoldUnicode pins the ASCII fast path to the general
// fold on every ASCII byte, alone and between letters.
func TestFoldASCIIAgreesWithFoldUnicode(t *testing.T) {
	t.Parallel()

	for b := 0; b < 128; b++ {
		for _, text := range []string{
			string(rune(b)), "a" + string(rune(b)) + "b", string(rune(b)) + "Pass", "Pass" + string(rune(b)),
			" " + string(rune(b)) + " x", "x" + string(rune(b)) + string(rune(b)) + "y",
		} {
			if got, want := foldASCII(text), foldUnicode(text); got != want {
				t.Fatalf("foldASCII(%q) = %q, foldUnicode = %q", text, got, want)
			}
		}
	}
}

// TestJoinedHasKeywordCutsNeverFakeABoundary slides a long first or second
// segment so the byte limit lands on every position of a word, and requires
// that a cut word is dropped whole instead of becoming a whole-word keyword.
func TestJoinedHasKeywordCutsNeverFakeABoundary(t *testing.T) {
	t.Parallel()

	for pad := 0; pad < 40; pad++ {
		filler := strings.Repeat("c", pad)
		for _, word := range []string{"betoken", "spinning", "pinned", "secretary", "tokenize"} {
			if joinedHasKeyword(foldSpoken("ok "+word+" "+filler), foldSpoken("x")) {
				t.Fatalf("tail: %q + pad %d matched", word, pad)
			}
			if joinedHasKeyword(foldSpoken("ok"), foldSpoken("x "+filler+" "+word+" tail")) {
				t.Fatalf("head: %q + pad %d matched", word, pad)
			}
			if joinedHasKeyword(foldSpoken(filler+" "+word), foldSpoken(word+" "+filler)) {
				t.Fatalf("both: %q + pad %d matched", word, pad)
			}
		}
	}
}

// TestJoinedHasKeywordMatchesFullJoin checks the bounded join against the full
// join of the two folded segments, over deterministic random pairs where
// neither side holds a keyword alone. The bounded join must find exactly the
// keywords that need both halves.
func TestJoinedHasKeywordMatchesFullJoin(t *testing.T) {
	t.Parallel()

	words := []string{
		"the", "api", "key", "pass", "word", "private", "p", "i", "n", "pin", "pinned", "spin", "token", "tokenize",
		"secret", "secretary", "compass", "restart", "worker", "swordfish", "a", "is", "code", "phrase",
		"pwd", "w", "d", "marker", "supercalifragilistic", strings.Repeat("z", 30),
	}
	rng := rand.New(rand.NewSource(9788))
	build := func() string {
		count := 1 + rng.Intn(9)
		parts := make([]string, count)
		for i := range parts {
			parts[i] = words[rng.Intn(len(words))]
		}
		return strings.Join(parts, " ")
	}
	halves := [][2]string{
		{"the api", "key is"},
		{"pass", "word"},
		{"my private", "key"},
		{"p i", "n"},
		{"p", "i n"},
		{"a p i", "key"},
		{"secret", "marker"},
		{"pass", "phrase"},
		{"p w", "d"},
		{"token", "marker"},
	}
	straddling := 0
	for range 20000 {
		prevText, curText := build(), build()
		if rng.Intn(2) == 0 {
			half := halves[rng.Intn(len(halves))]
			prevText, curText = prevText+" "+half[0], half[1]+" "+curText
		}
		prev, cur := foldSpoken(prevText), foldSpoken(curText)
		if foldedHasKeyword(prev) || foldedHasKeyword(cur) {
			continue
		}
		want := foldedHasKeyword(prev + " " + cur)
		if got := joinedHasKeyword(prev, cur); got != want {
			t.Fatalf("joinedHasKeyword(%q, %q) = %v, full join = %v", prev, cur, got, want)
		}
		if want {
			straddling++
		}
	}
	if straddling < 100 {
		t.Fatalf("only %d straddling pairs generated, the property test proves too little", straddling)
	}
}
