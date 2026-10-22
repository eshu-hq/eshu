// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

var _ router.Ranker = (*Lexical)(nil)

func TestRankMatchesIntentPhrase(t *testing.T) {
	tools, entries := fixtureTools()
	got := rankWith(t, tools, entries, router.Query{Intent: "where is this function defined", Limit: 3})
	if len(got) == 0 || got[0].Name != "find_symbol" {
		t.Fatalf("top = %v, want find_symbol first", names(got))
	}
	if got[0].Score <= 0 {
		t.Fatalf("Score = %v, want > 0", got[0].Score)
	}
}

func TestRankMatchesNameTokens(t *testing.T) {
	tools, _ := fixtureTools()
	got := rankWith(t, tools, nil, router.Query{Intent: "dead code", Limit: 3})
	if len(got) == 0 || got[0].Name != "get_dead_code" {
		t.Fatalf("top = %v, want get_dead_code first", names(got))
	}
}

func TestRankIntentOutweighsDescription(t *testing.T) {
	tools := []toolcontract.ToolDefinition{
		def("alpha_one", "Handles zebra processing."),
		def("alpha_two", "Handles other processing."),
	}
	entries := map[string]catalog.Entry{
		"alpha_two": {Name: "alpha_two", Cost: catalog.CostLow, Intents: []string{"zebra"}},
		"alpha_one": {Name: "alpha_one", Cost: catalog.CostLow},
	}
	got := rankWith(t, tools, entries, router.Query{Intent: "zebra", Limit: 5})
	if want := []string{"alpha_two", "alpha_one"}; !equalStrings(names(got), want) {
		t.Fatalf("order = %v, want %v", names(got), want)
	}
}

func TestRankNameOutweighsDescription(t *testing.T) {
	tools := []toolcontract.ToolDefinition{
		def("alpha_one", "Handles zebra processing."),
		def("zebra_two", "Handles other processing."),
	}
	got := rankWith(t, tools, nil, router.Query{Intent: "zebra", Limit: 5})
	if want := []string{"zebra_two", "alpha_one"}; !equalStrings(names(got), want) {
		t.Fatalf("order = %v, want %v", names(got), want)
	}
}

func TestRankTieBreakOrder(t *testing.T) {
	// Identical text and token counts, so lexical scores are exactly equal.
	tools := []toolcontract.ToolDefinition{
		def("beta_d", "Locates the zebra."),
		def("beta_c", "Locates the zebra."),
		def("beta_b", "Locates the zebra."),
		def("beta_a", "Locates the zebra."),
	}
	entries := map[string]catalog.Entry{
		"beta_a": {Name: "beta_a", Cost: catalog.CostHigh},
		"beta_b": {Name: "beta_b", Cost: catalog.CostLow},
		"beta_c": {Name: "beta_c", Cost: catalog.CostLow},
		"beta_d": {Name: "beta_d", Cost: catalog.CostHigh, Canonical: true},
	}
	got := rankWith(t, tools, entries, router.Query{Intent: "zebra", Limit: 10})
	// Canonical first, then cheaper cost, then name.
	if want := []string{"beta_d", "beta_b", "beta_c", "beta_a"}; !equalStrings(names(got), want) {
		t.Fatalf("order = %v, want %v", names(got), want)
	}
	if got[0].Score != got[3].Score {
		t.Fatalf("scores differ (%v vs %v); fixture is not a true tie", got[0].Score, got[3].Score)
	}
}

func TestRankTieBreakCostWithoutCanonical(t *testing.T) {
	tools := []toolcontract.ToolDefinition{
		def("beta_a", "Locates the zebra."),
		def("beta_b", "Locates the zebra."),
		def("beta_c", "Locates the zebra."),
	}
	entries := map[string]catalog.Entry{
		"beta_a": {Name: "beta_a", Cost: catalog.CostHigh},
		"beta_b": {Name: "beta_b", Cost: catalog.CostModerate},
		// beta_c has no entry: unknown cost ranks as high (conservative).
	}
	got := rankWith(t, tools, entries, router.Query{Intent: "zebra", Limit: 10})
	if want := []string{"beta_b", "beta_a", "beta_c"}; !equalStrings(names(got), want) {
		t.Fatalf("order = %v, want %v", names(got), want)
	}
	if got[2].Cost != catalog.CostHigh {
		t.Fatalf("missing entry Cost = %q, want %q", got[2].Cost, catalog.CostHigh)
	}
}

