// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strconv"
	"strings"
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
// -- see README.md and checksum_alias.go. #7007 then added
// 121_fact_records_content_entity_dependency_variable_repo_idx.sql; #7126 added
// 122_fact_records_documentation_source_only_idx.sql and
// 123_fact_records_story_support_kinds_idx.sql and
// 124_fact_records_documentation_semantic_target_refs_idx.sql, #6679 added
// 125_shared_projection_acceptance_generation_key.sql, and #6475 added
// 126-129 (the service materialization lineage scope column, its backfill,
// and the rescoped active-service index); #7115 added
// 130_projector_scope_claim_fences.sql; #7237 added
// 131_fact_records_semantic_code_hint_order_idx.sql and
// 132_content_entities_repo_path_start_idx.sql; #7242 added
// 133_repository_entry_points_index.sql; #7033 added the path GIN and
// lifecycle validator as migrations 134-135; #7127 added
// 136_changed_since_link_ledger.sql; #7248 added
// 137_content_files_repo_path_pattern_idx.sql; #7125 adds
// 138_content_file_secret_lines.sql; #7088 adds package-consumption identity
// and readiness indexes in migrations 139-144; #7242 adds
// 145_fact_records_workload_names_scope_idx.sql; #7279 adds the retention key
// indexes as 146_fact_records_content_entity_key_idx.sql and
// 147_fact_records_file_key_idx.sql; #7319 adds
// 148_scope_generations_delta_baseline_commit_sha.sql.
const goldenBootstrapDefinitionsDigest = "14b1098c8e8bcc3fab79695b4e761a2afbc7e9e0550c3308d6b0c4735b85c28c"

// goldenBootstrapDefinitionsCount pins the definition count alongside the
// digest so a truncated embed pattern (e.g. matching embed.go itself, or
// silently dropping files) fails loudly even in the unlikely case of a hash
// collision.
const goldenBootstrapDefinitionsCount = 169

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

// TestBootstrapDefinitionsHaveUniqueSequenceNumbers keeps recent migrations
// from claiming the same ordering slot after concurrent PRs merge. Older
// migrations intentionally reuse several slots, so the guard starts at 100.
func TestBootstrapDefinitionsHaveUniqueSequenceNumbers(t *testing.T) {
	seen := make(map[int]string)
	for _, def := range BootstrapDefinitions() {
		filename := path.Base(def.Path)
		parts := strings.SplitN(filename, "_", 2)
		if len(parts) != 2 {
			t.Fatalf("migration %q has no sequence prefix", filename)
		}
		sequence, err := strconv.Atoi(parts[0])
		if err != nil || sequence < 100 {
			continue
		}
		if previous, exists := seen[sequence]; exists {
			t.Fatalf("migration sequence %d is shared by %q and %q", sequence, previous, filename)
		}
		seen[sequence] = filename
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
