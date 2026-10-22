// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// TestRankEqualTermFrequencyShorterToolWins: same term frequency, different
// document length. BM25 length normalization must put the shorter tool first.
// The shorter tool sorts later by name, so a ranker without length
// normalization would order them alpha_one, alpha_two.
func TestRankEqualTermFrequencyShorterToolWins(t *testing.T) {
	tools := []toolcontract.ToolDefinition{
		def("alpha_one", "Handles zebra processing for many unrelated historic archive purposes today."),
		def("alpha_two", "Handles zebra."),
	}
	got := rankWith(t, tools, nil, router.Query{Intent: "zebra", Limit: 5})
	if want := []string{"alpha_two", "alpha_one"}; !equalStrings(names(got), want) {
		t.Fatalf("order = %v, want %v", names(got), want)
	}
	if got[0].Score <= got[1].Score {
		t.Fatalf("scores = %v, %v; want the shorter tool strictly higher", got[0].Score, got[1].Score)
	}
}

// TestRankScoreMatchesWorkedBM25Example pins the exact score for a two-tool
// registry whose inputs are derived by hand, not read from the code:
//
//	alpha_one: tokens alpha,one (name, weight 2 each = 4) + handle,zebra (desc, 1 each = 2)
//	           -> length 6, tf(zebra) = 1
//	alpha_two: name 4 + handle,zebra,widely,used,here = 5 -> length 9
//	average length = 7.5; N = 2, df(zebra) = 2
//	idf = ln(1 + (2 - 2 + 0.5) / (2 + 0.5)) = ln(1.2)
//	score(alpha_one) = idf * tf * (k1+1) / (tf + k1 * (1 - b + b * 6/7.5))
//	with k1 = 1.2 and b = 0.75.
func TestRankScoreMatchesWorkedBM25Example(t *testing.T) {
	tools := []toolcontract.ToolDefinition{
		def("alpha_one", "Handles zebra."),
		def("alpha_two", "Handles zebra widely used here."),
	}
	got := rankWith(t, tools, nil, router.Query{Intent: "zebra", Limit: 5})
	byName := map[string]float64{}
	for _, c := range got {
		byName[c.Name] = c.Score
	}
	idf := math.Log(1.2)
	wantOne := idf * 1 * 2.2 / (1 + 1.2*(1-0.75+0.75*6/7.5))
	wantTwo := idf * 1 * 2.2 / (1 + 1.2*(1-0.75+0.75*9/7.5))
	if math.Abs(byName["alpha_one"]-wantOne) > 1e-9 || math.Abs(byName["alpha_two"]-wantTwo) > 1e-9 {
		t.Fatalf("scores = %v, want alpha_one %.12f alpha_two %.12f", byName, wantOne, wantTwo)
	}
}

// quoted returns the text between the first and last double quote of a Why line.
func quoted(t *testing.T, why string) string {
	t.Helper()
	i, j := strings.Index(why, `"`), strings.LastIndex(why, `"`)
	if i < 0 || j <= i {
		t.Fatalf("Why = %q, want a quoted phrase", why)
	}
	return why[i+1 : j]
}

// TestRankWhyPhraseIsAtMost160Runes pins the documented cut: 160 runes plus the
// ellipsis, counted in runes so multibyte text cannot slip past a byte bound.
func TestRankWhyPhraseIsAtMost160Runes(t *testing.T) {
	long := "zebra " + strings.Repeat("wörd ", 200)
	tools := []toolcontract.ToolDefinition{def("alpha_tool", long)}
	got := rankWith(t, tools, nil, router.Query{Intent: "zebra", Limit: 1})
	phrase := quoted(t, got[0].Why)
	if n := utf8.RuneCountInString(phrase); n > 161 || n < 100 {
		t.Fatalf("quoted phrase = %d runes, want between 100 and 161 (160 plus the ellipsis)", n)
	}
	if !strings.HasSuffix(phrase, "…") {
		t.Fatalf("phrase %q was cut but has no ellipsis", phrase)
	}
}

// boilerplateRegistry builds n tools with unique names and unique first
// sentences; the first copies tools also carry the shared sentence.
func boilerplateRegistry(n, copies int, shared string) []toolcontract.ToolDefinition {
	tools := make([]toolcontract.ToolDefinition, 0, n)
	for i := 0; i < n; i++ {
		desc := fmt.Sprintf("Reads area%02d data.", i)
		if i < copies {
			desc += " " + shared
		}
		tools = append(tools, def(fmt.Sprintf("tool_%02d", i), desc))
	}
	return tools
}

// TestRankSentenceRepeatedTwiceIsKeptThreeTimesIsStripped pins the repeat
// threshold: two copies are coincidence, three are boilerplate.
func TestRankSentenceRepeatedTwiceIsKeptThreeTimesIsStripped(t *testing.T) {
	const shared = "Quasar frobnicates zebra gently."
	q := router.Query{Intent: "frobnicates", Limit: 10}
	if got := rankWith(t, boilerplateRegistry(14, 2, shared), nil, q); len(got) != 2 {
		t.Fatalf("sentence in 2 tools: got %v, want both tools kept", names(got))
	}
	if got := rankWith(t, boilerplateRegistry(14, 3, shared), nil, q); len(got) != 0 {
		t.Fatalf("sentence in 3 tools: got %v, want it stripped as boilerplate", names(got))
	}
}

// TestRankBoilerplateNeedsTwelveTools pins the corpus floor: an 11-tool
// registry keeps even a sentence repeated in every tool; 12 tools strip it.
func TestRankBoilerplateNeedsTwelveTools(t *testing.T) {
	const shared = "Quasar frobnicates zebra gently."
	q := router.Query{Intent: "frobnicates", Limit: 10}
	if got := rankWith(t, boilerplateRegistry(11, 11, shared), nil, q); len(got) != 10 {
		t.Fatalf("11 tools: got %d results, want 10 (limit) because nothing is stripped", len(got))
	}
	if got := rankWith(t, boilerplateRegistry(12, 12, shared), nil, q); len(got) != 0 {
		t.Fatalf("12 tools: got %v, want the shared sentence stripped", names(got))
	}
}
