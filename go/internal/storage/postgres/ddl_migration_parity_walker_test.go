// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDDLMigrationParityWalkerReadsEveryKind drives the real-tree path end to
// end on a synthetic module: goStringLiteralSources walks the scan roots,
// extractDDLInventory reads the literals, and ddlParityGaps compares them
// with an empty migration inventory. A Go file that holds only one statement
// of a compared kind must be read, whatever other words it holds. The
// walker's exclusions (_test.go files, testdata trees, the migrations
// directory, and paths outside the roots) must stay unread.
func TestDDLMigrationParityWalkerReadsEveryKind(t *testing.T) {
	t.Parallel()

	goRoot := t.TempDir()
	files := map[string]string{
		"cmd/seqonly/store.go":                         "CREATE SEQUENCE IF NOT EXISTS walker_seq",
		"cmd/seqonly/joined.go":                        `CREATE " + "SEQUENCE walker_joined_seq`,
		"cmd/viewonly/store.go":                        "CREATE OR REPLACE VIEW walker_view AS SELECT 1",
		"cmd/matviewonly/store.go":                     "CREATE MATERIALIZED VIEW walker_matview AS SELECT 1",
		"cmd/proconly/store.go":                        "CREATE PROCEDURE walker_proc() LANGUAGE sql AS $$ SELECT 1 $$",
		"cmd/typeonly/store.go":                        "CREATE TYPE walker_type AS ENUM ('a')",
		"cmd/domainonly/store.go":                      "CREATE DOMAIN walker_domain AS TEXT",
		"internal/storage/postgres/altertype/store.go": "ALTER TYPE walker_type ADD VALUE 'b'",
		// Exclusions: none of these may be reported.
		"cmd/seqonly/store_test.go":                      "CREATE SEQUENCE excluded_test_seq",
		"cmd/seqonly/testdata/fixture.go":                "CREATE SEQUENCE excluded_testdata_seq",
		"internal/storage/postgres/migrations/embed.go":  "CREATE SEQUENCE excluded_migrations_seq",
		"internal/elsewhere/store.go":                    "CREATE SEQUENCE excluded_outside_roots_seq",
		"cmd/seqonly/notgo.txt":                          "CREATE SEQUENCE excluded_not_go_seq",
		"internal/storage/postgres/altertype/comment.go": "",
	}
	for rel, stmt := range files {
		path := filepath.Join(goRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		body := "package walker\n\nconst walkerSQL = \"" + stmt + "\"\n"
		if stmt == "" {
			// DDL only in a Go comment never counts.
			body = "package walker\n\n// CREATE SEQUENCE excluded_comment_seq\nvar _ = 1\n"
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	sources, err := goStringLiteralSources(goRoot, []string{"internal/storage/postgres", "cmd"})
	if err != nil {
		t.Fatalf("goStringLiteralSources: %v", err)
	}
	gaps := ddlParityGaps(extractDDLInventory(sources), extractDDLInventory(nil), nil, nil)
	want := []string{
		"cmd/domainonly/store.go:3: domain walker_domain",
		"cmd/matviewonly/store.go:3: materialized view walker_matview",
		"cmd/proconly/store.go:3: procedure walker_proc",
		"cmd/seqonly/joined.go:3: sequence walker_joined_seq",
		"cmd/seqonly/store.go:3: sequence walker_seq",
		"cmd/typeonly/store.go:3: type walker_type",
		"cmd/viewonly/store.go:3: view walker_view",
		`internal/storage/postgres/altertype/store.go:3: unresolved DDL in "ALTER TYPE walker_type ADD VALUE 'b'"`,
	}
	if len(gaps) != len(want) {
		t.Fatalf("gaps = %d %q, want %d", len(gaps), gaps, len(want))
	}
	for i := range want {
		if !strings.HasPrefix(gaps[i], want[i]) {
			t.Errorf("gap[%d] = %q, want prefix %q", i, gaps[i], want[i])
		}
	}
	for _, gap := range gaps {
		if strings.Contains(gap, "excluded_") {
			t.Errorf("an excluded file was read: %q", gap)
		}
	}
}
