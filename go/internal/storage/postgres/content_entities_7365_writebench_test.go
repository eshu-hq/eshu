// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"log/slog"
	"math"
	"testing"
)

// TestEntityBenchCorpusShape pins the #7365 bench corpus contract: exact file
// and entity counts, one entity set per file path, and every 20th file
// carrying a secret-pattern line for trigger derivation.
func TestEntityBenchCorpusShape(t *testing.T) {
	records, entities := entityBenchCorpus(40, 25)
	if len(records) != 40 {
		t.Fatalf("files = %d, want 40", len(records))
	}
	if len(entities) != 1000 {
		t.Fatalf("entities = %d, want 1000", len(entities))
	}
	byPath := map[string]int{}
	for _, e := range entities {
		byPath[e.Path]++
		if e.EntityType == "" || e.EntityName == "" || e.StartLine <= 0 || e.SourceCache == "" {
			t.Fatalf("entity %+v misses a required write field", e)
		}
	}
	for _, r := range records {
		if byPath[r.Path] != 25 {
			t.Fatalf("path %s has %d entities, want 25", r.Path, byPath[r.Path])
		}
	}
	secret := 0
	for i, r := range records {
		has := len(r.Body) > 0 && containsSecretLineForBench(r.Body)
		if has {
			secret++
		}
		if (i%20 == 0) != has {
			t.Fatalf("file %d secret-pattern presence = %v, want %v", i, has, i%20 == 0)
		}
	}
	if secret != 2 {
		t.Fatalf("secret-pattern files = %d, want 2", secret)
	}
	again, entitiesAgain := entityBenchCorpus(40, 25)
	for i := range records {
		if records[i].Body != again[i].Body || entities[i*25].SourceCache != entitiesAgain[i*25].SourceCache {
			t.Fatalf("corpus is not deterministic at file %d", i)
		}
	}
}

func containsSecretLineForBench(body string) bool {
	for _, line := range splitLinesForBench(body) {
		if len(line) > 0 && (hasSubstr(line, "testPassword") || hasSubstr(line, "hunter2")) {
			return true
		}
	}
	return false
}

func splitLinesForBench(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func hasSubstr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestPrefixBenchEntityIDs pins the seed guard: per-repo namespacing keeps
// the base and measured populations disjoint under the global entity_id key,
// and the shared corpus is not mutated.
func TestPrefixBenchEntityIDs(t *testing.T) {
	_, entities := entityBenchCorpus(2, 3)
	a := prefixBenchEntityIDs(entities, "repo-a")
	b := prefixBenchEntityIDs(entities, "repo-b")
	seen := map[string]bool{}
	for i := range a {
		if a[i].EntityID == b[i].EntityID {
			t.Fatalf("repo collision at %d: %s", i, a[i].EntityID)
		}
		seen[a[i].EntityID] = true
		seen[b[i].EntityID] = true
		if entities[i].EntityID == a[i].EntityID {
			t.Fatalf("shared corpus mutated at %d", i)
		}
	}
	if len(seen) != 12 {
		t.Fatalf("distinct ids = %d, want 12", len(seen))
	}
}

// TestEntityBenchMedian pins median selection including the even-count upper
// middle the bench reports.
func TestEntityBenchMedian(t *testing.T) {
	if got := entityBenchMedian([]float64{3, 1, 2}); got != 2 {
		t.Fatalf("odd median = %v, want 2", got)
	}
	if got := entityBenchMedian([]float64{4, 1, 3, 2}); got != 3 {
		t.Fatalf("even median = %v, want 3", got)
	}
}

// TestEntityBenchMedianEmpty pins the missing-stage guard: an empty sample
// reports NaN instead of panicking, so a renamed or skipped writer stage
// surfaces as an obvious anomaly in the report.
func TestEntityBenchMedianEmpty(t *testing.T) {
	if got := entityBenchMedian(nil); !math.IsNaN(got) {
		t.Fatalf("empty median = %v, want NaN", got)
	}
}

// TestEntityBenchEnvInt pins the bench sizing override: blank or invalid
// input keeps the default, positive input wins.
func TestEntityBenchEnvInt(t *testing.T) {
	t.Setenv("ESHU_7365_BENCH_PROBE", "")
	if got := entityBenchEnvInt("ESHU_7365_BENCH_PROBE", 7); got != 7 {
		t.Fatalf("blank = %d, want 7", got)
	}
	t.Setenv("ESHU_7365_BENCH_PROBE", "nope")
	if got := entityBenchEnvInt("ESHU_7365_BENCH_PROBE", 7); got != 7 {
		t.Fatalf("invalid = %d, want 7", got)
	}
	t.Setenv("ESHU_7365_BENCH_PROBE", "12")
	if got := entityBenchEnvInt("ESHU_7365_BENCH_PROBE", 7); got != 12 {
		t.Fatalf("override = %d, want 12", got)
	}
}

// TestEntityBenchStageHandler pins the stage capture the bench medians rest
// on: one logStage record lands under its stage name with its duration.
func TestEntityBenchStageHandler(t *testing.T) {
	h := &entityBenchStageHandler{}
	logger := slog.New(h)
	logger.InfoContext(context.Background(), "content writer stage completed",
		"stage", "upsert_entities", "duration_seconds", 0.42)
	logger.InfoContext(context.Background(), "content writer stage completed",
		"stage", "upsert_entities", "duration_seconds", 0.5)
	if got := entityBenchMedian(h.stages["upsert_entities"]); got != 0.5 {
		t.Fatalf("captured median = %v, want 0.5", got)
	}
	if len(h.stages["upsert_files"]) != 0 {
		t.Fatalf("uncaptured stage has %d samples, want 0", len(h.stages["upsert_files"]))
	}
}
