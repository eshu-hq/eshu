// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"math"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestMigration069PredicateMatchesIdentityFactFilter locks the partial-index
// WHERE predicate in migrations/069_fact_records_identity_epoch_idx.sql to the
// Go identityFactFilterSQL const (facts_active_container_image_identity.go).
// These two filter predicates MUST stay identical: the index only covers the
// probe/load query with an Index Only Scan when its predicate is a subset
// match of the query's WHERE clause. If the two drift, the partial index
// silently stops covering probeIdentityEpochQuery / listActiveContainerImageIdentityFactsQuery
// and the epoch probe falls back to a full scan — a perf regression with no
// functional test failure to catch it, hence this drift lock.
//
// The migration predicate uses bare column names (fact_kind, source_system,
// payload); the Go const uses a `fact.`-prefixed alias because it is embedded
// in a query that joins fact_records AS fact. normalizeIdentityFilterPredicate
// accounts for that known, intentional difference before comparing.
func TestMigration069PredicateMatchesIdentityFactFilter(t *testing.T) {
	t.Parallel()

	migrationSQL, err := os.ReadFile("migrations/069_fact_records_identity_epoch_idx.sql")
	if err != nil {
		t.Fatalf("read migration 069: %v", err)
	}

	migrationFilter := normalizeIdentityFilterPredicate(extractIdentityFilter(string(migrationSQL)))
	goFilter := normalizeIdentityFilterPredicate(extractIdentityFilter(probeIdentityEpochQuery))

	if migrationFilter != goFilter {
		t.Fatalf(
			"migration 069 predicate drifted from identityFactFilterSQL:\nmigration: %s\ngo:        %s",
			migrationFilter, goFilter,
		)
	}

	for _, want := range []string{
		"oci_registry.image_tag_observation",
		"oci_registry.image_manifest",
		"oci_registry.image_index",
		"aws_image_reference",
		"azure_image_reference",
		"gcp_image_reference",
		"aws_relationship",
		"content_entity",
		"->>'target_type' = 'container_image'",
		"entity_metadata' ? 'container_images'",
		"metadata' ? 'container_images'",
	} {
		if !strings.Contains(migrationFilter, want) {
			t.Fatalf("migration 069 predicate missing %q", want)
		}
		if !strings.Contains(goFilter, want) {
			t.Fatalf("identityFactFilterSQL missing %q", want)
		}
	}
}

// TestProbeIdentityEpochQueryUsesCollisionResistantFingerprint locks the
// active_fingerprint subquery in probeIdentityEpochQuery
// (facts_active_container_image_identity.go) to the collision-resistant
// md5-digest-of-ordered-mapping shape (issue #5438 P1-B). The prior shape,
// `sum(hashtext(scope_id || ':' || active_generation_id))`, could silently
// fail to detect a real active-generation supersession — either via a
// 32-bit hashtext collision between two different active mappings, or via
// two offsetting per-scope deltas that cancel out in the sum — leaving the
// cache serving stale identity facts with no self-heal, since the
// fingerprint is deterministic for a given active mapping. This is a pure
// SQL-shape assertion (no DB required); TestIdentityEpochProbeDetectsSupersessionLive
// in identity_epoch_cache_live_test.go proves the fix against real Postgres.
func TestProbeIdentityEpochQueryUsesCollisionResistantFingerprint(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"md5(",
		"string_agg(",
		"ORDER BY scope_id",
	} {
		if !strings.Contains(probeIdentityEpochQuery, want) {
			t.Fatalf("probeIdentityEpochQuery missing %q:\n%s", want, probeIdentityEpochQuery)
		}
	}

	if strings.Contains(probeIdentityEpochQuery, "hashtext") {
		t.Fatalf("probeIdentityEpochQuery still uses the summed hashtext fingerprint, which can silently miss a real supersession (32-bit collision or offsetting deltas):\n%s", probeIdentityEpochQuery)
	}
}

var identityFilterWhitespaceRE = regexp.MustCompile(`\s+`)

// normalizeIdentityFilterPredicate strips the "fact." table alias (present
// only in the Go const, which is embedded in a query that joins fact_records
// AS fact; absent from the migration's bare-column partial-index predicate)
// and collapses whitespace runs, so alias or formatting differences don't
// cause a false drift failure — only an actual predicate mismatch should fail
// TestMigration069PredicateMatchesIdentityFactFilter.
func normalizeIdentityFilterPredicate(filter string) string {
	stripped := strings.ReplaceAll(filter, "fact.", "")
	collapsed := identityFilterWhitespaceRE.ReplaceAllString(stripped, " ")
	return strings.TrimSpace(collapsed)
}