func TestRankAllowedFiltersBeforeRanking(t *testing.T) {
	tools := []toolcontract.ToolDefinition{
		def("secret_zebra", "Zebra zebra zebra internals."),
		def("public_zebra", "Zebra lookup."),
		def("public_other", "Unrelated horse lookup."),
	}
	q := router.Query{Intent: "zebra lookup", Limit: 10}
	r := New(tools, nil)
	allowed := func(n string) bool { return n != "secret_zebra" }
	got, err := r.Rank(context.Background(), q, tools, nil, allowed)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	for _, c := range got {
		if c.Name == "secret_zebra" {
			t.Fatalf("returned a disallowed tool: %v", names(got))
		}
	}
	// A disallowed tool must not shift scores: ranking the allowed subset as the
	// whole registry gives identical scores and order.
	want := rankWith(t, tools[1:], nil, q)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", names(got), names(want))
	}
	for i := range got {
		if got[i].Name != want[i].Name || got[i].Score != want[i].Score {
			t.Fatalf("candidate %d = %s/%v, want %s/%v (disallowed tool shifted scores)",
				i, got[i].Name, got[i].Score, want[i].Name, want[i].Score)
		}
	}
}

func TestRankAllowedAppliesToCanonicalTools(t *testing.T) {
	tools := []toolcontract.ToolDefinition{def("beta_a", "Locates the zebra."), def("beta_b", "Locates the zebra.")}
	entries := map[string]catalog.Entry{"beta_a": {Name: "beta_a", Canonical: true}}
	r := New(tools, entries)
	got, err := r.Rank(context.Background(), router.Query{Intent: "zebra", Limit: 5}, tools, entries,
		func(n string) bool { return n == "beta_b" })
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if want := []string{"beta_b"}; !equalStrings(names(got), want) {
		t.Fatalf("got %v, want %v", names(got), want)
	}
}

func TestRankNilAllowedFailsClosed(t *testing.T) {
	tools, entries := fixtureTools()
	got, err := New(tools, entries).Rank(context.Background(), router.Query{Intent: "function", Limit: 3}, tools, entries, nil)
	if err == nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want an error and no candidates for a nil allowed func", names(got), err)
	}
}

func TestRankEmptyIntent(t *testing.T) {
	tools, entries := fixtureTools()
	for _, intent := range []string{"", "   ", "the of and"} {
		got := rankWith(t, tools, entries, router.Query{Intent: intent, Limit: 5})
		if len(got) != 0 {
			t.Fatalf("intent %q: got %v, want no candidates", intent, names(got))
		}
	}
}

func TestRankNoMatchIsEmptyNotError(t *testing.T) {
	tools, entries := fixtureTools()
	got, err := New(tools, entries).Rank(context.Background(),
		router.Query{Intent: "xylophone quasar", Limit: 5}, tools, entries, allowAll)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want none", names(got))
	}
}

func TestRankLimitBounds(t *testing.T) {
	tools := make([]toolcontract.ToolDefinition, 0, 14)
	for i := 0; i < 14; i++ {
		tools = append(tools, def(fmt.Sprintf("tool_%02d", i), fmt.Sprintf("Locates the zebra near area%02d.", i)))
	}
	cases := []struct {
		limit int
		want  int
	}{
		{-1, 0}, {0, 0}, {1, 1}, {3, 3}, {10, 10}, {14, router.MaxLimit}, {100, router.MaxLimit},
	}
	for _, c := range cases {
		got := rankWith(t, tools, nil, router.Query{Intent: "zebra", Limit: c.limit})
		if len(got) != c.want {
			t.Errorf("Limit %d: got %d candidates, want %d", c.limit, len(got), c.want)
		}
	}
}

