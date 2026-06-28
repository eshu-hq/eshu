// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"
)

// B-6 (#3799) retry/idempotency replay suite.
//
// The retry path historically had far fewer tests than the lease path. This
// suite closes that gap with a registry-driven proof: for every reducer domain
// in DefaultDomainDefinitions() we either replay its emit path twice with the
// same input (same FencingToken on the loaded facts) and assert the projection
// does not duplicate, or we record an explicit, documented exemption.
//
// "No duplicate projection" is modeled faithfully. Reducer idempotency is
// enforced by MERGE semantics on stable keys, not by an explicit fencing-token
// comparison in the write path. Each case therefore captures the rows a handler
// writes through a recording fake, accumulates the rows across BOTH replays, and
// deduplicates them by the handler's own MERGE identity. A correct (idempotent)
// handler produces a deduplicated projection after two runs that is byte-for-byte
// identical to the projection after one run: no growth, no duplicate identities,
// stable identities across replays. A handler that minted a fresh identity per
// run, or that appended instead of merging, would fail the count assertion.

// idempotencyReplayCase drives one reducer domain's emit path twice with the
// same input and exposes the rows it wrote for MERGE-identity deduplication.
type idempotencyReplayCase struct {
	// domain names the reducer domain this case proves idempotent.
	domain Domain
	// run executes the handler's emit path once against a fresh recording fake
	// and returns the projected rows as identity-keyed entries. The same input
	// (including the same FencingToken on the loaded facts) MUST be used on every
	// invocation so the only variable under test is replay, not input drift.
	run func(t *testing.T) []idempotencyRow
}

// idempotencyRow is one projected unit of truth a reducer emit path produced,
// reduced to the stable MERGE identity the projection upserts on plus the full
// row contents used to prove the replayed row is identical, not merely
// same-keyed.
type idempotencyRow struct {
	// identity is the stable MERGE key the projection deduplicates on (node uid,
	// edge identity, durable intent id, or canonical write key). Two rows with
	// the same identity collapse to one row in the materialized graph.
	identity string
	// contents is the full comparable row payload. Replaying the same input MUST
	// reproduce byte-identical contents for the same identity; otherwise a replay
	// would overwrite truth with a different value under a stable key.
	contents string
}

// mergeDedup models the projection's MERGE-on-identity behavior: rows sharing an
// identity collapse to one, and a stable identity that reappears with different
// contents is a correctness failure (a MERGE that rewrites truth on replay).
func mergeDedup(t *testing.T, rows []idempotencyRow) map[string]string {
	t.Helper()
	deduped := make(map[string]string, len(rows))
	for _, row := range rows {
		if existing, ok := deduped[row.identity]; ok && existing != row.contents {
			t.Fatalf(
				"MERGE identity %q produced divergent contents across rows:\n  %s\n  %s",
				row.identity, existing, row.contents,
			)
		}
		deduped[row.identity] = row.contents
	}
	return deduped
}

// assertIdempotentReplay runs the case once, then replays it with the same
// input, and proves the accumulated projection after two runs deduplicates to
// exactly the single-run projection: same identities, same contents, no growth.
func assertIdempotentReplay(t *testing.T, c idempotencyReplayCase) {
	t.Helper()

	first := c.run(t)
	if len(first) == 0 {
		t.Fatalf("domain %q: emit path produced no projected rows; fixture must drive at least one write", c.domain)
	}
	single := mergeDedup(t, first)

	second := c.run(t)

	// Accumulate both replays the way a durable upsert target would, then MERGE.
	combined := mergeDedup(t, append(append([]idempotencyRow(nil), first...), second...))

	if len(combined) != len(single) {
		t.Fatalf(
			"domain %q: projection grew under replay: single-run identities=%d, after two replays=%d (duplicate projection)",
			c.domain, len(single), len(combined),
		)
	}
	if !reflect.DeepEqual(combined, single) {
		t.Fatalf(
			"domain %q: replayed projection differs from single-run projection:\nsingle=%v\ncombined=%v",
			c.domain, single, combined,
		)
	}

	// The second run on its own must reproduce identical identities and contents,
	// proving the handler is deterministic for the same FencingToken input.
	if !reflect.DeepEqual(mergeDedup(t, second), single) {
		t.Fatalf("domain %q: second replay diverged from first replay (non-deterministic emit path)", c.domain)
	}
}

// TestReducerEmitPathsAreIdempotentUnderReplay replays each covered reducer
// domain's emit path twice with the same input and asserts no duplicate
// projection.
func TestReducerEmitPathsAreIdempotentUnderReplay(t *testing.T) {
	t.Parallel()

	for _, c := range idempotencyReplayCases() {
		c := c
		t.Run(string(c.domain), func(t *testing.T) {
			t.Parallel()
			assertIdempotentReplay(t, c)
		})
	}
}

// TestReducerIdempotencyCoverageGuard fails if any DefaultDomainDefinitions()
// domain is neither covered by a replay case nor explicitly exempted. This is
// the drift guard: a newly registered reducer domain forces an author to either
// add an idempotency replay case or record a documented exemption, so "each
// reducer" coverage cannot silently erode.
func TestReducerIdempotencyCoverageGuard(t *testing.T) {
	t.Parallel()

	covered := make(map[Domain]struct{})
	for _, c := range idempotencyReplayCases() {
		if _, dup := covered[c.domain]; dup {
			t.Fatalf("duplicate idempotency replay case for domain %q", c.domain)
		}
		covered[c.domain] = struct{}{}
	}

	var missing []string
	for _, def := range DefaultDomainDefinitions() {
		if _, ok := covered[def.Domain]; ok {
			continue
		}
		if _, exempt := idempotencyExemptDomains[def.Domain]; exempt {
			continue
		}
		missing = append(missing, string(def.Domain))
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Fatalf(
			"reducer domain(s) lack an idempotency replay case and are not exempted: %v\n"+
				"add a case to idempotencyReplayCases() or an entry to idempotencyExemptDomains with a reason",
			missing,
		)
	}

	// Every exemption must name a real DefaultDomainDefinitions() domain and carry
	// a non-empty reason, so the exemption set cannot rot into stale or blank
	// entries.
	defined := make(map[Domain]struct{})
	for _, def := range DefaultDomainDefinitions() {
		defined[def.Domain] = struct{}{}
	}
	for domain, reason := range idempotencyExemptDomains {
		if _, ok := defined[domain]; !ok {
			t.Errorf("exempt domain %q is not in DefaultDomainDefinitions(); remove the stale exemption", domain)
		}
		if _, ok := covered[domain]; ok {
			t.Errorf("domain %q is both covered and exempted; drop the exemption", domain)
		}
		if reason == "" {
			t.Errorf("exempt domain %q has a blank reason; document why it cannot be unit-replayed", domain)
		}
	}
}

// stableRowContents renders a map row into a deterministic comparable string so
// two replays of the same input compare equal regardless of map iteration order.
func stableRowContents(row map[string]any) string {
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b []byte
	for _, k := range keys {
		b = append(b, fmt.Sprintf("%s=%v;", k, row[k])...)
	}
	return string(b)
}

// drainContext is a tiny helper so cases read clearly at the call site.
func drainContext() context.Context { return context.Background() }
