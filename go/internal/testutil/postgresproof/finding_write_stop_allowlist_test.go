// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The shrink-only allowlist of every caller of the finding write stop's disarm forms
// (#10880 PR-C2 k, arbiter-c2-n-armed section 4). Proof databases start armed. A caller
// that disables the stop, scoped (SeedFindingFacts) or for a whole test
// (DisarmFindingWriteStopForTest), or through the raw primitives, is named here with a
// one-line reason, so a forgotten disarm cannot spread: an unlisted caller fails, and an
// entry whose file no longer calls any form fails too, so the list only shrinks.
//
// The reason says which of two classes the file is in: it plants frozen finding facts around
// code that runs armed (scoped), or it models a schema from before C2 and may run disarmed
// for the whole test.
var findingWriteStopCallers = map[string]string{
	"internal/storage/postgres/supply_chain_impact_fact_write_stop_live_test.go":         "tests the stop itself: seeds one frozen row scoped, and disarms a whole subtest to prove a re-apply keeps a disabled trigger disabled",
	"internal/storage/postgres/supply_chain_impact_pre_c2_live_helpers_test.go":          "openPreC2ReplaceSetLiveDB: whole-test disarm for the proofs that model a pre-C2 schema (old-writer fence, backfill, drain, verify, key fill, differentials against the frozen fact statements)",
	"internal/storage/postgres/supply_chain_impact_pre_c2_isolated_helpers_test.go":      "openPreC2IsolatedLiveDB: the same whole-test disarm for the pre-C2 proofs that open an isolated schema (drain and reader equality against the frozen fact reads)",
	"internal/storage/postgres/supply_chain_impact_winners_isolated_schema_live_test.go": "the isolated-schema capture proofs plant finding facts with raw SQL (whole-test disarm; to become SeedFindingFacts)",
	"internal/storage/postgres/supply_chain_impact_advisory_group_winners_live_test.go":  "the advisory-group fixtures plant finding facts with raw SQL (whole-test disarm; to become SeedFindingFacts)",
	"internal/storage/postgres/supply_chain_impact_capped_scope_live_test.go":            "openCappedScopeLiveDB: the capped-scope proofs plant finding facts with raw SQL (whole-test disarm; to become SeedFindingFacts)",
	"internal/storage/postgres/supply_chain_impact_fanout_live_test.go":                  "fanOutLiveDB: the fan-out family plants finding facts with raw SQL across ~16 files; whole-test disarm for now, each plant to become a SeedFindingFacts call (the holder reads under test read supports)",
	"internal/storage/postgres/supply_chain_impact_winners_write_on_change_live_test.go": "winnersLiveExec, the raw-SQL seed of the winners and audit family: each raw write of a finding fact is a scoped plant, the writer and maintainer run armed",
}

var disarmCall = regexp.MustCompile(`\b(SeedFindingFacts|DisarmFindingWriteStopForTest|DisarmFindingWriteStop|ArmFindingWriteStop)\(`)

// findingWriteStopCallerFiles returns the root-relative Go files under root that call a
// disarm form, excluding this package's own implementation and its tests. Comments are
// removed first, so a mention in prose is not a call.
func findingWriteStopCallerFiles(root string) ([]string, error) {
	lineComment := regexp.MustCompile(`(?m)//.*$`)
	blockComment := regexp.MustCompile(`(?s)/\*.*?\*/`)
	var files []string
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
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "internal/testutil/postgresproof/finding_write_stop") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		code := blockComment.ReplaceAllString(string(raw), "")
		code = lineComment.ReplaceAllString(code, "")
		if disarmCall.MatchString(code) {
			files = append(files, rel)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

// TestFindingWriteStopCallersAreAllowlisted is the GREEN half: every file that disarms the
// stop is listed with a reason, and every listed file still disarms it.
func TestFindingWriteStopCallersAreAllowlisted(t *testing.T) {
	t.Parallel()

	files, err := findingWriteStopCallerFiles(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("scan the Go tree: %v", err)
	}
	found := make(map[string]bool, len(files))
	for _, file := range files {
		found[file] = true
		reason, listed := findingWriteStopCallers[file]
		switch {
		case !listed:
			t.Errorf("%s disarms the finding write stop and is not in the allowlist: seed with SeedFindingFacts around the plant and add the file with a one-line reason", file)
		case strings.TrimSpace(reason) == "":
			t.Errorf("%s is in the allowlist with an empty reason", file)
		}
	}
	for file := range findingWriteStopCallers {
		if !found[file] {
			t.Errorf("allowlist entry %s no longer disarms the finding write stop: delete it, the list is shrink-only", file)
		}
	}
}

// TestFindingWriteStopCallerScanCatchesPlantedCallers is the RED half: each planted file that
// calls a disarm form is reported, and look-alikes (a comment, a test-name suffix, the helper's
// own file) are not.
func TestFindingWriteStopCallerScanCatchesPlantedCallers(t *testing.T) {
	t.Parallel()

	files := map[string]struct {
		body string
		want bool
	}{
		"scoped.go":         {"package x\nfunc f() { postgresproof.SeedFindingFacts(t, db, func() {}) }\n", true},
		"whole_test.go":     {"package x\nfunc f() { postgresproof.DisarmFindingWriteStopForTest(t, db) }\n", true},
		"raw_disarm.go":     {"package x\nfunc f() { _ = postgresproof.DisarmFindingWriteStop(ctx, db) }\n", true},
		"raw_arm.go":        {"package x\nfunc f() { _ = postgresproof.ArmFindingWriteStop(ctx, db) }\n", true},
		"comment_only.go":   {"package x\n// calls SeedFindingFacts(t, db, f) in prose\n/* DisarmFindingWriteStop( */\n", false},
		"other_function.go": {"package x\nfunc f() { postgresproof.FindingWriteStopArmed(ctx, db) }\n", false},
		"similar_name.go":   {"package x\nfunc f() { MySeedFindingFactsHelper(t) }\n", false},
	}
	root := t.TempDir()
	for name, file := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(file.body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	found, err := findingWriteStopCallerFiles(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	got := make(map[string]bool, len(found))
	for _, f := range found {
		got[f] = true
	}
	for name, file := range files {
		if got[name] != file.want {
			t.Errorf("%s reported = %v, want %v", name, got[name], file.want)
		}
	}
}