func TestRankLimitAboveMatchCount(t *testing.T) {
	tools, entries := fixtureTools()
	got := rankWith(t, tools, entries, router.Query{Intent: "repositories", Limit: 10})
	if len(got) == 0 || len(got) >= 10 {
		t.Fatalf("got %d candidates (%v), want fewer than Limit and at least one", len(got), names(got))
	}
}

func TestRankAnchorsPreferToolsAcceptingThem(t *testing.T) {
	tools := []toolcontract.ToolDefinition{
		def("gamma_a", "Locates the zebra.", "name"),
		def("gamma_b", "Locates the zebra.", "repo_id"),
		def("gamma_c", "Locates the zebra.", "environment"),
	}
	base := router.Query{Intent: "zebra", Limit: 5}
	if got := rankWith(t, tools, nil, base); !equalStrings(names(got), []string{"gamma_a", "gamma_b", "gamma_c"}) {
		t.Fatalf("no anchors: order = %v, want name order", names(got))
	}
	withRepo := base
	withRepo.Anchors.RepoID = "repo-1"
	if got := rankWith(t, tools, nil, withRepo); got[0].Name != "gamma_b" {
		t.Fatalf("repo_id anchor: order = %v, want gamma_b first", names(got))
	}
	withEnv := base
	withEnv.Anchors.Environment = "prod"
	if got := rankWith(t, tools, nil, withEnv); got[0].Name != "gamma_c" {
		t.Fatalf("environment anchor: order = %v, want gamma_c first", names(got))
	}
}

func TestRankAnchorsDoNotCreateMatches(t *testing.T) {
	tools := []toolcontract.ToolDefinition{def("gamma_b", "Locates the zebra.", "repo_id")}
	q := router.Query{Intent: "quasar", Limit: 5}
	q.Anchors.RepoID = "repo-1"
	if got := rankWith(t, tools, nil, q); len(got) != 0 {
		t.Fatalf("got %v, want none: anchors only prefer, they never add a lexical match", names(got))
	}
}

func TestRankDownWeightsRepeatedBoilerplate(t *testing.T) {
	const boiler = "Scoped tokens receive only granted repositories; an ungranted repository selector is rejected."
	var tools []toolcontract.ToolDefinition
	for i := 0; i < 14; i++ {
		tools = append(tools, def(fmt.Sprintf("filler_%02d", i), fmt.Sprintf("Handles area%02d work. %s", i, boiler)))
	}
	tools = append(tools, def("list_repos", "Lists every indexed repository."))

	got := rankWith(t, tools, nil, router.Query{Intent: "granted tokens", Limit: 10})
	if len(got) != 0 {
		t.Fatalf("boilerplate-only match returned %v, want none", names(got))
	}
	got = rankWith(t, tools, nil, router.Query{Intent: "repository", Limit: 10})
	if want := []string{"list_repos"}; !equalStrings(names(got), want) {
		t.Fatalf("got %v, want %v: the boilerplate sentence must not match", names(got), want)
	}
}

func TestRankRareTermsOutweighCommonTerms(t *testing.T) {
	var tools []toolcontract.ToolDefinition
	for i := 0; i < 10; i++ {
		tools = append(tools, def(fmt.Sprintf("common_%02d", i), fmt.Sprintf("Reads common data from source%02d.", i)))
	}
	tools = append(tools, def("rare_one", "Reads data via the quasar index."))
	got := rankWith(t, tools, nil, router.Query{Intent: "common quasar", Limit: 3})
	if len(got) == 0 || got[0].Name != "rare_one" {
		t.Fatalf("top = %v, want rare_one (rare term should outweigh a common one)", names(got))
	}
}

func TestRankWhyIsOneLineFromBestIntent(t *testing.T) {
	tools, entries := fixtureTools()
	got := rankWith(t, tools, entries, router.Query{Intent: "jump to a class definition", Limit: 1})
	if len(got) != 1 || got[0].Name != "find_symbol" {
		t.Fatalf("got %v, want find_symbol", names(got))
	}
	why := got[0].Why
	if strings.ContainsAny(why, "\n\r") || why == "" {
		t.Fatalf("Why = %q, want a non-empty single line", why)
	}
	if !strings.Contains(why, "jump to a class definition") {
		t.Fatalf("Why = %q, want it built from the best-matching intent", why)
	}
}

