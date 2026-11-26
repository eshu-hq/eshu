// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The write-path census of the finding-fact write stop (#10880 PR-C2 k). The reject
// trigger refuses every INSERT or UPDATE of a reducer_supply_chain_impact_finding row
// in fact_records, so no non-test Go file may be able to write that kind there. A file
// is a write site when it both names the kind and carries a fact_records write form: a
// literal INSERT or UPDATE, a COPY into the table, or a call into the generic fact
// writers. The allowlist is empty and shrink-only: a new write site fails the census
// instead of being waived, and an entry that no longer matches fails too, so the list
// can only get shorter. Readers of the kind (the backfill, drain, verification, the
// legacy read paths PR-C2 m removes, the audit) are not write sites and are not listed.

// findingWriteSiteAllowlist is the set of files allowed to write the kind into
// fact_records. It is empty.
var findingWriteSiteAllowlist = map[string]string{}

var (
	findingKindReference = regexp.MustCompile(`reducer_supply_chain_impact_finding|ReducerSupplyChainImpactFindingFactKind|supplyChainImpactFactKind|winnersSeedFactKind`)
	factRecordsWriteForm = regexp.MustCompile(`INSERT\s+INTO\s+fact_records|UPDATE\s+fact_records|pgx\.Identifier\{"fact_records"\}|factwrite\.(BatchInsertVersionedFacts|BatchInsertFacts|SingleInsertQuery|BatchInsertVersionedQuery|BatchInsertQuery)\b`)
	goLineComment        = regexp.MustCompile(`(?m)//.*$`)
	goBlockComment       = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

// findingWriteSites returns the root-relative paths of the non-test Go files under root
// that name the finding kind and carry a fact_records write form, comments removed.
func findingWriteSites(root string) ([]string, error) {
	var sites []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "vendor", "testdata", "node_modules", ".git":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		code := goBlockComment.ReplaceAllString(string(raw), "")
		code = goLineComment.ReplaceAllString(code, "")
		if findingKindReference.MatchString(code) && factRecordsWriteForm.MatchString(code) {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			sites = append(sites, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(sites)
	return sites, err
}

// TestNoGoFileWritesTheFindingKindToFactRecords is the GREEN half: the production tree
// has no write site of the kind outside the (empty) allowlist, and no allowlist entry
// outlives its site.
func TestNoGoFileWritesTheFindingKindToFactRecords(t *testing.T) {
	t.Parallel()

	sites, err := findingWriteSites(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("scan the Go tree: %v", err)
	}
	found := make(map[string]bool, len(sites))
	for _, site := range sites {
		found[site] = true
		if _, allowed := findingWriteSiteAllowlist[site]; !allowed {
			t.Errorf("%s names the finding kind and writes fact_records: the reject trigger (migration 358) refuses that write, so the file must write the supports table instead (WriteSupplyChainImpactFindingRows)", site)
		}
	}
	for site, reason := range findingWriteSiteAllowlist {
		if !found[site] {
			t.Errorf("allowlist entry %s (%s) matches no write site: delete it, the list is shrink-only", site, reason)
		}
	}
}

// TestFindingWriteCensusCatchesPlantedViolations is the RED half: each planted file
// that writes the kind is reported, and each look-alike that must not be reported is
// not, so the census can neither miss a write site nor flag a reader or a comment.
func TestFindingWriteCensusCatchesPlantedViolations(t *testing.T) {
	t.Parallel()

	files := map[string]struct {
		body string
		want bool
	}{
		"copy_seed.go":      {"package x\nvar _ = pgx.Identifier{\"fact_records\"}\nconst k = \"reducer_supply_chain_impact_finding\"\n", true},
		"insert_literal.go": {"package x\nconst q = `INSERT INTO fact_records (a) VALUES (1)`\nconst k = ReducerSupplyChainImpactFindingFactKind\n", true},
		"update_literal.go": {"package x\nconst q = `UPDATE fact_records SET a = 1`\nvar _ = supplyChainImpactFactKind\n", true},
		"factwrite_call.go": {"package x\nfunc f() { factwrite.BatchInsertVersionedFacts(nil, nil, nil) }\nconst k = winnersSeedFactKind\n", true},
		"reader_only.go":    {"package x\nconst q = `SELECT 1 FROM fact_records WHERE fact_kind = 'reducer_supply_chain_impact_finding'`\n", false},
		"other_kind.go":     {"package x\nconst q = `INSERT INTO fact_records (a) VALUES (1)`\nconst k = \"reducer_container_image_identity\"\n", false},
		"comment_only.go":   {"package x\n// INSERT INTO fact_records of reducer_supply_chain_impact_finding is refused\n/* UPDATE fact_records */\nconst k = \"other\"\n", false},
		"copy_seed_test.go": {"package x\nvar _ = pgx.Identifier{\"fact_records\"}\nconst k = \"reducer_supply_chain_impact_finding\"\n", false},
	}
	root := t.TempDir()
	for name, file := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(file.body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	sites, err := findingWriteSites(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	got := make(map[string]bool, len(sites))
	for _, site := range sites {
		got[site] = true
	}
	for name, file := range files {
		if got[name] != file.want {
			t.Errorf("%s reported = %v, want %v", name, got[name], file.want)
		}
	}
}
