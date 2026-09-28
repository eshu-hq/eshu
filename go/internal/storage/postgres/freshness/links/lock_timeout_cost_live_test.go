// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// costEnv gates the W11 measurement of arbiter ruling arb-7127-3e-wait: it is
// a timing run, driven by an interleaving script over two builds, not a gate.
const costEnv = "ESHU_7127_W11_COST"

// TestLinkCostW11 measures LinkNext wall time on the two paths the generation
// lock timeout adds round trips to: a delta break (the set before the lock)
// and a full incremental link (the set, then the reset before the
// statement). It seeds 30 scopes per path, links each once, and prints one
// JSON line of per-path samples in microseconds.
func TestLinkCostW11(t *testing.T) {
	if os.Getenv(costEnv) == "" {
		t.Skipf("set %s to run the W11 timing measurement", costEnv)
	}
	l := openLedgerDB(t)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	const perPath = 30
	for i := range perPath {
		full, delta := fmt.Sprintf("full-%02d", i), fmt.Sprintf("delta-%02d", i)
		l.seedRootedScope(t, w, full, full+"-0", full+"-1")
		l.seedScope(t, delta)
		l.seedGeneration(t, delta, delta+"-0", false, "superseded", fixtureEpoch, fixtureEpoch.Add(time.Hour))
		l.seedGeneration(t, delta, delta+"-d1", true, "active", fixtureEpoch.Add(time.Hour), time.Time{})
		l.insertFacts(t, delta, delta+"-0", baseFacts())
		l.journal(t, delta, delta+"-0", "")
		l.journal(t, delta, delta+"-d1", delta+"-0")
		mustLink(t, w, l, delta)
	}
	samples := map[string][]int64{}
	for i := range perPath {
		for _, path := range []string{"full", "delta"} {
			scope := fmt.Sprintf("%s-%02d", path, i)
			start := time.Now()
			res := mustLink(t, w, l, scope)
			samples[path] = append(samples[path], time.Since(start).Microseconds())
			want := linksfreshnessstore.LinkKindIncremental
			if path == "delta" {
				want = linksfreshnessstore.LinkKindNone
			}
			if res.Kind != want {
				t.Fatalf("%s: link = %+v, want kind %s", scope, res, want)
			}
		}
	}
	for path := range samples {
		slices.Sort(samples[path])
	}
	line, _ := json.Marshal(samples)
	fmt.Println("W11 " + string(line))
}
