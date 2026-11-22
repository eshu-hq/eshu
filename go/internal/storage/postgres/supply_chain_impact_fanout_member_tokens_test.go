// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// memberTokenBuckets is the bucket count the SQL token function can emit:
// strpos over the 16 lower-case hex digits gives 1 to 16, and 0 for any
// other last character of the fact id.
const memberTokenBuckets = 17

// TestHolderMemberTokensPartitionTheBuckets proves the lookup tokens of a
// ranged read cover every (bucket, id) pair exactly once over its statements,
// for the shipped 16 ranges and for other counts, so the token arms partition
// the foreign rows whatever the range count. With 16 statements, statement 1
// carries buckets 0 and 1 and statement k carries bucket k.
func TestHolderMemberTokensPartitionTheBuckets(t *testing.T) {
	t.Parallel()

	ids := []string{"GHSA-aaaa-bbbb-cccc", "GHSA:colon:id", "2:X"}
	for _, statements := range []int{len(supplychaincore.HolderKeyRanges()), 1, 3, 17, 40} {
		seen := map[string]int{}
		for i := 0; i < statements; i++ {
			for _, token := range postgres.HolderMemberTokens(ids, i, statements) {
				seen[token]++
			}
		}
		for bucket := 0; bucket < memberTokenBuckets; bucket++ {
			for _, id := range ids {
				token := strconv.Itoa(bucket) + ":" + id
				if seen[token] != 1 {
					t.Fatalf("statements=%d: token %q carried by %d statements, want exactly 1", statements, token, seen[token])
				}
				delete(seen, token)
			}
		}
		if len(seen) != 0 {
			t.Fatalf("statements=%d: unexpected tokens %v", statements, seen)
		}
	}

	ranges := len(supplychaincore.HolderKeyRanges())
	first := postgres.HolderMemberTokens([]string{"X"}, 0, ranges)
	slices.Sort(first)
	if want := []string{"0:X", "1:X"}; !slices.Equal(first, want) {
		t.Fatalf("statement 1 tokens = %v, want %v (bucket 0 rides in statement 1)", first, want)
	}
	for i := 1; i < ranges; i++ {
		got := postgres.HolderMemberTokens([]string{"X"}, i, ranges)
		if want := []string{strconv.Itoa(i+1) + ":X"}; !slices.Equal(got, want) {
			t.Fatalf("statement %d tokens = %v, want %v", i+1, got, want)
		}
	}
	if got := postgres.HolderMemberTokens(nil, 0, ranges); len(got) != 0 {
		t.Fatalf("no lookup ids gave tokens %v, want none", got)
	}
}

// keyRangeTokenArm returns the token arm predicate the shipped statement must
// carry, with its parameter.
func keyRangeTokenArm(param string) string {
	return postgres.SupplyChainImpactForeignMemberTokensFunction + "(finding.fact_id, finding.payload) && " + param + "::text[]"
}

// TestHolderKeyRangeStatementsCarryTheTokenArm derives the arm structure
// from the shipped statement constants: three arms each (cve_id, advisory_id,
// foreign-member token), the token arm on the next free parameter, with no
// fact_id bound of its own (the bucket, not the key range, partitions it).
func TestHolderKeyRangeStatementsCarryTheTokenArm(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, query, param string
	}{
		{"bounded", postgres.FindSupplyChainImpactHolderKeyRangeQuery, "$5"},
		{"open", postgres.FindSupplyChainImpactHolderOpenKeyRangeQuery, "$4"},
	} {
		arms := strings.Split(tc.query, "FROM fact_records AS finding")
		if len(arms) != 4 {
			t.Fatalf("%s statement has %d arms, want 3 (cve_id, advisory_id, token):\n%s", tc.name, len(arms)-1, tc.query)
		}
		token := arms[3]
		if !strings.Contains(token, keyRangeTokenArm(tc.param)) {
			t.Fatalf("%s statement token arm lacks %q:\n%s", tc.name, keyRangeTokenArm(tc.param), token)
		}
		if strings.Contains(token, "finding.fact_id >=") || strings.Contains(token, "finding.fact_id <") {
			t.Fatalf("%s statement token arm carries a fact_id bound:\n%s", tc.name, token)
		}
		for _, arm := range arms[1:3] {
			if strings.Contains(arm, postgres.SupplyChainImpactForeignMemberTokensFunction) {
				t.Fatalf("%s statement: a btree arm reads the token function:\n%s", tc.name, arm)
			}
		}
		if strings.Contains(tc.query, "advisory_group_members") {
			t.Fatalf("%s statement still reads the lossy members expression:\n%s", tc.name, tc.query)
		}
	}
}

// TestHolderLookupExceedsTokenBytesCountsBytes is the reader half of arbiter
// ruling 10643m: a lookup set falls back to the shipped statement exactly when
// one of its ids is over HolderMemberTokenMaxBytes bytes, the bound the token
// function applies with octet_length. Bytes, not characters: 512 two-byte
// characters fit, 513 do not, and 256 four-byte characters fit.
func TestHolderLookupExceedsTokenBytesCountsBytes(t *testing.T) {
	t.Parallel()

	ascii := strings.Repeat("a", postgres.HolderMemberTokenMaxBytes)
	for _, tc := range []struct {
		name string
		ids  []string
		want bool
	}{
		{"none", nil, false},
		{"short alias", []string{"GHSA-aaaa-bbbb-cccc"}, false},
		{"1,024 ASCII bytes", []string{ascii}, false},
		{"1,025 ASCII bytes", []string{ascii + "a"}, true},
		{"512 two-byte characters (1,024 bytes)", []string{strings.Repeat("é", 512)}, false},
		{"513 two-byte characters (1,026 bytes)", []string{strings.Repeat("é", 513)}, true},
		{"256 four-byte characters (1,024 bytes)", []string{strings.Repeat("𝄞", 256)}, false},
		{"1,024 two-byte characters (2,048 bytes)", []string{strings.Repeat("é", 1024)}, true},
		{"mixed: one long id among short ones", []string{"CVE-2026-1", ascii + "a", "GHSA-x"}, true},
	} {
		if got := postgres.HolderLookupExceedsTokenBytes(tc.ids); got != tc.want {
			t.Fatalf("%s: HolderLookupExceedsTokenBytes = %t, want %t", tc.name, got, tc.want)
		}
	}
}
