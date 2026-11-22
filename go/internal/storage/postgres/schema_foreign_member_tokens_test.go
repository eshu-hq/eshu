// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
)

// foreignMemberTokensBodySHA256 pins the body of the shipped token function
// (#10643 PR 2, function v4; docs/internal/evidence/10643-foreign-member-token-index.md). The
// body is never altered after release: a change is a new versioned function,
// a new index, and a new migration, proven again with the F1 shape matrix and
// a whole-fixture identity scan. Do not update this digest to make a test
// pass.
const foreignMemberTokensBodySHA256 = "f27cf0276991ca3faf9ab96a436dd3699e02427a064ffe3d989f0a1d0216bb90"

// foreignMemberTokensBody returns the text between the function's dollar
// quotes, exactly what PostgreSQL stores in pg_proc.prosrc.
func foreignMemberTokensBody(t *testing.T, ddl string) string {
	t.Helper()
	start := strings.Index(ddl, "AS $$")
	end := strings.LastIndex(ddl, "$$;")
	if start < 0 || end <= start {
		t.Fatalf("token function DDL has no $$ body:\n%s", ddl)
	}
	return ddl[start+len("AS $$") : end]
}

// migrationSQLByName returns the embedded migration named name, or fails.
func migrationSQLByName(t *testing.T, name string) (path, sql string) {
	t.Helper()
	for _, def := range BootstrapDefinitions() {
		if def.Name == name {
			return def.Path, def.SQL
		}
	}
	t.Fatalf("no embedded migration named %q", name)
	return "", ""
}

// TestForeignMemberTokensFunctionIsTheFrozenV4 pins the function's name,
// signature, attributes and body digest.
func TestForeignMemberTokensFunctionIsTheFrozenV4(t *testing.T) {
	t.Parallel()

	ddl := supplyChainImpactForeignMemberTokensFunctionSQL
	header := "CREATE OR REPLACE FUNCTION " + SupplyChainImpactForeignMemberTokensFunction +
		"(p_fact_id text, p_payload jsonb) RETURNS text[]\nLANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$"
	if !strings.HasPrefix(strings.TrimSpace(ddl), header) {
		t.Fatalf("token function DDL does not open with %q:\n%s", header, ddl)
	}
	// The Go lookup tokens (HolderMemberTokens) bucket by the same alphabet.
	if want := "strpos('" + holderMemberTokenAlphabet + "', right(p_fact_id, 1))"; strings.Count(ddl, want) != 3 {
		t.Fatalf("token function DDL must bucket with %q in each of its 3 branches", want)
	}
	// Arbiter ruling 10643m: every branch tokenizes a string only up to
	// HolderMemberTokenMaxBytes bytes, so no stored value can make a GIN
	// entry exceed its 2,712-byte limit and fail the finding insert.
	limit := strconv.Itoa(HolderMemberTokenMaxBytes)
	for _, guard := range []string{
		"AND octet_length(e #>> '{}') <= " + limit + "\n",
		"WHERE octet_length(k) <= " + limit + "\n",
		"CASE WHEN octet_length(z.s) <= " + limit + "\n",
	} {
		if strings.Count(ddl, guard) != 1 {
			t.Fatalf("token function DDL must carry the length guard %q once", guard)
		}
	}
	if strings.Count(ddl, "octet_length(") != 3 {
		t.Fatalf("token function DDL must hold exactly 3 octet_length guards, one per branch")
	}
	sum := sha256.Sum256([]byte(foreignMemberTokensBody(t, ddl)))
	if got := hex.EncodeToString(sum[:]); got != foreignMemberTokensBodySHA256 {
		t.Fatalf("token function body sha256 = %s, want the frozen v4 %s", got, foreignMemberTokensBodySHA256)
	}
}

// TestForeignMemberTokensMigrationsMatchTheGoDDL keeps the numbered
// migrations and the Go DDL in step: the function migration carries the Go
// function DDL byte for byte and sorts before the index migration, and the
// index migration is one concurrent build of exactly the Go DDL's index.
func TestForeignMemberTokensMigrationsMatchTheGoDDL(t *testing.T) {
	t.Parallel()

	fnPath, fnSQL := migrationSQLByName(t, SupplyChainImpactForeignMemberTokensFunction)
	idxPath, idxSQL := migrationSQLByName(t, SupplyChainImpactForeignMemberTokensIndex)
	if !strings.Contains(fnSQL, supplyChainImpactForeignMemberTokensFunctionSQL) {
		t.Fatalf("%s does not carry the Go token function DDL byte for byte", fnPath)
	}
	if !strings.Contains(factRecordSchemaSQL, supplyChainImpactForeignMemberTokensFunctionSQL) {
		t.Fatal("the fact_records Go DDL does not create the token function")
	}
	if fnPath >= idxPath {
		t.Fatalf("function migration %s must sort before index migration %s", fnPath, idxPath)
	}
	concurrent := "CREATE INDEX CONCURRENTLY IF NOT EXISTS " + SupplyChainImpactForeignMemberTokensIndex
	at := strings.Index(idxSQL, concurrent)
	if at < 0 {
		t.Fatalf("%s is not a concurrent build of %s:\n%s", idxPath, SupplyChainImpactForeignMemberTokensIndex, idxSQL)
	}
	stmt := idxSQL[at:]
	var code []string
	for _, line := range strings.Split(idxSQL, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			code = append(code, line)
		}
	}
	if strings.Count(strings.Join(code, "\n"), ";") != 1 || !strings.HasSuffix(strings.TrimSpace(stmt), ";") {
		t.Fatalf("%s must be one statement: a concurrent build cannot run in a transaction block", idxPath)
	}
	// A failed concurrent build leaves an INVALID index that IF NOT EXISTS
	// would skip forever; the schema apply path drops it by name first.
	if names := concurrentIndexNamesForInvalidCleanup(idxSQL); len(names) != 1 || names[0] != SupplyChainImpactForeignMemberTokensIndex {
		t.Fatalf("invalid-index cleanup names = %v, want [%s]", names, SupplyChainImpactForeignMemberTokensIndex)
	}
	plain := strings.Replace(strings.TrimSpace(stmt), "CONCURRENTLY ", "", 1)
	if !strings.Contains(factRecordSchemaSQL, plain) {
		t.Fatalf("the Go DDL index differs from %s; want it to contain:\n%s", idxPath, plain)
	}
	for _, want := range []string{
		"USING GIN (" + SupplyChainImpactForeignMemberTokensFunction + "(fact_id, payload))",
		"WHERE fact_kind = 'reducer_supply_chain_impact_finding'",
		"AND is_tombstone = FALSE;",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("token index lacks %q:\n%s", want, plain)
		}
	}
}
