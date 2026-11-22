// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit_test

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/store/audit"
)

// kindJoinBacks is the pinned allowlist of fact_records aliases that carry no
// kind predicate on purpose, keyed by a fragment that identifies the statement
// and the alias. Each is a join-back that re-reads, by fact_id, rows the same
// statement already selected under its own reducer-kind filter.
var kindJoinBacks = []struct{ statement, alias, reason string }{
	{
		"JOIN fact_records AS source ON source.fact_id = page.fact_id", "source",
		"re-reads the page's rows by fact_id; page.fact_id comes from source_candidates, which filters fact_kind = $1",
	},
	{
		"LEFT JOIN fact_records AS override ON override.fact_id = page.override_fact_id", "override",
		"re-reads the operator override by fact_id; override_fact_id comes from operator_candidates, which filters fact_kind = $1",
	},
}

func normalized(sql string) string { return strings.Join(strings.Fields(sql), " ") }

// TestEveryProbedFactRecordsAliasCarriesItsOwnKindPredicate is the kind canary
// over the real statements: every live fact_records alias a probe records has a
// recognized positive kind predicate, except the pinned join-backs, and every
// pinned join-back is still exercised.
func TestEveryProbedFactRecordsAliasCarriesItsOwnKindPredicate(t *testing.T) {
	used := map[string]bool{}
	for _, p := range probes() {
		recorder := &audit.Recorder{}
		p.run(context.Background(), recorder)
		for _, statement := range recorder.Statements() {
			aliases, err := audit.KindlessFactRecordsAliases(statement.SQL)
			if err != nil {
				t.Errorf("%s / %s: canary cannot read a statement: %v", p.capability, p.variant, err)
				continue
			}
			for _, alias := range aliases {
				allowed := false
				for i, joinBack := range kindJoinBacks {
					if alias == joinBack.alias && strings.Contains(normalized(statement.SQL), joinBack.statement) {
						allowed, used[kindJoinBacks[i].statement] = true, true
					}
				}
				if !allowed {
					t.Errorf("%s / %s: fact_records alias %q has no recognized kind predicate (=, IN, = ANY($n)) of its own: %s",
						p.capability, p.variant, alias, strings.TrimSpace(statement.SQL)[:min(100, len(strings.TrimSpace(statement.SQL)))])
				}
			}
		}
	}
	for _, joinBack := range kindJoinBacks {
		if !used[joinBack.statement] {
			t.Errorf("pinned join-back %q (%s) is no longer exercised: delete the allowlist entry", joinBack.alias, joinBack.statement)
		}
	}
}

// TestKindCanaryRejectsEveryHiddenKindForm is the seeded RED set: each form
// puts a reducer-kind read on alias f beside a sibling collector literal on
// alias g, in a shape the classifier does not recognize as a kind predicate.
// The classifier attributes the statement to the sibling alone; the canary
// must flag f. GREEN: the recognized forms pass.
func TestKindCanaryRejectsEveryHiddenKindForm(t *testing.T) {
	t.Parallel()
	const sibling = `SELECT 1 FROM fact_records f, vulnerability_source_states g WHERE g.fact_kind = 'vulnerability.cve' AND `
	for name, predicate := range map[string]string{
		"cast to text":               `f.fact_kind::text = 'reducer_x'`,
		"CAST":                       `CAST(f.fact_kind AS text) = 'reducer_x'`,
		"lower over a cast":          `lower(f.fact_kind::text) = 'reducer_x'`,
		"cast LIKE":                  `f.fact_kind::text LIKE 'reducer_%'`,
		"concatenation":              `f.fact_kind || '' = 'reducer_x'`,
		"COLLATE":                    `f.fact_kind COLLATE "C" = 'reducer_x'`,
		"parenthesized column":       `(f.fact_kind) = 'reducer_x'`,
		"position":                   `position('reducer' in f.fact_kind) = 1`,
		"starts-with operator":       `f.fact_kind ^@ 'reducer'`,
		"range comparison":           `f.fact_kind >= 'reducer_' AND f.fact_kind < 'reducf'`,
		"ANY of an array":            `f.fact_kind = ANY(ARRAY['reducer_x'])`,
		"IN subquery":                `f.fact_kind IN (SELECT k FROM kinds)`,
		"IN subquery of unnest":      `f.fact_kind IN (SELECT unnest($1::text[]))`,
		"NOT of an equality":         `NOT (f.fact_kind = 'vulnerability.cve')`,
		"IS DISTINCT FROM":           `f.fact_kind IS DISTINCT FROM 'vulnerability.cve'`,
		"row compare":                `(f.fact_kind, f.scope_id) = ('reducer_x', 'a')`,
		"NOT IN across aliases":      `f.fact_kind NOT IN ('vulnerability.cve')`,
		"function of the column":     `lower(f.fact_kind) = 'reducer_x'`,
		"no kind predicate at all":   `f.scope_id = 'a'`,
		"predicate on another alias": `g.fact_kind = 'reducer_x'`,
	} {
		aliases, err := audit.KindlessFactRecordsAliases(sibling + predicate)
		if err != nil {
			t.Errorf("%s: canary cannot read the seeded statement: %v", name, err)
			continue
		}
		if len(aliases) != 1 || aliases[0] != "f" {
			t.Errorf("%s RED: canary flagged %v, want [f]", name, aliases)
		}
	}
	for name, sql := range map[string]string{
		"literal":         `SELECT 1 FROM fact_records f WHERE f.fact_kind = 'reducer_x'`,
		"bind":            `SELECT 1 FROM fact_records AS f WHERE f.fact_kind = $1`,
		"ANY of a bind":   `SELECT 1 FROM fact_records f WHERE f.fact_kind = ANY($1::text[])`,
		"IN list":         `SELECT 1 FROM fact_records f WHERE f.fact_kind IN ('reducer_x', $1)`,
		"bare column":     `SELECT 1 FROM fact_records WHERE fact_kind = 'reducer_x'`,
		"two aliases":     `SELECT 1 FROM fact_records a JOIN fact_records b ON b.scope_id = a.scope_id WHERE a.fact_kind = 'reducer_x' AND b.fact_kind = 'reducer_y'`,
		"in a dead arm":   `SELECT 1 FROM ingestion_scopes WHERE EXISTS (SELECT 1 FROM fact_records f WHERE f.scope_id = 'a' AND FALSE)`,
		"quoted relation": `SELECT 1 FROM "fact_records" f WHERE f.fact_kind = 'reducer_x'`,
	} {
		aliases, err := audit.KindlessFactRecordsAliases(sql)
		if err != nil || len(aliases) != 0 {
			t.Errorf("%s GREEN: canary flagged %v (err %v)", name, aliases, err)
		}
	}
	if aliases, _ := audit.KindlessFactRecordsAliases(`SELECT 1 FROM fact_records a JOIN fact_records b ON TRUE WHERE a.fact_kind = 'reducer_x'`); len(aliases) != 1 || aliases[0] != "b" {
		t.Errorf("RED: the second alias carried no predicate of its own, canary flagged %v, want [b]", aliases)
	}
}
