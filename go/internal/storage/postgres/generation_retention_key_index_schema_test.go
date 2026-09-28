// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestBootstrapDefinitionsIncludeGenerationRetentionKeyIndexes pins the two
// partial expression indexes the retention prunes probe for retained holders
// (#7279). Each must be its own single-statement concurrent migration so the
// migration coordinator builds it outside a transaction, and each predicate must
// repeat the prunes' literal fact_kind and is_tombstone filters so a generic
// plan can still prove the partial predicate.
func TestBootstrapDefinitionsIncludeGenerationRetentionKeyIndexes(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"fact_records_content_entity_key_idx": `CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_content_entity_key_idx
    ON fact_records ((payload->>'repo_id'), (payload->>'entity_id'))
    WHERE fact_kind = 'content_entity'
      AND is_tombstone = FALSE;`,
		"fact_records_file_key_idx": `CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_file_key_idx
    ON fact_records ((payload->>'repo_id'), (payload->>'relative_path'))
    WHERE fact_kind = 'file'
      AND is_tombstone = FALSE;`,
	}
	found := map[string]bool{}
	for _, definition := range BootstrapDefinitions() {
		statement, ok := want[definition.Name]
		if !ok {
			continue
		}
		found[definition.Name] = true
		if !strings.Contains(definition.SQL, statement) {
			t.Errorf("%s does not match the retention key probe:\n%s", definition.Name, definition.SQL)
		}
		if n := strings.Count(definition.SQL, ";"); n != 1 {
			t.Errorf("%s holds %d statements, want exactly one concurrent build", definition.Name, n)
		}
	}
	for name := range want {
		if !found[name] {
			t.Errorf("retention key index migration %s is missing", name)
		}
	}
}

// TestBootstrapDefinitionsKeepRetentionKeyIndexesOnColdBootstrap pins that the
// deferred cold-bootstrap layout still builds the retention key indexes: the
// retention store refuses a cycle until both are valid, and nothing rebuilds a
// deferred fact_records index later.
func TestBootstrapDefinitionsKeepRetentionKeyIndexesOnColdBootstrap(t *testing.T) {
	t.Parallel()

	seen := 0
	for _, definition := range BootstrapDefinitionsWithoutContentSearchIndexes() {
		switch definition.Name {
		case "fact_records_content_entity_key_idx", "fact_records_file_key_idx":
			seen++
			if definition.Variant != "" || !strings.Contains(definition.SQL, "CREATE INDEX CONCURRENTLY") {
				t.Errorf("%s is deferred on cold bootstrap (variant %q):\n%s", definition.Name, definition.Variant, definition.SQL)
			}
		}
	}
	if seen != 2 {
		t.Errorf("cold bootstrap layout carries %d retention key index migrations, want 2", seen)
	}
}
