// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// neo4jSchemaAnchorLabelSets parses the Neo4j schema DDL into the three label
// sets the entity-id anchor seeks: uid uniqueness constraints, uid RANGE
// indexes on labels without a uid constraint, and id uniqueness constraints.
func neo4jSchemaAnchorLabelSets(t *testing.T) (uidConstrained, uidIndexed, idConstrained []string) {
	t.Helper()
	stmts, err := graph.SchemaStatementsForBackend(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend() error = %v", err)
	}
	uidRe := regexp.MustCompile(`FOR \((\w+):(\w+)\) REQUIRE (\w+)\.uid IS UNIQUE`)
	uidIndexRe := regexp.MustCompile(`^CREATE INDEX \w+ IF NOT EXISTS FOR \((\w+):(\w+)\) ON \((\w+)\.uid\)$`)
	idRe := regexp.MustCompile(`FOR \((\w+):(\w+)\) REQUIRE (\w+)\.id IS UNIQUE`)
	for _, stmt := range stmts {
		if m := uidRe.FindStringSubmatch(stmt); m != nil && m[1] == m[3] {
			uidConstrained = append(uidConstrained, m[2])
		}
		if m := uidIndexRe.FindStringSubmatch(stmt); m != nil && m[1] == m[3] {
			uidIndexed = append(uidIndexed, m[2])
		}
		if m := idRe.FindStringSubmatch(stmt); m != nil && m[1] == m[3] {
			idConstrained = append(idConstrained, m[2])
		}
	}
	// A uid index on a label that also has the uid constraint adds nothing to
	// the anchor; only unconstrained uid-indexed labels form the middle branch.
	uidIndexed = slices.DeleteFunc(uidIndexed, func(label string) bool {
		return slices.Contains(uidConstrained, label)
	})
	slices.Sort(uidConstrained)
	slices.Sort(uidIndexed)
	slices.Sort(idConstrained)
	return uidConstrained, uidIndexed, idConstrained
}

// TestNeo4jEntityIDAnchorLabelsMatchSchema pins the three anchor label lists
// to the Neo4j schema DDL, so a label that gains a uid or id uniqueness
// constraint, or a uid index, cannot silently fall out of the entity-id anchor
// (issue #7057).
func TestNeo4jEntityIDAnchorLabelsMatchSchema(t *testing.T) {
	t.Parallel()

	uidLabels, uidIndexLabels, idLabels := neo4jSchemaAnchorLabelSets(t)
	if len(uidLabels) == 0 || len(uidIndexLabels) == 0 || len(idLabels) == 0 {
		t.Fatalf("parsed %d uid-constrained, %d uid-indexed and %d id-constrained labels; the DDL shape changed",
			len(uidLabels), len(uidIndexLabels), len(idLabels))
	}
	if got := slices.Sorted(slices.Values(neo4jEntityUIDAnchorLabels)); !slices.Equal(got, uidLabels) {
		t.Errorf("neo4jEntityUIDAnchorLabels = %v\nschema uid-constrained labels = %v", got, uidLabels)
	}
	if got := slices.Sorted(slices.Values(neo4jEntityUIDIndexAnchorLabels)); !slices.Equal(got, uidIndexLabels) {
		t.Errorf("neo4jEntityUIDIndexAnchorLabels = %v\nschema uid-indexed (unconstrained) labels = %v", got, uidIndexLabels)
	}
	if got := slices.Sorted(slices.Values(neo4jEntityIDAnchorLabels)); !slices.Equal(got, idLabels) {
		t.Errorf("neo4jEntityIDAnchorLabels = %v\nschema id-constrained labels = %v", got, idLabels)
	}
	for _, label := range idLabels {
		if slices.Contains(uidLabels, label) || slices.Contains(uidIndexLabels, label) {
			t.Errorf("label %q is in the id list and a uid list; its nodes would be probed twice", label)
		}
	}
}

// uidMergeLabelsRe matches a literal uid-keyed MERGE and captures its label
// expression, e.g. "Package:PackageRegistryPackage".
var uidMergeLabelsRe = regexp.MustCompile(`MERGE \(\w*:([A-Za-z0-9:]+)\s*\{uid:`)

// TestNeo4jEntityIDAnchorCoversEveryUIDWriter fails when a production graph
// writer MERGEs a node by uid under a label the anchor cannot seek. Every such
// uid can come back to a caller -- the relationships row returns neighbours'
// coalesce(id, uid) as source_id/target_id -- so a label outside all three
// anchor lists makes that id resolve on NornicDB and 404 on Neo4j (#7057 F1).
// The fix is a uid constraint or a uid index for the label, which the schema
// pin above then carries into the anchor.
func TestNeo4jEntityIDAnchorCoversEveryUIDWriter(t *testing.T) {
	t.Parallel()

	anchored := map[string]struct{}{}
	for _, list := range [][]string{neo4jEntityUIDAnchorLabels, neo4jEntityUIDIndexAnchorLabels, neo4jEntityIDAnchorLabels} {
		for _, label := range list {
			anchored[label] = struct{}{}
		}
	}

	merges := 0
	for _, dir := range []string{"../../storage/cypher", "../../reducer"} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range uidMergeLabelsRe.FindAllStringSubmatch(string(src), -1) {
				merges++
				labels := strings.Split(m[1], ":")
				if !slices.ContainsFunc(labels, func(l string) bool { _, ok := anchored[l]; return ok }) {
					t.Errorf("%s: MERGE on uid for %q, but no label is in an entity-id anchor list", path, m[1])
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	if merges == 0 {
		t.Fatal("found no literal uid-keyed MERGE; the scan is vacuous")
	}

	// The canonical entity writer MERGEs every entityTypeLabelMap label on uid
	// through a formatted template the literal scan cannot see. Module and
	// Parameter take their own name-keyed phases (projector/canonical
	// builder.go), so they are the only exceptions.
	for _, label := range canonical.EntityTypeLabelMap() {
		if label == "Module" || label == "Parameter" {
			continue
		}
		if _, ok := anchored[label]; !ok {
			t.Errorf("canonical entity label %q is MERGEd on uid but is in no entity-id anchor list", label)
		}
	}
}

// TestNeo4jEntityIDAnchorShape pins the rendered clause: a scoped, uncorrelated
// CALL () subquery with inline {uid:} and {id:} branches, and no WHERE-side
// id-OR-uid predicate.
func TestNeo4jEntityIDAnchorShape(t *testing.T) {
	t.Parallel()

	got := Neo4jEntityIDAnchor("e", "$entity_id")
	for _, want := range []string{
		"CALL () {\n",
		"MATCH (e:AnalyticsModel|",
		"|Variable {uid: $entity_id})",
		"MATCH (e:DocumentationSection|Rationale {uid: $entity_id})",
		"MATCH (e:CloudAction|Endpoint|EvidenceArtifact|Platform|Repository|Workload|WorkloadInstance {id: $entity_id})",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Neo4jEntityIDAnchor() missing %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "UNION"); n != 2 {
		t.Errorf("Neo4jEntityIDAnchor() has %d UNIONs, want 2:\n%s", n, got)
	}
	if strings.Contains(got, "WHERE") || strings.Contains(got, " OR ") {
		t.Errorf("Neo4jEntityIDAnchor() must anchor inline, not filter:\n%s", got)
	}
}
