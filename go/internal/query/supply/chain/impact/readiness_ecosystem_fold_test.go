// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"regexp"
	"strings"
	"testing"
)

// readinessSimpleLocaleFoldPattern matches a LOWER(TRIM(...)) or
// LOWER(BTRIM(...)) fold with no COLLATE inside it, that is a fold that
// follows the database collation.
var readinessSimpleLocaleFoldPattern = regexp.MustCompile(`LOWER\((TRIM|BTRIM)\([^()]*\)\)`)

// readinessIdentityTokenPattern reports whether a SQL fold expression folds a
// package-identity or ecosystem spelling.
var readinessIdentityTokenPattern = regexp.MustCompile(`(?i)\b(ecosystem|package_manager|package_name)\b`)

// TestReadinessEcosystemFoldsASCIIOnly pins the #9622 SQL contract: every fold
// of an ecosystem or package-manager spelling in the readiness queries uses
// LOWER under COLLATE "C" (packageidentity.LowerASCII, the fold that wrote the
// rows being joined), never a locale LOWER that maps U+0130 to i and U+212A
// to k. The trailing COLLATE "default" keeps the folded value comparable
// with the default-collation index (a bare COLLATE "C" value loses the index
// seek, per the #9584 probe).
func TestReadinessEcosystemFoldsASCIIOnly(t *testing.T) {
	t.Parallel()

	const asciiFold = `COLLATE "C") COLLATE "default"`

	type sqlCase struct {
		name string
		sql  string
		// oldLocaleFold is a locale fold the query used before #9622.
		oldLocaleFold []string
		// newASCIIFold counts a shipped ASCII-only fold per expected site.
		newASCIIFold map[string]int
	}

	packageManagerFold := `LOWER(TRIM(payload->'entity_metadata'->>'package_manager') COLLATE "C") COLLATE "default"`
	cases := []sqlCase{
		{
			name: "configured source targets",
			sql:  readinessConfiguredSourceTargetsCTE,
			oldLocaleFold: []string{
				`LOWER(TRIM(target->>'ecosystem'))`,
			},
			newASCIIFold: map[string]int{
				`LOWER(TRIM(target->>'ecosystem') COLLATE "C") COLLATE "default"`: 2,
			},
		},
		{
			name: "unsupported and source targets",
			sql:  listSupplyChainImpactReadinessQueryUnsupportedAndSource,
			oldLocaleFold: []string{
				`LOWER(TRIM(payload->'entity_metadata'->>'package_manager'))`,
				`LOWER(TRIM(warn.payload->>'ecosystem'))`,
			},
			newASCIIFold: map[string]int{
				packageManagerFold + `, ''`:                                             3,
				packageManagerFold + ` NOT IN`:                                          1,
				`LOWER(TRIM(warn.payload->>'ecosystem') COLLATE "C") COLLATE "default"`: 1,
			},
		},
		{
			name: "full readiness query",
			sql:  ListReadinessQuery,
			oldLocaleFold: []string{
				`LOWER(TRIM(payload->'entity_metadata'->>'package_manager'))`,
				`LOWER(TRIM(consumption.payload->>'ecosystem'))`,
				`LOWER(TRIM(registry.payload->>'package_manager'))`,
				`LOWER(TRIM(snapshot.payload->>'ecosystem'))`,
			},
			newASCIIFold: map[string]int{
				packageManagerFold:             5,
				packageManagerFold + `, ''`:    4,
				packageManagerFold + ` NOT IN`: 1,
				`LOWER(TRIM(consumption.payload->>'ecosystem') COLLATE "C") COLLATE "default"`:    2,
				`LOWER(TRIM(registry.payload->>'package_manager') COLLATE "C") COLLATE "default"`: 1,
				`LOWER(TRIM(snapshot.payload->>'ecosystem') COLLATE "C") COLLATE "default"`:       1,
				"END\n    ) " + asciiFold: 1,
			},
		},
		{
			name: "aggregate canonical facts",
			sql:  aggregateCanonicalFactsCTETemplate,
			oldLocaleFold: []string{
				`COALESCE(LOWER(fact.payload->>'ecosystem'), '')`,
				`LOWER(fact.payload->>'ecosystem') = LOWER($7)`,
			},
			newASCIIFold: map[string]int{
				`LOWER((fact.payload->>'ecosystem') COLLATE "C") COLLATE "default"`: 2,
				`LOWER(($7) COLLATE "C") COLLATE "default"`:                         1,
			},
		},
		{
			name: "split findings",
			sql:  listFindingsQueryTemplate,
			oldLocaleFold: []string{
				`LOWER(fact.payload->>'ecosystem') = LOWER($8)`,
			},
			newASCIIFold: map[string]int{
				`LOWER((fact.payload->>'ecosystem') COLLATE "C") COLLATE "default"`: 1,
				`LOWER(($8) COLLATE "C") COLLATE "default"`:                         1,
			},
		},
		{
			name: "winner findings",
			sql:  listFindingsFromWinnersQueryTemplate,
			oldLocaleFold: []string{
				`LOWER(w.ecosystem) = LOWER($8)`,
			},
			newASCIIFold: map[string]int{
				`LOWER((w.ecosystem) COLLATE "C") COLLATE "default"`: 1,
				`LOWER(($8) COLLATE "C") COLLATE "default"`:          1,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, old := range tc.oldLocaleFold {
				if strings.Contains(tc.sql, old) {
					t.Errorf("readiness SQL keeps locale fold %q; use LOWER(x COLLATE \"C\") COLLATE \"default\"", old)
				}
			}
			for shipped, want := range tc.newASCIIFold {
				if got := strings.Count(tc.sql, shipped); got != want {
					t.Errorf("readiness SQL holds %d copies of %q, want %d", got, shipped, want)
				}
			}
			for _, got := range readinessSimpleLocaleFoldPattern.FindAllString(tc.sql, -1) {
				if readinessIdentityTokenPattern.MatchString(got) {
					t.Errorf("readiness SQL keeps locale identity fold %q; use LOWER(x COLLATE \"C\") COLLATE \"default\"", got)
				}
			}
		})
	}
}
