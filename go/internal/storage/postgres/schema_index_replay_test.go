// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// This directory has no applied-migration ledger. BootstrapDefinitions
// enumerates every file under migrations/ and ApplyDefinitions Execs all of
// them, in filename order, on every bootstrap;
// TestApplyBootstrapExecutesDefinitionsInOrder pins that. Every service start
// therefore replays the whole directory, so a migration is a desired-state
// statement that has to be a no-op once the state it asks for already holds.
//
// An index name that one definition CREATEs and another DROPs breaks that. The
// drop leaves the name absent, so the next bootstrap's `IF NOT EXISTS` no
// longer skips and rebuilds the index -- concurrently, over a populated table
// -- and the drop removes it again, on every startup, forever. Replacing an
// index therefore means creating the replacement under a NEW name and dropping
// the old one, with no create of the old name left in the tree:
// 059_relationship_family_candidate_index.sql with
// 068_drop_relationship_family_candidate_index_legacy.sql is the shape, and
// 101/102 is the same shape for code_reachability_rows.

// migrationIndexCreatePattern and migrationIndexDropPattern read the index name
// out of a CREATE INDEX or DROP INDEX statement in any of the spellings this
// directory uses: UNIQUE, CONCURRENTLY, and IF [NOT] EXISTS are all optional.
var (
	migrationIndexCreatePattern = regexp.MustCompile(
		`(?is)\bCREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?([A-Za-z_][A-Za-z0-9_$]*)`)
	migrationIndexDropPattern = regexp.MustCompile(
		`(?is)\bDROP\s+INDEX\s+(?:CONCURRENTLY\s+)?(?:IF\s+EXISTS\s+)?([A-Za-z_][A-Za-z0-9_$]*)`)
)

// replayRebuiltIndexNames are the index names a bootstrap both drops and
// recreates today, and therefore rebuilds on every single startup. It is a
// record of a known defect, not a licence to add another: the assertion below
// requires the violating set to equal this list exactly, so a new offender
// fails the test and fixing this one fails it too until the entry goes.
//
// fact_records_identity_epoch_idx is created by migration 069, dropped by 076
// and created again under the SAME name with a wider predicate by 077, so every
// bootstrap drops it and rebuilds it concurrently over fact_records. Deleting a
// file cannot fix it the way it fixed code_reachability_entity_repository_idx,
// because the replacement reuses the name: an install still holding 069's
// definition has nothing to distinguish it from 077's, so converging those
// installs needs the replacement renamed, which changes an index the
// container-image identity path is measured against and needs its own proof.
var replayRebuiltIndexNames = []string{"fact_records_identity_epoch_idx"}

// TestBootstrapDefinitionsDoNotRebuildIndexesOnEveryReplay fails when a
// bootstrap definition creates an index name another definition drops, because
// the pair costs a concurrent index build on every service start.
func TestBootstrapDefinitionsDoNotRebuildIndexesOnEveryReplay(t *testing.T) {
	t.Parallel()

	created := map[string][]string{}
	dropped := map[string][]string{}
	createCount, dropCount := 0, 0
	for _, definition := range BootstrapDefinitions() {
		statements := stripSQLLineComments(definition.SQL)
		for _, match := range migrationIndexCreatePattern.FindAllStringSubmatch(statements, -1) {
			created[match[1]] = append(created[match[1]], definition.Path)
			createCount++
		}
		for _, match := range migrationIndexDropPattern.FindAllStringSubmatch(statements, -1) {
			dropped[match[1]] = append(dropped[match[1]], definition.Path)
			dropCount++
		}
	}
	// A regex that stopped matching would make every assertion below vacuously
	// true, so require the scan to have found the statements it exists to read.
	if createCount < 200 || dropCount < 5 {
		t.Fatalf("scanned %d CREATE INDEX and %d DROP INDEX statements, want at least 200 and 5; the statement patterns no longer match the migrations",
			createCount, dropCount)
	}

	rebuilt := make([]string, 0, len(dropped))
	for name := range dropped {
		if _, ok := created[name]; ok {
			rebuilt = append(rebuilt, name)
		}
	}
	slices.Sort(rebuilt)
	want := slices.Clone(replayRebuiltIndexNames)
	slices.Sort(want)
	if !slices.Equal(rebuilt, want) {
		for _, name := range rebuilt {
			if !slices.Contains(want, name) {
				t.Errorf("index %s is created by %v and dropped by %v, so every bootstrap rebuilds it; create the replacement under a new name and leave no create of the dropped one",
					name, created[name], dropped[name])
			}
		}
		for _, name := range want {
			if !slices.Contains(rebuilt, name) {
				t.Errorf("index %s no longer has a create/drop pair; remove it from replayRebuiltIndexNames", name)
			}
		}
	}
}

