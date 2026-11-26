// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	tokenAlias = "GHSA-tok-alias"
	tokenColon = "GHSA:tok:colon"
	tokenOwn   = "tok:own"
)

// tokenAliasHolders are the scopes the shipped read returns for tokenAlias:
// two bulk foreign holders spread over every bucket, own-key holders found by
// the btree arms, and one lone holder per members shape or edge the token
// arm must not lose.
var tokenAliasHolders = []string{
	"tok:bulk-a", "tok:bulk-b",
	"tok:own-cve",  // cve_id is the alias: the cve arm, never a token
	"tok:own-adv",  // advisory_id is the alias: the advisory arm
	"tok:object",   // members {"alias": 1}
	"tok:scalar",   // members "alias"
	"tok:null-cve", // no cve_id key
	"tok:null-adv", // no advisory_id key
	"tok:bucket0",  // fact id ends in a non-hex character: bucket 0
	"tok:upper",    // fact id ends in an upper-case hex digit: bucket 0
	"tok:mixed",    // members [5, {...}, null, "alias"]
}

// tokenColonHolders are the scopes the shipped read returns for tokenColon.
var tokenColonHolders = []string{"tok:colon", "tok:colon-object"}

// tokenSeed seeds the #10643 PR 2 differential. Non-holders: the asking
// scope, members missing, empty, JSON null, a number, a nested array, a
// token-prefix look-alike, a tombstone, and a superseded generation.
func tokenSeed(t *testing.T, ctx context.Context, sqldb *sql.DB) {
	t.Helper()
	kind := facts.ReducerSupplyChainImpactFindingFactKind
	scopes := append(append([]string{tokenOwn, "tok:missing", "tok:empty", "tok:jsonnull", "tok:number", "tok:nested", "tok:prefix", "tok:tomb"},
		tokenAliasHolders...), tokenColonHolders...)
	for _, scope := range scopes {
		fanOutSeedScope(t, ctx, sqldb, scope, "g1")
	}
	fanOutSeedScope(t, ctx, sqldb, "tok:superseded", "g2", "g1")
	insert := func(label, query string, args ...any) {
		t.Helper()
		if _, err := sqldb.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload) `+query, args...); err != nil {
			t.Fatalf("seed %s: %v", label, err)
		}
	}
	insert("bulk foreign holders", `
SELECT $1 || ':' || md5(s.scope_id || ':' || g), s.scope_id, s.active_generation_id, $1, md5(s.scope_id || ':' || g), '1.0.0',
  'synthetic', 'tok', now(), now(), false,
  jsonb_build_object('cve_id', 'CVE-tok-group', 'advisory_id', 'CVE-tok-group', 'impact_status', 'affected_exact',
    'provenance', jsonb_build_object('advisory_group_members', jsonb_build_array('CVE-tok-group', $2::text)))
FROM ingestion_scopes s CROSS JOIN generate_series(1, 150) g
WHERE s.scope_id IN ('tok:bulk-a', 'tok:bulk-b', 'tok:own')`, kind, tokenAlias)
	row := func(scope, factID, generation, payload string, tombstone bool) {
		t.Helper()
		insert(scope, `VALUES ($1, $2, $3, $4, $1, '1.0.0', 'synthetic', 'tok', now(), now(), $5, $6::jsonb)`,
			factID, scope, scope+"/"+generation, kind, tombstone, payload)
	}
	m := func(own, members string) string {
		return `{` + own + `"impact_status": "affected_exact", "provenance": {"advisory_group_members": ` + members + `}}`
	}
	keys := `"cve_id": "CVE-tok-x", "advisory_id": "GHSA-tok-x", `
	alias := strconv.Quote(tokenAlias)
	row("tok:own-cve", kind+":0c01", "g1", m(`"cve_id": `+alias+`, "advisory_id": "GHSA-tok-y", `, `[`+alias+`]`), false)
	row("tok:own-adv", kind+":0c02", "g1", m(`"cve_id": "CVE-tok-y", "advisory_id": `+alias+`, `, `[`+alias+`]`), false)
	row("tok:object", kind+":0c03", "g1", m(keys, `{`+alias+`: 1}`), false)
	row("tok:scalar", kind+":0c04", "g1", m(keys, alias), false)
	row("tok:null-cve", kind+":0c05", "g1", m(`"advisory_id": "GHSA-tok-x", `, `[`+alias+`]`), false)
	row("tok:null-adv", kind+":0c06", "g1", m(`"cve_id": "CVE-tok-x", `, `[`+alias+`]`), false)
	row("tok:bucket0", kind+":0c0Z", "g1", m(keys, `[`+alias+`]`), false)
	row("tok:upper", kind+":0c0F", "g1", m(keys, `[`+alias+`]`), false)
	row("tok:mixed", kind+":0c07", "g1", m(keys, `[5, {"x": 1}, null, `+alias+`]`), false)
	row("tok:colon", kind+":0c08", "g1", m(keys, `[`+strconv.Quote(tokenColon)+`]`), false)
	row("tok:colon-object", kind+":0c09", "g1", m(keys, `{`+strconv.Quote(tokenColon)+`: 1}`), false)
	row("tok:missing", kind+":0c0a", "g1", `{"cve_id": "CVE-tok-x", "impact_status": "affected_exact"}`, false)
	row("tok:empty", kind+":0c0b", "g1", m(keys, `[]`), false)
	row("tok:jsonnull", kind+":0c0c", "g1", m(keys, `null`), false)
	row("tok:number", kind+":0c0d", "g1", m(keys, `5`), false)
	row("tok:nested", kind+":0c0e", "g1", m(keys, `[[`+alias+`]]`), false)
	row("tok:prefix", kind+":0c0f", "g1", m(keys, `["2:`+tokenAlias+`"]`), false)
	row("tok:tomb", kind+":0c10", "g1", m(keys, `[`+alias+`]`), true)
	row("tok:superseded", kind+":0c11", "g1", m(keys, `[`+alias+`]`), false)
	if _, err := sqldb.ExecContext(ctx, `ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
}

