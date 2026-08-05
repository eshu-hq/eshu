// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"regexp"
	"sort"
	"testing"
)

// repositoryInfrastructureCypherLabelPattern extracts the label names from the
// `WHERE infra:X OR infra:Y ...` disjunction in the production Cypher. `\s*`
// before the colon matters: Cypher accepts whitespace between a variable and
// its label colon (`infra :AnsiblePlaybook` parses the same as
// `infra:AnsiblePlaybook`), and a bare `infra:` pattern misses that spelling
// -- an added disjunct in that form would still change what the graph read
// matches while evading this extraction (#5764 round-8 P3-2 review
// follow-up).
var repositoryInfrastructureCypherLabelPattern = regexp.MustCompile(`infra\s*:([A-Za-z0-9_]+)`)

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
		if _, exists := seen[match[1]]; exists {
			t.Errorf("cypher lists label %q more than once", match[1])
			continue
		}
		seen[match[1]] = struct{}{}
		cypherLabels = append(cypherLabels, match[1])
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