// TestCodeReachabilityWalkIndexIsCreatedOnceAndNeverDropped pins the specific
// end state #5167 needs: the four-column walk index has exactly one create and
// no drop, and the two-column index it supersedes has a drop and no create at
// all, so a steady-state bootstrap issues neither a build nor a drop for
// code_reachability_rows.
func TestCodeReachabilityWalkIndexIsCreatedOnceAndNeverDropped(t *testing.T) {
	t.Parallel()

	const (
		walkIndex       = "code_reachability_entity_repository_scope_generation_idx"
		supersededIndex = "code_reachability_entity_repository_idx"
	)
	creates, drops := map[string]int{}, map[string]int{}
	for _, definition := range BootstrapDefinitions() {
		statements := stripSQLLineComments(definition.SQL)
		for _, match := range migrationIndexCreatePattern.FindAllStringSubmatch(statements, -1) {
			creates[match[1]]++
		}
		for _, match := range migrationIndexDropPattern.FindAllStringSubmatch(statements, -1) {
			drops[match[1]]++
		}
	}
	for _, want := range []struct {
		name    string
		creates int
		drops   int
	}{
		{name: walkIndex, creates: 1, drops: 0},
		{name: supersededIndex, creates: 0, drops: 1},
	} {
		if creates[want.name] != want.creates || drops[want.name] != want.drops {
			t.Errorf("%s has %d creates and %d drops, want %d and %d",
				want.name, creates[want.name], drops[want.name], want.creates, want.drops)
		}
	}
}

// stripSQLLineComments removes whole-line `--` comments so prose describing a
// statement is not read as one. Every comment in migrations/ is a whole-line
// comment; a trailing comment on a statement line is left alone deliberately,
// because removing it would need an SQL lexer to tell a comment from a `--`
// inside a string literal.
func stripSQLLineComments(sql string) string {
	lines := strings.Split(sql, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// TestCodeReachabilityPageRankIndexIsCreatedOnceAndNeverDropped pins the same
// end state for the index the cross-repo consumer-evidence PAGE needs (#6527).
// That statement orders a producer entity's consumers by confidence ahead of
// its LIMIT, so without an index in that order Postgres has to read the whole
// fan-in group before it can emit the group's first row. The index carrying the
// order is a create with no drop, like the walk index above and unlike
// fact_records_identity_epoch_idx: an install that already has it does no index
// work on bootstrap.
func TestCodeReachabilityPageRankIndexIsCreatedOnceAndNeverDropped(t *testing.T) {
	t.Parallel()

	const pageRankIndex = "code_reachability_entity_confidence_rank_idx"
	creates, drops := 0, 0
	for _, definition := range BootstrapDefinitions() {
		statements := stripSQLLineComments(definition.SQL)
		for _, match := range migrationIndexCreatePattern.FindAllStringSubmatch(statements, -1) {
			if match[1] == pageRankIndex {
				creates++
			}
		}
		for _, match := range migrationIndexDropPattern.FindAllStringSubmatch(statements, -1) {
			if match[1] == pageRankIndex {
				drops++
			}
		}
	}
	if creates != 1 || drops != 0 {
		t.Errorf("%s has %d creates and %d drops, want 1 and 0", pageRankIndex, creates, drops)
	}
}