// TestDefensiveCopyEnvelopesIsOneLevel is a canary for the deliberate
// one-level defensive-copy contract of defensiveCopyEnvelopes
// (identity_epoch_cache.go): it copies the top-level Payload map so callers
// cannot mutate the cache through a top-level key, but nested map values
// inside Payload are shared by reference with the cached copy. This test
// locks both halves of that contract. If a future change deepens or removes
// the copy, this test fails loudly, forcing a conscious decision (and an
// audit of identity-load callers) rather than a silent behavior change.
func TestDefensiveCopyEnvelopesIsOneLevel(t *testing.T) {
	t.Parallel()

	nested := map[string]any{"inner": "original"}
	src := []facts.Envelope{
		{
			FactID: "fact-1",
			Payload: map[string]any{
				"top":    "original",
				"nested": nested,
			},
		},
	}

	dst := defensiveCopyEnvelopes(src)

	// Top-level mutation on the copy must NOT reach src: defensiveCopyEnvelopes
	// allocates a fresh top-level Payload map for each envelope.
	dst[0].Payload["top"] = "mutated"
	if got := src[0].Payload["top"]; got != "original" {
		t.Fatalf("top-level mutation leaked into src: got %v, want %q", got, "original")
	}

	// Nested mutation on the copy MUST reach src: the copy is one level deep
	// only, so nested map values are shared by reference. This is the
	// intentional, documented boundary of defensiveCopyEnvelopes.
	dstNested, ok := dst[0].Payload["nested"].(map[string]any)
	if !ok {
		t.Fatalf("dst nested payload value has unexpected type %T", dst[0].Payload["nested"])
	}
	dstNested["inner"] = "mutated"

	srcNested, ok := src[0].Payload["nested"].(map[string]any)
	if !ok {
		t.Fatalf("src nested payload value has unexpected type %T", src[0].Payload["nested"])
	}
	if got := srcNested["inner"]; got != "mutated" {
		t.Fatalf("nested mutation did not reach src (one-level copy contract broken): got %v, want %q", got, "mutated")
	}
}

// TestEstimateEnvelopesByteSizeSumsMarshalableSize proves the happy path of
// estimateEnvelopesByteSize (identity_epoch_cache.go): a marshalable Payload
// contributes its own JSON-encoded length to the total, on top of the summed
// string-field lengths and the fixed per-envelope overhead, and no error is
// returned.
func TestEstimateEnvelopesByteSizeSumsMarshalableSize(t *testing.T) {
	t.Parallel()

	env := facts.Envelope{
		FactID:        "fact-1",  // 6 bytes
		ScopeID:       "scope-1", // 7 bytes
		StableFactKey: "key-1",   // 5 bytes
		Payload:       map[string]any{"a": "b"},
	}
	payloadJSON, err := json.Marshal(env.Payload)
	if err != nil {
		t.Fatalf("marshal test payload: %v", err)
	}

	got, err := estimateEnvelopesByteSize([]facts.Envelope{env})
	if err != nil {
		t.Fatalf("estimateEnvelopesByteSize returned unexpected error: %v", err)
	}

	want := int64(len(env.FactID)+len(env.ScopeID)+len(env.StableFactKey)+len(payloadJSON)) + 40
	if got != want {
		t.Fatalf("estimateEnvelopesByteSize = %d, want %d (fields + %d payload bytes + 40 overhead)", got, want, len(payloadJSON))
	}
	if got <= 0 {
		t.Fatalf("estimateEnvelopesByteSize = %d, want > 0", got)
	}
}

// TestEstimateEnvelopesByteSizeErrorsOnUnsizablePayload proves the defensive
// error path of estimateEnvelopesByteSize (identity_epoch_cache.go, issue
// #5438 P2-2): when a Payload value cannot be sized by json.Marshal (here, a
// NaN float — json.Marshal rejects NaN/Inf), estimateEnvelopesByteSize
// returns a non-nil error rather than silently treating the unsizable
// envelope as 0 bytes. The caller (IdentityEpochCache.get) treats this error
// the same as cap-exceeded and serves the set uncached, so an unsizable
// payload can never slip under the cache's maxBytes cap by being undercounted.
func TestEstimateEnvelopesByteSizeErrorsOnUnsizablePayload(t *testing.T) {
	t.Parallel()

	env := facts.Envelope{
		FactID:  "fact-unsizable",
		Payload: map[string]any{"nan": math.NaN()},
	}

	_, err := estimateEnvelopesByteSize([]facts.Envelope{env})
	if err == nil {
		t.Fatalf("estimateEnvelopesByteSize returned nil error for a NaN payload, want a marshal error")
	}
}