func TestRankWhyFallsBackToDescriptionSentenceThenName(t *testing.T) {
	tools := []toolcontract.ToolDefinition{
		def("alpha_tool", "Does something unrelated.\nSecond line mentions zebra here."),
		def("zebra_tool", "Does something unrelated."),
	}
	got := rankWith(t, tools, nil, router.Query{Intent: "zebra", Limit: 5})
	byName := map[string]router.Candidate{}
	for _, c := range got {
		byName[c.Name] = c
	}
	if why := byName["alpha_tool"].Why; !strings.Contains(why, "Second line mentions zebra here") || strings.ContainsAny(why, "\n\r") {
		t.Fatalf("alpha_tool Why = %q, want the matching description sentence on one line", why)
	}
	if why := byName["zebra_tool"].Why; !strings.Contains(why, "zebra") || strings.ContainsAny(why, "\n\r") {
		t.Fatalf("zebra_tool Why = %q, want a one-line name-based reason", why)
	}
}

func TestRankWhyIsTruncated(t *testing.T) {
	long := "zebra " + strings.Repeat("word ", 200)
	tools := []toolcontract.ToolDefinition{def("alpha_tool", long)}
	got := rankWith(t, tools, nil, router.Query{Intent: "zebra", Limit: 1})
	if len(got) != 1 || len(got[0].Why) > 260 {
		t.Fatalf("Why = %d bytes, want one candidate and <= 260", len(got[0].Why))
	}
}

func TestRankCandidateCarriesSchemaAndCost(t *testing.T) {
	tools, entries := fixtureTools()
	got := rankWith(t, tools, entries, router.Query{Intent: "repositories", Limit: 1})
	if len(got) != 1 || got[0].Name != "list_repositories" {
		t.Fatalf("got %v, want list_repositories", names(got))
	}
	if got[0].Cost != catalog.CostLow {
		t.Fatalf("Cost = %q, want %q", got[0].Cost, catalog.CostLow)
	}
	want := tools[1].InputSchema.(map[string]any)
	if got[0].InputSchema == nil || fmt.Sprintf("%p", got[0].InputSchema) != fmt.Sprintf("%p", want) {
		t.Fatalf("InputSchema does not alias the registry definition")
	}
}

func TestRankContextCanceled(t *testing.T) {
	tools, entries := fixtureTools()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(tools, entries).Rank(ctx, router.Query{Intent: "function", Limit: 3}, tools, entries, allowAll); err == nil {
		t.Fatalf("Rank with a canceled context returned no error")
	}
}

func TestRankBuildsIndexOnce(t *testing.T) {
	tools, entries := fixtureTools()
	r := New(tools, entries)
	if got := r.builds.Load(); got != 1 {
		t.Fatalf("builds after New = %d, want 1", got)
	}
	for i := 0; i < 50; i++ {
		if _, err := r.Rank(context.Background(), router.Query{Intent: "function", Limit: 3}, tools, entries, allowAll); err != nil {
			t.Fatalf("Rank: %v", err)
		}
	}
	if got := r.builds.Load(); got != 1 {
		t.Fatalf("builds after 50 Rank calls = %d, want 1 (index must not be rebuilt per call)", got)
	}
}

func TestRankRebuildsWhenToolSetDiffers(t *testing.T) {
	tools, entries := fixtureTools()
	r := New(tools, entries)
	other := append([]toolcontract.ToolDefinition{def("brand_new_zebra", "Zebra finder.")}, tools...)
	got, err := r.Rank(context.Background(), router.Query{Intent: "zebra", Limit: 3}, other, entries, allowAll)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if len(got) != 1 || got[0].Name != "brand_new_zebra" {
		t.Fatalf("got %v, want brand_new_zebra (a different tool set must be ranked correctly, not from a stale index)", names(got))
	}
}

