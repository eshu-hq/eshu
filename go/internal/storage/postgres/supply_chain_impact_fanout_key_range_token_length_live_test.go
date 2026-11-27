// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// tokenLongBytes is the length of the long member the #10643 PR 2 review
// (finding F1, arbiter ruling 10643m) used: 3,200 incompressible bytes. A
// GIN array_ops entry cannot exceed 2,712 bytes, so an untruncated token of
// it fails the insert with "index row size 3216 exceeds maximum 2712".
const tokenLongBytes = 3200

// incompressibleID returns an n-byte ASCII id that pglz cannot shrink: the
// hex of a SHA-256 chain seeded by seed, so the stored and indexed value
// keeps its full length.
func incompressibleID(seed string, n int) string {
	var b strings.Builder
	sum := sha256.Sum256([]byte(seed))
	for b.Len() < n {
		b.WriteString(hex.EncodeToString(sum[:]))
		sum = sha256.Sum256(sum[:])
	}
	return b.String()[:n]
}

// tokenLengthMembers wraps a members value in a finding payload with its own
// cve_id and advisory_id.
func tokenLengthMembers(members string) string {
	return `{"cve_id": "CVE-len-own", "advisory_id": "GHSA-len-own", "impact_status": "affected_exact", "provenance": {"advisory_group_members": ` + members + `}}`
}

// TestHolderKeyRangeTokenLongMemberInsertLive is the F1 regression: with the
// token index present (the migration path), a finding whose foreign member is
// 3,200 incompressible bytes inserts, as an array element, an object key and
// a string scalar. The function leaves the long string untokenized and keeps
// a short member of the same row.
func TestHolderKeyRangeTokenLongMemberInsertLive(t *testing.T) {
	ctx, sqldb := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, sqldb, "len:insert", "g1")
	kind := facts.ReducerSupplyChainImpactFindingFactKind
	long := strconv.Quote("GHSA-long-" + incompressibleID("insert", tokenLongBytes-len("GHSA-long-")))
	for _, tc := range []struct {
		name, factID, members string
		want                  []string // stored tokens; nil is SQL NULL
	}{
		{"array element", kind + ":1a01", `["GHSA-len-short", ` + long + `]`, []string{"2:GHSA-len-short"}},
		{"object key", kind + ":1a02", `{` + long + `: 1}`, nil},
		{"string scalar", kind + ":1a03", long, nil},
	} {
		payload := tokenLengthMembers(tc.members)
		// The plant is frozen finding history, so it runs in a scoped
		// postgresproof.SeedFindingFacts window: the proof database stays
		// armed and the token reads under test run armed. A failed plant
		// still reports every shape, so the flag carries the skip out of
		// the window (continue cannot cross the closure).
		planted := true
		postgresproof.SeedFindingFacts(t, sqldb, func() {
			if _, err := sqldb.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, 'len:insert', 'len:insert/g1', $2, $1, '1.0.0', 'synthetic', 'len', now(), now(), false, $3::jsonb)`,
				tc.factID, kind, payload); err != nil {
				// Report every shape, so the array, object and scalar cases each
				// show their own failure.
				t.Errorf("%s: insert a finding with a %d-byte foreign member: %v", tc.name, tokenLongBytes, err)
				planted = false
			}
		})
		if !planted {
			continue
		}
		var tokens []string
		var stored *string
		if err := sqldb.QueryRowContext(ctx, `SELECT `+postgres.SupplyChainImpactForeignMemberTokensFunction+`(fact_id, payload)::text
FROM fact_records WHERE fact_id = $1`, tc.factID).Scan(&stored); err != nil {
			t.Fatalf("%s: read tokens: %v", tc.name, err)
		}
		if stored != nil {
			tokens = strings.Split(strings.Trim(*stored, "{}"), ",")
		}
		if !slices.Equal(tokens, tc.want) {
			t.Fatalf("%s: stored tokens = %v, want %v", tc.name, tokens, tc.want)
		}
	}
}

// TestHolderKeyRangeTokenLengthBoundaryLive pins the bound in bytes: a member
// of exactly HolderMemberTokenMaxBytes bytes is tokenized and one byte more is
// not, for ASCII and for two-byte characters (512 of them are 1,024 bytes,
// 513 are 1,026), and 256 four-byte characters fit. Each row also inserts
// with the index present, so the longest token fits a GIN entry. jsonb ?|
// matches every one of them; the shipped statement serves the long ones.
func TestHolderKeyRangeTokenLengthBoundaryLive(t *testing.T) {
	ctx, sqldb := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, sqldb, "len:boundary", "g1")
	kind := facts.ReducerSupplyChainImpactFindingFactKind
	ascii := incompressibleID("boundary", postgres.HolderMemberTokenMaxBytes)
	n := 0
	for _, tc := range []struct {
		name, member string
		tokenized    bool
	}{
		{"1,024 ASCII bytes", ascii, true},
		{"1,025 ASCII bytes", ascii + "z", false},
		{"512 two-byte characters", strings.Repeat("é", 512), true},
		{"513 two-byte characters", strings.Repeat("é", 513), false},
		{"256 four-byte characters", strings.Repeat("𝄞", 256), true},
	} {
		quoted := strconv.Quote(tc.member)
		for _, shape := range []struct{ name, members string }{
			{"array", `[` + quoted + `]`},
			{"object", `{` + quoted + `: 1}`},
			{"scalar", quoted},
		} {
			n++
			factID := kind + ":1b" + strconv.FormatInt(int64(n), 16) + "0"
			payload := tokenLengthMembers(shape.members)
			// The plant is frozen finding history, so it runs in a scoped
			// postgresproof.SeedFindingFacts window: the proof database stays
			// armed and the token reads under test run armed.
			postgresproof.SeedFindingFacts(t, sqldb, func() {
				if _, err := sqldb.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, 'len:boundary', 'len:boundary/g1', $2, $1, '1.0.0', 'synthetic', 'len', now(), now(), false, $3::jsonb)`,
					factID, kind, payload); err != nil {
					t.Fatalf("%s %s: insert: %v", tc.name, shape.name, err)
				}
			})
			var tokenized, shipped bool
			if err := sqldb.QueryRowContext(ctx, `
SELECT COALESCE(`+postgres.SupplyChainImpactForeignMemberTokensFunction+`(fact_id, payload) @> ARRAY['1:' || $2::text], false),
       COALESCE((payload #> '{provenance,advisory_group_members}') ?| ARRAY[$2::text], false)
FROM fact_records WHERE fact_id = $1`, factID, tc.member).Scan(&tokenized, &shipped); err != nil {
				t.Fatalf("%s %s: read: %v", tc.name, shape.name, err)
			}
			if !shipped {
				t.Fatalf("%s %s: jsonb ?| does not match the member; the case is wrong", tc.name, shape.name)
			}
			if tokenized != tc.tokenized {
				t.Fatalf("%s %s (%d bytes): tokenized = %t, want %t", tc.name, shape.name, len(tc.member), tokenized, tc.tokenized)
			}
		}
	}
}

