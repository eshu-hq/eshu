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

// The digests are those of the four statements at origin/main f1f45ddad3, before
// C1 c: the frozen text each derived statement must reduce to.
const (
	baseRetractQueryDigest       = "56ac1d7e1230533276ea045a51c6fc3bfd6b679e5d3d40853e4183876fa7db29"
	baseRetractScopedQueryDigest = "b8bfbc5f5ce1b0dbdf38976673ef2d463b9bb4dbeafa5838de1c291b1bc21837"
	baseCVEIDsQueryDigest        = "4ddd8b647ea5604443de05b555bfc4e514d415cc6d67c8ebde5d4b32f71c3d50"
	baseGroupIDsQueryDigest      = "c9b079f3198ea4933c820e54c2f36820871258c158c5da9346df5c40b294d504"
)

// TestRetractionStatementsOnlyGainTheKeyFill proves both retraction statements
// reduce to their base text once the two shared fragments are removed, so nothing
// else about the tombstone, the dirty staging or the counts changed, and that the
// fill is the same text in both.
func TestRetractionStatementsOnlyGainTheKeyFill(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct{ query, base string }{
		"replace-set": {retractSupersededSupplyChainImpactFindingsQuery, baseRetractQueryDigest},
		"scoped":      {retractSupersededSupplyChainImpactFindingsScopedQuery, baseRetractScopedQueryDigest},
	} {
		if got := strings.Count(c.query, retractedFindingKeysReturningSQL); got != 1 {
			t.Errorf("%s statement carries the RETURNING fragment %d times, want 1", name, got)
		}
		if got := strings.Count(c.query, retractedFindingKeysRecordSQL); got != 1 {
			t.Errorf("%s statement carries the record fragment %d times, want 1", name, got)
		}
		stripped := strings.Replace(c.query, retractedFindingKeysReturningSQL, "", 1)
		stripped = strings.Replace(stripped, retractedFindingKeysRecordSQL, "", 1)
		if got := sha256Hex(stripped); got != c.base {
			t.Errorf("%s statement without the key fill hashes to %s, want the base %s: something else changed", name, got, c.base)
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
		"generation arg": "$3::text",
		"scope arg":      "$2::text",
		"hash":           "encode(sha256(convert_to(",
		"distinct":       "SELECT DISTINCT",
		"source":         "FROM retracted",
		"idempotent":     "WHERE NOT EXISTS (",
		"pk probe":       "known.generation_id = $3::text",
		"pk probe scope": "known.scope_id = $2::text",
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
	returning := retractedFindingKeysReturningSQL
	for _, want := range []string{"AS cve_id", "AS members", "advisory_group_members"} {
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