func TestRankConcurrentCallsAgree(t *testing.T) {
	tools, entries := fixtureTools()
	r := New(tools, entries)
	q := router.Query{Intent: "find a function definition", Limit: 5}
	want, err := r.Rank(context.Background(), q, tools, entries, allowAll)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				got, err := r.Rank(context.Background(), q, tools, entries, allowAll)
				if err != nil || !equalStrings(names(got), names(want)) {
					t.Errorf("concurrent Rank = %v, %v; want %v", names(got), err, names(want))
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestRankDownWeightsBoilerplateTemplateVariants covers boilerplate that is a
// template, not an exact repeat: the same refusal wording around a slot that
// differs per tool, as the real scoped-token sentence does.
func TestRankDownWeightsBoilerplateTemplateVariants(t *testing.T) {
	var tools []toolcontract.ToolDefinition
	for i := 0; i < 14; i++ {
		tools = append(tools, def(fmt.Sprintf("filler_%02d", i), fmt.Sprintf(
			"Reads slot%02d data. Scoped tokens receive only slot%02d rows whose every hop is in a granted repository; an ungranted repository selector is rejected.", i, i)))
	}
	tools = append(tools, def("list_repos", "Lists every indexed repository."))

	for _, intent := range []string{"scoped tokens granted", "ungranted selector rejected", "hop selector"} {
		if got := rankWith(t, tools, nil, router.Query{Intent: intent, Limit: 10}); len(got) != 0 {
			t.Fatalf("intent %q: boilerplate-only match returned %v, want none", intent, names(got))
		}
	}
	// The per-tool slot word is not boilerplate and still matches.
	if got := rankWith(t, tools, nil, router.Query{Intent: "slot05", Limit: 10}); !equalStrings(names(got), []string{"filler_05"}) {
		t.Fatalf("slot05: got %v, want [filler_05]", names(got))
	}
	// A real word shared with the boilerplate still matches where it is real.
	if got := rankWith(t, tools, nil, router.Query{Intent: "repository", Limit: 10}); !equalStrings(names(got), []string{"list_repos"}) {
		t.Fatalf("repository: got %v, want [list_repos]", names(got))
	}
}

// TestRankBoundsHugeIntent: a caller-supplied intent must not make Rank do
// unbounded work. Only the leading maxIntentBytes are read and only the first
// maxQueryTerms distinct terms score, so a hostile intent costs about what a
// normal one does and a match in the leading words still wins.
func TestRankBoundsHugeIntent(t *testing.T) {
	tools, entries := fixtureTools()
	var b strings.Builder
	b.WriteString("dead code ")
	for i := 0; b.Len() < 4<<20; i++ {
		fmt.Fprintf(&b, "w%d ", i)
	}
	r := New(tools, entries)
	start := time.Now()
	got, err := r.Rank(context.Background(), router.Query{Intent: b.String(), Limit: 3}, tools, entries, allowAll)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("Rank on a 4 MiB intent took %v, want it bounded", elapsed)
	}
	if len(got) == 0 || got[0].Name != "get_dead_code" {
		t.Fatalf("got %v, want get_dead_code first (leading words must still count)", names(got))
	}
	terms := queryTerms(b.String())
	if len(terms) != 32 || maxQueryTerms > 64 || maxIntentBytes > 8192 {
		t.Fatalf("queryTerms = %d terms (caps %d terms, %d bytes), want exactly 32 terms and sane caps",
			len(terms), maxQueryTerms, maxIntentBytes)
	}
	if terms[0] != "dead" || terms[1] != "code" {
		t.Fatalf("queryTerms = %v, want leading words first", terms[:3])
	}
}

func TestClipIntentCutsOnRuneBoundary(t *testing.T) {
	// "é" is two bytes; an odd limit offset would split one if the cut ignored
	// rune starts.
	in := "a" + strings.Repeat("é", maxIntentBytes)
	got := clipIntent(in)
	if len(got) > maxIntentBytes || !utf8.ValidString(got) {
		t.Fatalf("clipIntent = %d bytes, valid=%v; want <= %d and valid UTF-8", len(got), utf8.ValidString(got), maxIntentBytes)
	}
	if short := "dead code"; clipIntent(short) != short {
		t.Fatalf("clipIntent changed a short intent")
	}
}
