// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
)

// goldenBootstrapDefinitionsDigest pins BootstrapDefinitions(). It is sha256
// over "Name|Path|sha256(SQL)\n" per definition, in BootstrapDefinitions()
// order. The migration tracker keys applied migrations by path + variant +
// checksum_sha256 (schema_bootstrap_lock.go), so any drift here means an
// existing deployment's bootstrap would try to re-apply or diverge on
// already-applied migrations. A change to this constant must be justified by
// an intentional migration edit, never by a refactor alone.
//
// #7002: this value was last updated to 093_cross_scope_completion_queue.sql
// being restored to its originally shipped bytes (checksum
// c95cae2762bd4d0d42da4720eb0ad5545d2d032914bded15a65ab01acb92ce42). #6785
// and #6923 had edited that file in place instead of shipping a new guarded
// migration, which is exactly the mistake this golden digest exists to catch
// -- see README.md and checksum_alias.go.
const goldenBootstrapDefinitionsDigest = "c430b2cb8dba521c13024307f84a8952885f752eac9f5bc96a29c35278c98740"

// goldenBootstrapDefinitionsCount pins the definition count alongside the
// digest so a truncated embed pattern (e.g. matching embed.go itself, or
// silently dropping files) fails loudly even in the unlikely case of a hash
// collision.
const goldenBootstrapDefinitionsCount = 141

func TestBootstrapDefinitionsMatchesPreRefactorGolden(t *testing.T) {
	defs := BootstrapDefinitions()
	if len(defs) != goldenBootstrapDefinitionsCount {
		t.Fatalf("BootstrapDefinitions() count = %d, want %d", len(defs), goldenBootstrapDefinitionsCount)
	}
	h := sha256.New()
	for _, def := range defs {
		sqlSum := sha256.Sum256([]byte(def.SQL))
		fmt.Fprintf(h, "%s|%s|%s\n", def.Name, def.Path, hex.EncodeToString(sqlSum[:]))
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != goldenBootstrapDefinitionsDigest {
		t.Fatalf("BootstrapDefinitions() digest = %s, want %s (Name/Path/SQL/order must stay byte-identical)",
			got, goldenBootstrapDefinitionsDigest)
	}
}

// TestBootstrapDefinitionsExcludesNonSQLFiles guards the embed pattern: only
// *.sql files may become definitions. embed.go itself, doc.go, README.md and
// AGENTS.md must never appear.
func TestBootstrapDefinitionsExcludesNonSQLFiles(t *testing.T) {
	for _, def := range BootstrapDefinitions() {
		if def.Name == "" {
			t.Fatalf("definition with empty name for path %q", def.Path)
		}
	}
	for _, name := range []string{"embed", "doc", "README", "AGENTS"} {
		for _, def := range BootstrapDefinitions() {
			if def.Name == name {
				t.Fatalf("non-SQL file %q leaked into BootstrapDefinitions()", name)
			}
		}
	}
}
