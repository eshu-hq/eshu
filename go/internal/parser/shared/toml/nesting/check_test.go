// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nesting

import (
	"errors"
	"math"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// depthCases are valid TOML documents with the depth Check must measure. Each
// one is also decoded by go-toml, so an expectation can never rest on a
// document the real decoder rejects.
var depthCases = []struct {
	name  string
	doc   string
	depth int
}{
	{"scalar", "a = 1\n", 0},
	{"array", "a = [1]\n", 1},
	{"nested arrays", "a = [[1], [2, [3]]]\n", 3},
	{"dotted key into array", "a.b = [1]\n", 2},
	{"spaced dotted key", "a . b . c = 1\n", 2},
	{"dotted key inside inline table", "x = {a.b = {c = 1}}\n", 3},
	{"comma forgets the previous dotted key", "x = {a.b = 1, c = [1]}\n", 2},
	{"inline table in array", "a = [{b.c.d = 1}]\n", 4},
	{"header carries to its lines", "[a.b.c]\nd = [1]\n", 3},
	{"new header replaces old depth", "[a.b]\n[c]\nd = [1]\n", 1},
	{"array table header", "[[a.b]]\nx = 1\n[[a.b]]\nx = 2\n", 1},
	{"many sibling headers do not add", "[a]\n[b]\n[c]\n[d]\n[e]\nx = 1\n", 0},
	{"brackets in basic string", "a = \"[[[[{{{{\"\n", 0},
	{"brackets in literal string", "a = '[[{{.]]'\n", 0},
	{"escaped quote keeps string open", "a = \"\\\"[[\"\nb = [1]\n", 1},
	{"escaped backslash closes string", "a = \"\\\\\"\nb = [[1]]\n", 2},
	{"comment brackets", "# [[[[ {{{{ a.b.c\na = [1]\n", 1},
	{"comment inside array", "a = [ # [[[ {{\n  1, # ]]]\n]\n", 1},
	{"multi-line closer, three quotes", "a = [\"\"\"x\"\"\", [[1]]]\n", 3},
	{"multi-line closer, four quotes", "a = [\"\"\"x\"\"\"\", [[1]]]\n", 3},
	{"multi-line closer, five quotes", "a = [\"\"\"x\"\"\"\"\", [[1]]]\n", 3},
	{"literal multi-line closer, four quotes", "a = ['''x'''', [[1]]]\n", 3},
	{"literal multi-line closer, five quotes", "a = ['''x''''', [[1]]]\n", 3},
	{"empty multi-line strings", "a = [\"\"\"\"\"\", '''''', [1]]\n", 2},
	{"multi-line string spans lines", "a = \"\"\"\n[[[\n{{{\n\"\"\"\nb = [1]\n", 1},
	{"escaped quotes in multi-line", "a = \"\"\" \\\"\"\" [[ \"\"\"\nb = [1]\n", 1},
	{"line-ending backslash in multi-line", "a = \"\"\"x \\\n  [[ \"\"\"\nb = 1\n", 0},
	{"quoted key with dots", "\"a.b\".c = 1\n'x.y' = [1]\n", 1},
	{"dots in values do not count", "a = 1.5\nb = 1979-05-27T07:32:00.999Z\nc = [1.5, 2.5]\n", 1},
	{"multi-line array lines are values", "a = [\n[1],\n[2]\n]\n", 2},
	{"crlf", "a = [\r\n [1],\r\n]\r\nb.c = 1\r\n", 2},
	{
		"cargo target header with inline table",
		"[target.'cfg(any(unix, a.b))'.dependencies]\nx = { version = \"1\", features = [\"a\"] }\n",
		4,
	},
}

func TestScanMeasuresExactDepth(t *testing.T) {
	t.Parallel()
	for _, tc := range depthCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var decoded map[string]any
			if err := toml.Unmarshal([]byte(tc.doc), &decoded); err != nil {
				t.Fatalf("go-toml rejects the case, so it is not a valid expectation: %v", err)
			}
			got, err := scan([]byte(tc.doc), math.MaxInt)
			if err != nil {
				t.Fatalf("scan() error = %v, want nil", err)
			}
			if got != tc.depth {
				t.Fatalf("scan() depth = %d, want %d", got, tc.depth)
			}
		})
	}
}