// TestHolderKeyRangeLongLookupFallsBackLive is the differential for long
// ids (arbiter ruling 10643m decision 4). A row carries a 3,200-byte foreign
// member and a short one. Looked up by the long id, the key-range read falls
// back to one shipped holder statement and finds it; by the short id, the
// token arm finds it; a lookup set mixing the long id and the alias falls
// back as a whole. Each read equals FindHolderScopes. The seeded RED, the 16
// token statements for the long id, loses the holder: without the fallback a
// long id is read only through tokens the function never stores.
func TestHolderKeyRangeLongLookupFallsBackLive(t *testing.T) {
	ctx, sqldb := fanOutLiveDB(t)
	// The seed plants frozen finding facts, so it runs in a scoped
	// postgresproof.SeedFindingFacts window: the proof database stays armed
	// and the holder reads under test run armed.
	postgresproof.SeedFindingFacts(t, sqldb, func() {
		tokenSeed(t, ctx, sqldb)
	})
	fanOutSeedScope(t, ctx, sqldb, "tok:long", "g1")
	kind := facts.ReducerSupplyChainImpactFindingFactKind
	long := "GHSA-long-" + incompressibleID("fallback", tokenLongBytes-len("GHSA-long-"))
	const short = "GHSA-tok-long-partner"
	// The plant is frozen finding history, so it runs in a scoped
	// postgresproof.SeedFindingFacts window: the proof database stays armed
	// and the holder reads under test run armed.
	postgresproof.SeedFindingFacts(t, sqldb, func() {
		if _, err := sqldb.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, 'tok:long', 'tok:long/g1', $2, $1, '1.0.0', 'synthetic', 'tok', now(), now(), false, $3::jsonb)`,
			kind+":0c12", kind, `{"cve_id": "CVE-tok-x", "advisory_id": "GHSA-tok-x", "impact_status": "affected_exact",
  "provenance": {"advisory_group_members": [`+strconv.Quote(long)+`, `+strconv.Quote(short)+`]}}`); err != nil {
			t.Fatalf("seed the long-member holder: %v", err)
		}
	})
	store := fanOutLiveStore(sqldb)
	ranges := supplychaincore.HolderKeyRanges()
	for _, tc := range []struct {
		name     string
		ids      []string
		fallback bool
	}{
		{"long id", []string{long}, true},
		{"short member of the same row", []string{short}, false},
		{"mixed long id and alias", []string{tokenAlias, long}, true},
	} {
		shipped, err := store.FindHolderScopes(ctx, tokenOwn, tc.ids)
		if err != nil || !slices.Contains(shipped, "tok:long") {
			t.Fatalf("%s: shipped holders = %v, %v; want tok:long among them (the seed is wrong)", tc.name, shipped, err)
		}
		read, err := store.FindHolderScopesByKeyRange(ctx, tokenOwn, tc.ids, ranges)
		if err != nil || !slices.Equal(read.Scopes, shipped) {
			t.Fatalf("%s: key-range holders = %v, %v; want the shipped set %v", tc.name, read.Scopes, err, shipped)
		}
		if read.Fallback != tc.fallback {
			t.Fatalf("%s: fallback = %t, want %t", tc.name, read.Fallback, tc.fallback)
		}
		want := len(ranges)
		if tc.fallback {
			want = 1
		}
		if len(read.Statements) != want {
			t.Fatalf("%s: %d statements ran, want %d", tc.name, len(read.Statements), want)
		}
		if tc.fallback && (read.Statements[0].Kind != telemetry.FanOutReadKindHolders || read.Statements[0].Range != 1) {
			t.Fatalf("%s: fallback statement = %+v, want kind %s range 1", tc.name, read.Statements[0], telemetry.FanOutReadKindHolders)
		}
	}
	// Seeded RED: the token statements alone, as the reader ran them before
	// the fallback, cannot find a holder by an untokenized long id.
	if got := tokenRead(t, ctx, sqldb, shippedTokenStatement([]string{long}, len(ranges))); slices.Contains(got, "tok:long") {
		t.Fatalf("seeded RED: the token statements found tok:long by its %d-byte id; the fallback is untested: %v", tokenLongBytes, got)
	}
}
