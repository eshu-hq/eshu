// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import "context"

// SqlBlastRadiusProbe is one ordered cleanup step for a blast-radius fixture
// prefix: the DELETE that empties it plus the VERIFY read-back proving the
// delete left nothing behind.
type SqlBlastRadiusProbe struct {
	What   string
	Delete string
	Verify string
}

// SqlBlastRadiusTableFor names the shared SqlTable fixture one prefix's
// branches converge on.
func SqlBlastRadiusTableFor(prefix string) string { return prefix + "_orders" }

// SqlBlastRadiusProbes lists the deletes that empty one fixture prefix, in
// order: the prefixed fixture nodes, the repositories keyed by id rather than
// repo_id, then the shared table the branches converge on.
//
// It lives here rather than in a package query test file because of the Go
// rule documented on FakeScopedTokenResolver (scopedtoken.go): the moved
// impact/ coverage tests pin the cleanup pairing from outside package query,
// so the probes must be importable.
func SqlBlastRadiusProbes(prefix string) []SqlBlastRadiusProbe {
	table := SqlBlastRadiusTableFor(prefix)
	return []SqlBlastRadiusProbe{
		{
			What:   "prefixed fixture nodes",
			Delete: `MATCH (n) WHERE n.repo_id STARTS WITH '` + prefix + `' DETACH DELETE n`,
			Verify: `MATCH (n) WHERE n.repo_id STARTS WITH '` + prefix + `' RETURN n.repo_id AS leftover LIMIT 5`,
		},
		{
			What:   "fixture repositories",
			Delete: `MATCH (r:Repository) WHERE r.id STARTS WITH '` + prefix + `' DETACH DELETE r`,
			Verify: `MATCH (r:Repository) WHERE r.id STARTS WITH '` + prefix + `' RETURN r.id AS leftover LIMIT 5`,
		},
		{
			What:   "the shared fixture table",
			Delete: `MATCH (t:SqlTable {name: '` + table + `'}) DETACH DELETE t`,
			Verify: `MATCH (t:SqlTable {name: '` + table + `'}) RETURN t.name AS leftover LIMIT 5`,
		},
	}
}

// SqlBlastRadiusFailer is the part of *testing.T that the cleanup helper
// reports through. It carries Logf alongside Errorf deliberately: this helper's
// whole bug history is that it logged where it should have failed, so a
// recorder able to observe only Errorf could not tell a downgrade back to
// logging apart from the call being deleted outright.
type SqlBlastRadiusFailer interface {
	Errorf(format string, args ...any)
	Logf(format string, args ...any)
}

// SqlBlastRadiusRunner executes one cleanup statement against the graph.
// (*Neo4jReader).Run satisfies it, and so does a stub -- which is the only way
// to reach the delete-error, read-back-error, and leftover-rows paths without a
// live backend.
type SqlBlastRadiusRunner func(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)

// SqlBlastRadiusCleanupWith deletes every node a prefix's fixtures created
// plus the shared table they converge on, failing through failer when a delete
// errors or leaves anything behind.
//
// The seam exists because a real backend does not fail on demand. Without it
// the three reporting decisions below could only be exercised by a live run,
// which is how a downgrade from Errorf to Logf sat here undetected by every
// test that runs without a backend.
func SqlBlastRadiusCleanupWith(ctx context.Context, failer SqlBlastRadiusFailer, run SqlBlastRadiusRunner, prefix string) {
	for _, probe := range SqlBlastRadiusProbes(prefix) {
		if _, err := run(ctx, probe.Delete, nil); err != nil {
			failer.Errorf("cleanup of %s failed: %v -- fixtures are left in the graph this gate "+
				"shares with the replay tier's exact node and edge assertions (%s)",
				probe.What, err, probe.Delete)
			continue
		}
		rows, err := run(ctx, probe.Verify, nil)
		if err != nil {
			failer.Errorf("cleanup of %s could not be confirmed: %v -- the delete reported success "+
				"but the graph was never read back (%s)", probe.What, err, probe.Verify)
			continue
		}
		if len(rows) > 0 {
			failer.Errorf("cleanup of %s left %v behind -- the delete reported success and the "+
				"fixtures are still there, which is the #6182 leak reached by another route",
				probe.What, SqlBlastRadiusLeftovers(rows))
		}
	}
}

// SqlBlastRadiusLeftovers reduces the verify rows to the identifiers they
// carry, so a cleanup failure names the fixtures still in the graph.
func SqlBlastRadiusLeftovers(rows []map[string]any) []string {
	leftovers := make([]string, 0, len(rows))
	for _, row := range rows {
		value, _ := row["leftover"].(string)
		leftovers = append(leftovers, value)
	}
	return leftovers
}