// TestCheckLimitIsInclusive proves a document at exactly the limit passes and
// one level over fails.
func TestCheckLimitIsInclusive(t *testing.T) {
	t.Parallel()
	for _, tc := range depthCases {
		if err := Check([]byte(tc.doc), tc.depth); err != nil {
			t.Errorf("%s: Check(limit=%d) = %v, want nil", tc.name, tc.depth, err)
		}
		if tc.depth == 0 {
			continue
		}
		if err := Check([]byte(tc.doc), tc.depth-1); !errors.Is(err, ErrTooDeep) {
			t.Errorf("%s: Check(limit=%d) = %v, want ErrTooDeep", tc.name, tc.depth-1, err)
		}
	}
}

func TestCheckNegativeLimitActsAsZero(t *testing.T) {
	t.Parallel()
	if err := Check([]byte("a = 1\n"), -5); err != nil {
		t.Fatalf("Check(flat, -5) = %v, want nil", err)
	}
	if err := Check([]byte("a = [1]\n"), -5); !errors.Is(err, ErrTooDeep) {
		t.Fatalf("Check(array, -5) = %v, want ErrTooDeep", err)
	}
}

// TestCheckRejectsUnterminatedStrings covers the fail-closed rule: a string
// that never closes makes the document invalid TOML, so the guard reports it
// instead of guessing where the string ends. go-toml must reject each one
// too, which is what makes the rejection exact.
func TestCheckRejectsUnterminatedStrings(t *testing.T) {
	t.Parallel()
	for name, doc := range map[string]string{
		"basic at end of file":       "a = \"abc",
		"basic at end of line":       "a = \"abc\nb = 1\n",
		"basic ending in escape":     "a = \"abc\\\"\n",
		"literal":                    "a = 'abc\n",
		"multi-line basic":           "a = \"\"\"abc\n[[[\n",
		"multi-line literal":         "a = '''abc''\n",
		"quoted key":                 "\"a = 1\n",
		"string hiding brackets":     "a = \"[[[[[[[[[[[[[[[[[[[[[[[[[[",
		"multi-line hiding brackets": "a = '''\n" + "[[[[[[[[[[[[[[[[[[[[[[[[[[",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := Check([]byte(doc), 64); !errors.Is(err, ErrUnterminatedString) {
				t.Fatalf("Check() = %v, want ErrUnterminatedString", err)
			}
			var decoded map[string]any
			if err := toml.Unmarshal([]byte(doc), &decoded); err == nil {
				t.Fatal("go-toml accepts the document; the guard must not reject valid TOML")
			}
		})
	}
}

// TestCheckIgnoresUnclosedHeaderBrackets proves header brackets never
// accumulate across lines: many broken headers stay at depth 0.
func TestCheckIgnoresUnclosedHeaderBrackets(t *testing.T) {
	t.Parallel()
	doc := make([]byte, 0, 4*10_000)
	for range 10_000 {
		doc = append(doc, "[[a\n"...)
	}
	if err := Check(doc, 1); err != nil {
		t.Fatalf("Check() = %v, want nil", err)
	}
}

func TestCheckErrorNamesOffset(t *testing.T) {
	t.Parallel()
	err := Check([]byte("a = [[[1]]]\n"), 2)
	if !errors.Is(err, ErrTooDeep) {
		t.Fatalf("Check() = %v, want ErrTooDeep", err)
	}
	if got, want := err.Error(), "toml nesting exceeds limit: depth 3 passes limit 2 at byte 6"; got != want {
		t.Fatalf("Check() error = %q, want %q", got, want)
	}
}
