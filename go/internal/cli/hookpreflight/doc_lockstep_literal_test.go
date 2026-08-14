// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Occurrence-counting half of the doc-literal checks (see doc_lockstep_test.go
// for the conventions).
//
// docsMissingLiteral answers "does this file contain this string at all", which
// is the right question for a literal that appears once and the wrong one for a
// literal that appears several times. The schema name is quoted seven times
// across the three package docs; rewriting any single occurrence left the whole
// suite green, because the other six still matched. The branch had already hit
// this with eshu_hook_timeout and fixed it by pinning both surrounding
// sentences, and then did not generalize the fix.
//
// Counting is the general form. A doc edit that adds or removes a mention has
// to move the number here, which is the same "change both or neither" the rest
// of the guard is built on.

// docLiteralOccurrences counts how many times literal appears in dir/name.
func docLiteralOccurrences(dir, name, literal string) (int, error) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return 0, err
	}
	return strings.Count(string(data), literal), nil
}

// schemaLiteral is the contract name Evaluate stamps into Output.Schema.
const schemaLiteral = "assistant_fast_path_hook.v1"

// docSchemaLiteralCounts pins how many times each package doc quotes
// schemaLiteral. The counts are the assertion, not a floor: rewriting one of
// README.md's four mentions to a different contract name has to show up
// somewhere, and a bare "the file mentions it" check is exactly where it did
// not.
func docSchemaLiteralCounts() map[string]int {
	return map[string]int{
		"README.md": 4,
		"doc.go":    1,
		"AGENTS.md": 2,
	}
}

// TestDocLockstepSchemaLiteralOccurrenceCounts holds every mention of the
// contract name in the package docs, not just the first.
func TestDocLockstepSchemaLiteralOccurrenceCounts(t *testing.T) {
	t.Parallel()

	pinned := docSchemaLiteralCounts()
	if len(pinned) != 3 {
		t.Fatalf("pinned docs = %d, want the three package docs", len(pinned))
	}

	total := 0
	for name, want := range pinned {
		got, err := docLiteralOccurrences(".", name, schemaLiteral)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if got != want {
			t.Errorf("%s mentions %s %d times, pinned at %d; a rewritten mention is a contract rename in one place and not the other", name, schemaLiteral, got, want)
		}
		total += got
	}
	if total == 0 {
		t.Fatal("counted 0 mentions across the package docs; the assertion would be vacuous")
	}
}

// TestDocLockstepREADMENamesEveryLockstepFile pins the sentence the rest of
// this guard is read through: README.md says its per-file list "is the boundary
// -- a claim not in it is not pinned". That is only true while the list is
// complete, and nothing held it. A new doc_lockstep file could land unlisted,
// or a listed one could be deleted, and the boundary sentence would go on
// claiming a coverage map that no longer matches the directory.
func TestDocLockstepREADMENamesEveryLockstepFile(t *testing.T) {
	t.Parallel()

	// The glob is deliberately "doc_lockstep*" and not "doc_lockstep_*":
	// doc_lockstep_test.go, the file the rest of the guard is documented from,
	// does not match the second one, and a pin that silently skips the main
	// file is the failure mode this whole directory exists to stop.
	entries, err := filepath.Glob("doc_lockstep*_test.go")
	if err != nil {
		t.Fatalf("list lockstep files: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("found %d lockstep files; the assertion would be vacuous", len(entries))
	}
	if !slices.Contains(entries, "doc_lockstep_test.go") {
		t.Fatalf("the lockstep file list %v does not include doc_lockstep_test.go; the glob stopped matching the file the guard is documented from", entries)
	}
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	for _, name := range entries {
		if !strings.Contains(string(readme), name) {
			t.Errorf("README.md's lockstep list does not name %s; the page calls that list the boundary of what is pinned, so a file missing from it makes the boundary sentence false", name)
		}
	}
	if want := strings.Count(string(readme), "doc_lockstep_"); want < len(entries) {
		t.Errorf("README.md mentions doc_lockstep_ %d times for %d files; a listed file was dropped from the directory", want, len(entries))
	}

	// The numeral in the same sentence. An unlisted new file fails the loop
	// above, but a listed one does not, and the sentence goes on quoting the
	// old count -- which is the boundary claim itself, stated as a number.
	if len(entries) >= len(lockstepCountWords) {
		t.Fatalf("%d lockstep files, and lockstepCountWords spells only %d; extend it", len(entries), len(lockstepCountWords))
	}
	want := lockstepCountWords[len(entries)] + " lockstep"
	if !strings.Contains(string(readme), want) {
		t.Errorf("README.md does not say %q for the %d lockstep files on disk; the sentence that calls the list a boundary quotes the count, so the count has to move with the directory", want, len(entries))
	}
	for count, word := range lockstepCountWords {
		if count == len(entries) {
			continue
		}
		if stale := word + " lockstep"; strings.Contains(string(readme), stale) {
			t.Errorf("README.md still says %q while %d lockstep files are on disk", stale, len(entries))
		}
	}
}

// lockstepCountWords spells the counts README.md's boundary sentence can carry,
// index by index, so the assertion above can name the word the directory
// implies rather than checking that some number is present.
var lockstepCountWords = []string{
	"Zero", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight",
	"Nine", "Ten", "Eleven", "Twelve", "Thirteen", "Fourteen", "Fifteen",
	"Sixteen", "Seventeen", "Eighteen", "Nineteen", "Twenty",
}

// TestDocLockstepOccurrenceCounterReportsARewrittenMention is the negative
// half: it proves the counter distinguishes a rewritten mention from an intact
// file, which a Contains check cannot.
func TestDocLockstepOccurrenceCounterReportsARewrittenMention(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	intact := "the " + schemaLiteral + " contract\nand again: " + schemaLiteral + "\n"
	rewritten := "the " + schemaLiteral + " contract\nand again: assistant_fast_path_hook.v2\n"
	if err := os.WriteFile(filepath.Join(dir, "intact.md"), []byte(intact), 0o600); err != nil {
		t.Fatalf("write intact.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rewritten.md"), []byte(rewritten), 0o600); err != nil {
		t.Fatalf("write rewritten.md: %v", err)
	}

	got, err := docLiteralOccurrences(dir, "intact.md", schemaLiteral)
	if err != nil || got != 2 {
		t.Fatalf("intact.md count = %d (err %v), want 2", got, err)
	}
	got, err = docLiteralOccurrences(dir, "rewritten.md", schemaLiteral)
	if err != nil || got != 1 {
		t.Fatalf("rewritten.md count = %d (err %v), want 1", got, err)
	}
	missing, err := docsMissingLiteral(dir, []string{"rewritten.md"}, schemaLiteral)
	if err != nil {
		t.Fatalf("scan rewritten.md: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("docsMissingLiteral reported %v; the point of the counter is that a Contains check stays quiet here", missing)
	}
}
