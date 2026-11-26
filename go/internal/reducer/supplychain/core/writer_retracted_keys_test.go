// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// The retracted-keys table (#10880 PR-C1 c) keeps what a retraction removed: the
// recovery reads used to find retracted findings as tombstoned fact rows, and
// PR-C stops tombstoning facts. These tests pin the statements hermetically: the
// retraction statements only gain the fill, the recovery reads only gain two
// legs and keep the fact read as the transition leg, and every added piece is a
// shared fragment so the two retraction statements cannot drift.

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// The recovery-read digests are those of the two fact statements at origin/main
// f1f45ddad3, before C1 c: the frozen text each fact leg must still equal.
const (
	baseCVEIDsQueryDigest   = "4ddd8b647ea5604443de05b555bfc4e514d415cc6d67c8ebde5d4b32f71c3d50"
	baseGroupIDsQueryDigest = "c9b079f3198ea4933c820e54c2f36820871258c158c5da9346df5c40b294d504"
	// baseRetractStagingTailDigest is the digest of both retraction statements from the
	// dirty staging CTE to the end, with the key record removed, as they stood when the
	// retraction tombstoned fact rows (origin/main 4c547ccff7 plus PR-C1). PR-C2 i changed
	// only what feeds that tail, so the tail must still hash to it.
	baseRetractStagingTailDigest = "c306941c58fae0bbef972065ee55a1efb4d31c7b2c0cd452a6b69d1e004bc4ca"
)

// TestRetractionStatementsKeepTheStagingTailAndGainTheKeyFill proves both
// retraction statements still stage and count exactly as the tombstone statements
// did: from the dirty staging CTE on, with the shared key record removed, each
// hashes to the base tail, and each carries the shared fragments once, so the two
// cannot drift. Only the head (what is retracted, and from where) changed.
func TestRetractionStatementsKeepTheStagingTailAndGainTheKeyFill(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"replace-set": retractSupersededSupplyChainImpactFindingsQuery,
		"scoped":      retractSupersededSupplyChainImpactFindingsScopedQuery,
	} {
		for fragment, text := range map[string]string{
			"returning": retractedSupportsReturningSQL,
			"staging":   retractedStagingSQL,
			"record":    retractedFindingKeysRecordSQL,
			"counts":    retractedCountsSQL,
		} {
			if got := strings.Count(query, text); got != 1 {
				t.Errorf("%s statement carries the %s fragment %d times, want 1", name, fragment, got)
			}
		}
		_, tail, ok := strings.Cut(query, "\nordered AS (")
		if !ok {
			t.Fatalf("%s statement has no staging CTE", name)
		}
		tail = strings.Replace(tail, retractedFindingKeysRecordSQL, "", 1)
		// The first count's alias was renamed with the statement's meaning (tombstoned to
		// deleted); the rest of the tail is the base text.
		tail = strings.Replace(tail, "AS deleted,", "AS tombstoned,", 1)
		if got := sha256Hex(tail); got != baseRetractStagingTailDigest {
			t.Errorf("%s statement tail without the key record hashes to %s, want the base %s: staging or counts changed", name, got, baseRetractStagingTailDigest)
		}
	}
}

// TestRetractedKeysFillShape pins what the fill writes: the pair's generation
// and scope, a sha256 key hash (never md5: the FIPS gate), the cve id, advisory
// id and member set the retracted rows carried, once per distinct key, and a
// NOT EXISTS on the primary key so a replay of a retraction adds no row. It has
// no ON CONFLICT: the retraction statements keep none on their hot path.
func TestRetractedKeysFillShape(t *testing.T) {
	t.Parallel()

	fill := retractedFindingKeysRecordSQL
	for name, want := range map[string]string{
		"target table":   "INSERT INTO supply_chain_impact_retracted_finding_keys",
		"columns":        "(generation_id, scope_id, key_hash, cve_id, advisory_id, members)",
		"generation arg": "$2::text",
		"scope arg":      "$1::text",
		"hash":           "encode(sha256(convert_to(",
		"distinct":       "SELECT DISTINCT",
		"source":         "FROM retracted",
		"idempotent":     "WHERE NOT EXISTS (",
		"pk probe":       "known.generation_id = $2::text",
		"pk probe scope": "known.scope_id = $1::text",
		"pk probe key":   "known.key_hash = hashed.key_hash",
	} {
		if !strings.Contains(fill, want) {
			t.Errorf("the key fill lacks its %s: %q", name, want)
		}
	}
	if strings.Contains(fill, "ON CONFLICT") {
		t.Fatal("the key fill uses ON CONFLICT: the retraction statements keep no conflict arbiter on their hot path")
	}
	if strings.Contains(strings.ToLower(fill), "md5") {
		t.Fatal("the key fill uses md5: the FIPS sweep forbids it, use sha256")
	}
	returning := retractedSupportsReturningSQL
	for _, want := range []string{"\n        cve_id,", "AS members", "advisory_group_members"} {
		if !strings.Contains(returning, want) {
			t.Errorf("the RETURNING fragment lacks %q", want)
		}
	}
}

