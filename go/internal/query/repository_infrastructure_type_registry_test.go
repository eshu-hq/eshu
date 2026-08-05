// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"regexp"
	"sort"
	"testing"
)

// repositoryInfrastructureCypherLabelPattern extracts the label names the
// production Cypher tests for, across the two spellings
// queryRepoInfrastructureFromGraph's disjunction can use for the same
// predicate: `infra:X` (label syntax, `\s*` before the colon because Cypher
// accepts whitespace there -- `infra :AnsiblePlaybook` parses the same as
// `infra:AnsiblePlaybook`, round-8 P3-2 review follow-up) and
// `'X' IN labels(infra)` (the idiomatic membership-test spelling of the same
// check). Widening to the second form is round-9's P3-2 review follow-up.
//
// This pattern is lexical, not a Cypher parser, and it is scoped to exactly
// the two spellings above -- it is not, and cannot cheaply be made, total.
// Known gaps, left as disclosed residual exposure rather than chased:
//   - a comment between `infra` and its colon (`infra /* x */ :X`) evades
//     the first form; closing this needs a comment-aware preprocessor
//     (nested comments, string literals containing "/*"), disproportionate
//     for a test-only guard.
//   - other Cypher label-membership idioms, for example
//     `ANY(l IN labels(infra) WHERE l = 'X')`, evade the second form; each
//     added spelling only narrows, never closes, this gap.
//   - a backtick-quoted label is unreachable today regardless: the
//     production Cypher is a Go raw string literal, which cannot contain a
//     backtick.
//
// If a future review finds the production Cypher written in one of the
// undetected forms above, that is this pattern reaching its documented
// limit, not a silent regression.
var repositoryInfrastructureCypherLabelPattern = regexp.MustCompile(
	`infra\s*:([A-Za-z0-9_]+)|'([A-Za-z0-9_]+)'\s+IN\s+labels\(\s*infra\s*\)`,
)

// repositoryInfrastructureCypherLabelMatch returns whichever capture group in
// a repositoryInfrastructureCypherLabelPattern match is non-empty: group 1 for
// the `infra:X` spelling, group 2 for the `'X' IN labels(infra)` spelling.
func repositoryInfrastructureCypherLabelMatch(match []string) string {
	if match[1] != "" {
		return match[1]
	}
	return match[2]
}

// TestRepositoryInfrastructureEntryFromContentClassifiesEveryCanonicalType
// pins repositoryInfrastructureEntryFromContent's switch to the canonical
// repositoryInfrastructureEntityTypes list (#5764 round-7 P1-2). Rebuilding
// isRepositoryInfrastructureType from that list removed the last independent
// enumeration that could catch asymmetric drift, so this table test restores a
// cross-check: adding a type to the canonical list without adding it to the
// switch makes ListRepoEntitiesByTypes fetch rows the content path then drops
// at ok=false, narrowing the panel and consuming the 5001-row budget with NO
// truncation signal to explain either.
func TestRepositoryInfrastructureEntryFromContentClassifiesEveryCanonicalType(t *testing.T) {
	t.Parallel()

	for _, entityType := range repositoryInfrastructureEntityTypes {
		t.Run(entityType, func(t *testing.T) {
			t.Parallel()

			entry, ok := repositoryInfrastructureEntryFromContent(EntityContent{
				EntityType:   entityType,
				EntityName:   "example",
				RelativePath: "infra/example.yaml",
			})
			if !ok {
				t.Fatalf("repositoryInfrastructureEntryFromContent(%q) ok = false, want true: the canonical type list fetches this type but the switch drops it", entityType)
			}
			if got := StringVal(entry, "type"); got != entityType {
				t.Fatalf("entry[type] = %q, want %q", got, entityType)
			}
		})
	}
}

// TestRepositoryInfrastructureEntryFromContentRejectsNonCanonicalType is the
// negative half of the pin above: the switch must not classify a type the
// canonical list never fetches, so the `default: return nil, false` arm cannot
// be widened into a catch-all that silently admits every parsed entity type.
func TestRepositoryInfrastructureEntryFromContentRejectsNonCanonicalType(t *testing.T) {
	t.Parallel()

	for _, entityType := range []string{"Function", "Class", "AnsiblePlaybook", ""} {
		if _, ok := repositoryInfrastructureEntryFromContent(EntityContent{
			EntityType:   entityType,
			EntityName:   "example",
			RelativePath: "src/example.go",
		}); ok {
			t.Errorf("repositoryInfrastructureEntryFromContent(%q) ok = true, want false", entityType)
		}
	}
}

// TestRepositoryInfrastructureGraphCypherMatchesCanonicalTypes asserts SET
// EQUALITY between the `infra:` labels in the Cypher queryRepoInfrastructureFromGraph
// actually issues and repositoryInfrastructureEntityTypes (#5764 round-7 P1-2).
// The two enumerations are hand-maintained and gate the same panel from
// opposite sides -- the Go list drives the content read's SQL filter and the
// graph-response gate, the disjunction drives what the graph read returns at
// all -- so a type added or removed on one side only silently changes which
// source can see it. The Cypher is captured from the production call rather
// than re-declared here, so the assertion cannot drift from what ships.
//
// The K8sResource label intentionally covers Crossplane Claims (issue #5347,
// #5478): a Claim stays a K8sResource node, so both sides list K8sResource and
// neither lists CrossplaneClaim.
func TestRepositoryInfrastructureGraphCypherMatchesCanonicalTypes(t *testing.T) {
	t.Parallel()

	var captured string
	reader := fakeRepoGraphReader{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			captured = cypher
			return nil, nil
		},
	}
	if _, _, err := queryRepoInfrastructureFromGraph(t.Context(), reader, map[string]any{"repo_id": "repo-1"}); err != nil {
		t.Fatalf("queryRepoInfrastructureFromGraph() error = %v, want nil", err)
	}

	matches := repositoryInfrastructureCypherLabelPattern.FindAllStringSubmatch(captured, -1)
	cypherLabels := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		label := repositoryInfrastructureCypherLabelMatch(match)
		if _, exists := seen[label]; exists {
			t.Errorf("cypher lists label %q more than once", label)
			continue
		}
		seen[label] = struct{}{}
		cypherLabels = append(cypherLabels, label)
	}

	goTypes := append([]string(nil), repositoryInfrastructureEntityTypes...)
	sort.Strings(goTypes)
	sort.Strings(cypherLabels)

	if len(goTypes) != len(cypherLabels) {
		t.Fatalf("cypher labels %v, repositoryInfrastructureEntityTypes %v: sets differ in size", cypherLabels, goTypes)
	}
	for i, want := range goTypes {
		if cypherLabels[i] != want {
			t.Fatalf("cypher labels %v, repositoryInfrastructureEntityTypes %v: first difference %q vs %q", cypherLabels, goTypes, cypherLabels[i], want)
		}
	}
}
