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
		"JOIN fact_records AS fact ON fact.fact_id = consumption.fact_id", "fact",
		"re-reads the package-manifest dependency fact by fact_id; the ids come from package_manifest_consumption_keys, which the readiness statement joins to the target keys",
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

// TestKindCanaryScopesAPredicateToItsOwnRef is the seeded RED set for scoping: a
// predicate somewhere else in the statement must not satisfy a kindless ref.
func TestKindCanaryScopesAPredicateToItsOwnRef(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		sql  string
		want []string
	}{
		"alias reused in another CTE": {
			sql: `WITH a AS (SELECT 1 FROM fact_records AS fact WHERE fact.fact_kind = 'vulnerability.cve'),
				b AS (SELECT 1 FROM fact_records AS fact WHERE fact.scope_id = 's') SELECT 1 FROM a, b`,
			want: []string{"fact"},
		},
		"alias reused in a subquery": {
			sql: `SELECT 1 FROM fact_records AS fact WHERE fact.fact_kind = 'vulnerability.cve' AND EXISTS (
				SELECT 1 FROM fact_records AS fact WHERE fact.scope_id = 's')`,
			want: []string{"fact"},
		},
		"comma without a space": {
			sql:  `SELECT 1 FROM ingestion_scopes s,fact_records f WHERE s.scope_id = 'a'`,
			want: []string{"f"},
		},
		"parenthesized join tree": {
			sql:  `SELECT 1 FROM (fact_records f JOIN ingestion_scopes s ON TRUE) WHERE s.scope_id = 'a'`,
			want: []string{"f"},
		},
		"outer predicate and an inner unaliased ref": {
			sql: `SELECT 1 FROM fact_records WHERE fact_records.fact_kind = 'vulnerability.cve' AND EXISTS (
				SELECT 1 FROM fact_records WHERE scope_id = 's')`,
			want: []string{"fact_records"},
		},
		"the other UNION arm's predicate": {
			sql: `SELECT 1 FROM fact_records WHERE fact_kind = 'vulnerability.cve'
				UNION ALL SELECT 1 FROM fact_records WHERE scope_id = 's'`,
			want: []string{"fact_records"},
		},
	} {
		got, err := audit.KindlessFactRecordsAliases(tc.sql)
		if err != nil || strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s RED: canary flagged %v (err %v), want %v", name, got, err, tc.want)
		}
	}
	// GREEN: each ref carries its own predicate, in its own scope.
	for name, sql := range map[string]string{
		"two CTEs, each with its own": `WITH a AS (SELECT 1 FROM fact_records AS fact WHERE fact.fact_kind = 'vulnerability.cve'),
			b AS (SELECT 1 FROM fact_records AS fact WHERE fact.fact_kind = 'reducer_x') SELECT 1 FROM a, b`,
		"join tree with the WHERE outside": `SELECT 1 FROM (fact_records f JOIN ingestion_scopes s ON TRUE) WHERE f.fact_kind = 'reducer_x'`,
		"correlated predicate in a nested subquery": `SELECT 1 FROM fact_records f WHERE EXISTS (
			SELECT 1 FROM ingestion_scopes s WHERE f.fact_kind = 'reducer_x')`,
		"each UNION arm with its own": `SELECT 1 FROM fact_records WHERE fact_kind = 'reducer_x'
			UNION ALL SELECT 1 FROM fact_records WHERE fact_kind = 'reducer_y'`,
	} {
		if got, err := audit.KindlessFactRecordsAliases(sql); err != nil || len(got) != 0 {
			t.Errorf("%s GREEN: canary flagged %v (err %v)", name, got, err)
		}
	}
}

// TestKindCanaryRequiresARestrictingConjunct is the seeded RED set for presence
// versus restriction: a recognized predicate that does not restrict the ref's
// rows (an OR arm, under a NOT, inside a function, a CASE, an IS test, or in the
// select list) must not satisfy the canary.
func TestKindCanaryRequiresARestrictingConjunct(t *testing.T) {
	t.Parallel()
	const head = `SELECT 1 FROM fact_records f, ingestion_scopes s WHERE `
	for name, sql := range map[string]string{
		"OR with a non-kind arm":    head + `(f.fact_kind = 'vulnerability.cve' OR f.scope_id = 's')`,
		"OR with a hidden arm":      head + `(f.fact_kind = 'vulnerability.cve' OR f.fact_kind::text = 'reducer_x')`,
		"NOT of a conjunction":      head + `NOT (f.scope_id = 'x' AND f.fact_kind = 'vulnerability.cve')`,
		"IS NOT TRUE":               head + `(f.fact_kind = 'vulnerability.cve') IS NOT TRUE`,
		"compared with TRUE":        head + `(f.fact_kind = 'vulnerability.cve') <> TRUE`,
		"COALESCE with TRUE":        head + `COALESCE(f.fact_kind = 'vulnerability.cve', TRUE)`,
		"select-list CASE":          `SELECT CASE WHEN f.fact_kind = 'vulnerability.cve' THEN 1 END FROM fact_records f`,
		"ON clause with an OR":      `SELECT 1 FROM ingestion_scopes s JOIN fact_records f ON f.scope_id = s.scope_id OR f.fact_kind = 'vulnerability.cve'`,
		"OR before an AND conjunct": head + `f.scope_id = 'a' OR s.scope_id = 'b' AND f.fact_kind = 'vulnerability.cve'`,
	} {
		got, err := audit.KindlessFactRecordsAliases(sql)
		if err != nil || len(got) != 1 || got[0] != "f" {
			t.Errorf("%s RED: canary flagged %v (err %v), want [f]", name, got, err)
		}
	}
	for name, sql := range map[string]string{
		"a plain conjunct":                   head + `f.scope_id = 'a' AND f.fact_kind = 'reducer_x'`,
		"a conjunct inside an AND group":     head + `s.scope_id = 'a' AND (f.scope_id = 'a' AND f.fact_kind = 'reducer_x')`,
		"a conjunct in the ON clause":        `SELECT 1 FROM ingestion_scopes s JOIN fact_records f ON f.scope_id = s.scope_id AND f.fact_kind = 'reducer_x'`,
		"a conjunct before ORDER BY":         head + `f.fact_kind = 'reducer_x' ORDER BY f.fact_id`,
		"ANY with a typed bind":              head + `f.fact_kind = ANY($1::text[])`,
		"an OR elsewhere, outside its group": head + `(s.scope_id = 'a' OR s.scope_id = 'b') AND f.fact_kind = 'reducer_x'`,
	} {
		if got, err := audit.KindlessFactRecordsAliases(sql); err != nil || len(got) != 0 {
			t.Errorf("%s GREEN: canary flagged %v (err %v)", name, got, err)
		}
	}
}
