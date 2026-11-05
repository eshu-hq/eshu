// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
)

// errChangedNamesProbe is the staged store failure for the fail-closed test.
var errChangedNamesProbe = errors.New("changed names probe failure")

// TestListVulnerabilityChangedNamesReadsActiveChangedFacts pins the
// changed_name reader contract (#9777 step 2): the read selects the five
// fact-shape fields from active generations only, guards blanks and the
// kind, and fails closed on a store error.
func TestListVulnerabilityChangedNamesReadsActiveChangedFacts(t *testing.T) {
	t.Parallel()

	queryer := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{{Data: [][]any{
			{"GHSA-a", "go", "example.com/mod", true, "2026-11-01T12:00:00Z"},
			{"GHSA-b", "npm", "left-pad", false, "2026-11-01T12:01:00Z"},
		}}},
	}
	store := NewFactStore(queryer)

	changed, err := store.ListVulnerabilityChangedNames(context.Background())
	if err != nil {
		t.Fatalf("ListVulnerabilityChangedNames() error = %v, want nil", err)
	}
	if len(changed) != 2 {
		t.Fatalf("len(changed) = %d, want 2", len(changed))
	}
	if changed[0].AdvisoryID != "GHSA-a" || changed[0].Ecosystem != "go" ||
		changed[0].PackageName != "example.com/mod" || !changed[0].Withdrawn ||
		changed[0].FeedTimestamp != "2026-11-01T12:00:00Z" {
		t.Fatalf("first changed fact = %+v, want the GHSA-a go row withdrawn", changed[0])
	}
	if len(queryer.Queries) != 1 {
		t.Fatalf("len(queries) = %d, want 1", len(queryer.Queries))
	}
	query := queryer.Queries[0].Query
	for _, want := range []string{
		"vulnerability.changed_name",
		"is_tombstone = FALSE",
		"active_generation_id",
		"withdrawn",
		"feed_timestamp",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("changed_name read missing %q:\n%s", want, query)
		}
	}
}

// TestListVulnerabilityChangedNamesSkipsBlankRows pins that rows with a
// blank advisory, ecosystem, package, or feed timestamp never reach the
// intersection: a blank is a malformed fact, not a lookup.
func TestListVulnerabilityChangedNamesSkipsBlankRows(t *testing.T) {
	t.Parallel()

	queryer := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{{Data: [][]any{
			{"", "npm", "left-pad", false, "2026-11-01T12:00:00Z"},
			{"GHSA-c", "npm", "  ", false, "2026-11-01T12:00:00Z"},
			{"GHSA-d", "npm", "kept", false, "2026-11-01T12:00:00Z"},
		}}},
	}
	store := NewFactStore(queryer)

	changed, err := store.ListVulnerabilityChangedNames(context.Background())
	if err != nil {
		t.Fatalf("ListVulnerabilityChangedNames() error = %v, want nil", err)
	}
	if len(changed) != 1 || changed[0].AdvisoryID != "GHSA-d" {
		t.Fatalf("changed = %+v, want only the complete GHSA-d row", changed)
	}
}

// TestListVulnerabilityChangedNamesFailsClosedOnQueryError pins that a read
// failure is returned, never answered with an empty change set: planning
// nothing on an error would stall the replan queue while hiding the outage.
func TestListVulnerabilityChangedNamesFailsClosedOnQueryError(t *testing.T) {
	t.Parallel()

	queryer := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{{FailWith: errChangedNamesProbe}},
	}
	store := NewFactStore(queryer)

	if _, err := store.ListVulnerabilityChangedNames(context.Background()); err == nil {
		t.Fatal("ListVulnerabilityChangedNames() succeeded on a store error, want fail-closed")
	}
}
