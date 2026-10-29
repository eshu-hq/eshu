// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	vulnerabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/vulnsource"
)

// vulnerabilitySourcesQueryer answers the status snapshot's vulnerability
// source read with fixed rows and every other status read with no rows.
type vulnerabilitySourcesQueryer struct {
	rows [][]any
}

func (q *vulnerabilitySourcesQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	if query == vulnerabilitystore.VulnerabilitySourceStatusQuery {
		return &fakeRows{rows: q.rows}, nil
	}
	return &fakeRows{}, nil
}

// TestReadStatusSnapshotAgesVulnerabilitySourcesAtAsOf is the #9423 wiring
// proof: the status snapshot hands its own as-of time to the source-state
// aging, so a stored fresh row whose last success is older than as-of minus
// vulnsource.DefaultFreshnessWindow reads stale, and an in-window row stays
// fresh. A zero or wall-clock time in place of as-of would fail one of them.
func TestReadStatusSnapshotAgesVulnerabilitySourcesAtAsOf(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	row := func(scope string, success time.Time) []any {
		return []any{
			"live-instance", scope, "osv", "npm",
			nil, nil, success, success, nil, "",
			string(vulnsource.FreshnessFresh), string(vulnsource.TerminalSucceeded),
			int64(1), int64(0), success,
		}
	}
	queryer := &vulnerabilitySourcesQueryer{rows: [][]any{
		row("vuln-intel://osv/npm/old", asOf.Add(-vulnsource.DefaultFreshnessWindow-time.Hour)),
		row("vuln-intel://osv/npm/recent", asOf.Add(-time.Hour)),
	}}
	snapshot, err := NewStatusStore(queryer).ReadStatusSnapshotFiltered(
		context.Background(), asOf, statuspkg.FullSnapshotSelection(),
	)
	if err != nil {
		t.Fatalf("ReadStatusSnapshotFiltered() error = %v", err)
	}
	if len(snapshot.VulnerabilitySources) != 2 {
		t.Fatalf("vulnerability sources = %#v, want 2 rows", snapshot.VulnerabilitySources)
	}
	got := map[string]string{}
	for _, source := range snapshot.VulnerabilitySources {
		got[source.ScopeID] = source.FreshnessState
	}
	if got["vuln-intel://osv/npm/old"] != "stale" || got["vuln-intel://osv/npm/recent"] != "fresh" {
		t.Fatalf("vulnerability source freshness = %v, want old stale and recent fresh at as-of %s", got, asOf)
	}
}