// TestRecoveryReadsKeepTheFactReadAsTheTransitionLeg proves each recovery read is
// the supports leg, the retracted-keys leg and the unchanged fact read, joined by
// UNION (a set, so the legs dedupe), and that the fact leg is byte-identical to
// the base statement: it covers rows retracted before the table existed.
func TestRecoveryReadsKeepTheFactReadAsTheTransitionLeg(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		query, factsLeg, base string
	}{
		"cve ids":   {listSupplyChainImpactCVEIDsQuery, listSupplyChainImpactCVEIDsFactsLegQuery, baseCVEIDsQueryDigest},
		"group ids": {listSupplyChainImpactGroupIDsQuery, listSupplyChainImpactGroupIDsFactsLegQuery, baseGroupIDsQueryDigest},
	} {
		if got := sha256Hex(c.factsLeg); got != c.base {
			t.Errorf("%s: the fact leg hashes to %s, want the base statement %s", name, got, c.base)
		}
		if !strings.Contains(c.query, c.factsLeg) {
			t.Errorf("%s: the recovery read does not contain its fact leg verbatim", name)
		}
		for leg, want := range map[string]string{
			"supports leg":       "FROM supply_chain_impact_finding_supports AS s",
			"retracted keys leg": "FROM supply_chain_impact_retracted_finding_keys AS k",
		} {
			if !strings.Contains(c.query, want) {
				t.Errorf("%s: the recovery read lacks its %s: %q", name, leg, want)
			}
		}
		if got := strings.Count(c.query, "\nUNION\n"); got != 2 {
			t.Errorf("%s: the recovery read joins %d times with UNION, want 2 (three legs)", name, got)
		}
		if strings.Contains(c.query, "UNION ALL") {
			t.Errorf("%s: the legs are joined with UNION ALL, want UNION so the legs dedupe", name)
		}
		// Every leg takes the same scope, generation and previous-generation window.
		if got := strings.Count(c.query, "IN ($2, "+priorGenerationSupplyChainImpactSQL+")"); got != 3 {
			t.Errorf("%s: %d legs carry the shared generation window, want 3", name, got)
		}
		for _, leg := range []string{"WHERE s.scope_id = $1", "WHERE k.scope_id = $1", "AND scope_id = $1"} {
			if got := strings.Count(c.query, leg); got != 1 {
				t.Errorf("%s: %d legs carry %q, want 1", name, got, leg)
			}
		}
	}
	// Each leg selects what the reader scans: (cve id, members) and (cve id, advisory id).
	if !strings.Contains(listSupplyChainImpactCVEIDsQuery, "s.cve_id, COALESCE(s.payload #>> '{provenance,advisory_group_members}', '[]')") ||
		!strings.Contains(listSupplyChainImpactCVEIDsQuery, "k.cve_id, k.members") {
		t.Error("the cve id recovery read's new legs do not select (cve id, members)")
	}
	if !strings.Contains(listSupplyChainImpactGroupIDsQuery, "s.cve_id, s.advisory_id") ||
		!strings.Contains(listSupplyChainImpactGroupIDsQuery, "k.cve_id, k.advisory_id") {
		t.Error("the group id recovery read's new legs do not select (cve id, advisory id)")
	}
}