// tokenStatement builds statement i of a ranged read: its text and arguments.
type tokenStatement func(i int, r supplychaincore.HolderKeyRange) (string, []any)

// shippedTokenStatement is the production statement and arguments: the
// shipped constants with HolderMemberTokens, exactly as the store binds them.
func shippedTokenStatement(ids []string, n int) tokenStatement {
	return func(i int, r supplychaincore.HolderKeyRange) (string, []any) {
		tokens := postgres.HolderMemberTokens(ids, i, n)
		if r.Upper == "" {
			return postgres.FindSupplyChainImpactHolderOpenKeyRangeQuery, []any{ids, tokenOwn, r.Lower, tokens}
		}
		return postgres.FindSupplyChainImpactHolderKeyRangeQuery, []any{ids, tokenOwn, r.Lower, r.Upper, tokens}
	}
}

// tokenRead runs the n statements of build on one read-only REPEATABLE READ
// snapshot under the store's plan mode and returns the sorted scope union.
func tokenRead(t *testing.T, ctx context.Context, sqldb *sql.DB, build tokenStatement) []string {
	t.Helper()
	tx, err := sqldb.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('plan_cache_mode', $1, true)`, postgres.HolderKeyRangePlanCacheMode); err != nil {
		t.Fatalf("plan mode: %v", err)
	}
	set := map[string]struct{}{}
	for i, r := range supplychaincore.HolderKeyRanges() {
		query, args := build(i, r)
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			t.Fatalf("statement %d: %v", i+1, err)
		}
		for rows.Next() {
			var scope string
			if err := rows.Scan(&scope); err != nil {
				t.Fatalf("scan: %v", err)
			}
			set[scope] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("statement %d rows: %v", i+1, err)
		}
		_ = rows.Close()
	}
	return slices.Sorted(func(yield func(string) bool) {
		for scope := range set {
			if !yield(scope) {
				return
			}
		}
	})
}

// TestHolderKeyRangeTokenReadMatchesShippedLive is the #10643 PR 2 live
// differential: for the alias and a colon-bearing id, the 16 three-arm
// statements on one snapshot return exactly the shipped holder read
// (FindHolderScopes), through the store and through the shipped constants.
// Three seeded REDs, each derived from the shipped statements, must lose a
// holder: dropping bucket 0 from statement 1, dropping the token arm, and
// swapping in the array-only function.
func TestHolderKeyRangeTokenReadMatchesShippedLive(t *testing.T) {
	ctx, sqldb := fanOutLiveDB(t)
	// The seed plants frozen finding facts, so it runs in a scoped
	// postgresproof.SeedFindingFacts window: the proof database stays armed
	// and the holder reads under test run armed.
	postgresproof.SeedFindingFacts(t, sqldb, func() {
		tokenSeed(t, ctx, sqldb)
	})
	if _, err := sqldb.ExecContext(ctx, keyRangeArrayOnlyFunctionSQL); err != nil {
		t.Fatalf("create array-only RED function: %v", err)
	}
	store := fanOutLiveStore(sqldb)
	n := len(supplychaincore.HolderKeyRanges())
	for _, tc := range []struct {
		id   string
		want []string
	}{{tokenAlias, tokenAliasHolders}, {tokenColon, tokenColonHolders}} {
		ids := []string{tc.id}
		want := slices.Sorted(slices.Values(tc.want))
		shipped, err := store.FindHolderScopes(ctx, tokenOwn, ids)
		if err != nil || !slices.Equal(shipped, want) {
			t.Fatalf("%s: shipped holders = %v, %v; want %v (the seed is wrong)", tc.id, shipped, err, want)
		}
		read, err := store.FindHolderScopesByKeyRange(ctx, tokenOwn, ids, supplychaincore.HolderKeyRanges())
		if err != nil || !slices.Equal(read.Scopes, shipped) {
			t.Fatalf("%s: key-range holders = %v, %v; want the shipped set %v", tc.id, read.Scopes, err, shipped)
		}
		if got := tokenRead(t, ctx, sqldb, shippedTokenStatement(ids, n)); !slices.Equal(got, shipped) {
			t.Fatalf("%s: shipped-constant read = %v, want %v", tc.id, got, shipped)
		}
	}

	ids := []string{tokenAlias}
	shipped := shippedTokenStatement(ids, n)
	lost := func(name string, got []string, want ...string) {
		t.Helper()
		for _, scope := range want {
			if slices.Contains(got, scope) {
				t.Fatalf("seeded RED %s still found %s; the differential cannot see it: %v", name, scope, got)
			}
		}
	}
	dropBucket0 := tokenRead(t, ctx, sqldb, func(i int, r supplychaincore.HolderKeyRange) (string, []any) {
		query, args := shipped(i, r)
		tokens := args[len(args)-1].([]string)
		args[len(args)-1] = slices.DeleteFunc(slices.Clone(tokens), func(s string) bool { return strings.HasPrefix(s, "0:") })
		return query, args
	})
	lost("drop bucket 0", dropBucket0, "tok:bucket0", "tok:upper")
	dropArm := tokenRead(t, ctx, sqldb, func(i int, r supplychaincore.HolderKeyRange) (string, []any) {
		query, args := shipped(i, r)
		cut := strings.LastIndex(query, "\n    UNION")
		end := cut + strings.Index(query[cut:], "\n)")
		return query[:cut] + query[end:], args[:len(args)-1]
	})
	lost("drop the token arm", dropArm, "tok:bulk-a", "tok:object", "tok:scalar", "tok:null-cve", "tok:bucket0")
	arrayOnly := tokenRead(t, ctx, sqldb, func(i int, r supplychaincore.HolderKeyRange) (string, []any) {
		query, args := shipped(i, r)
		return strings.ReplaceAll(query, postgres.SupplyChainImpactForeignMemberTokensFunction, keyRangeArrayOnlyFunction), args
	})
	lost("array-only function", arrayOnly, "tok:object", "tok:scalar")
	if !slices.Contains(arrayOnly, "tok:bulk-a") {
		t.Fatalf("array-only RED lost the array holders too; it no longer isolates the object and scalar shapes: %v", arrayOnly)
	}
}
