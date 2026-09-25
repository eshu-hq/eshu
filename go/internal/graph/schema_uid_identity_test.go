// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph_test

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// uidIdentityComponents are the graph properties that carry the canonical
// entity uid's identity: content.CanonicalEntityID hashes repo, relative path,
// entity type, name, and start line, and the writer stores path (absolute, so
// it carries repo and relative path), name, and line_number on the node. A
// uniqueness key that omits any of them can collide on a node the writer
// considers distinct, because the writer upserts the new uid before
// entity_retract deletes the prior generation's node (#7095).
var uidIdentityComponents = []string{"name", "path", "line_number"}

var uniqueConstraintPattern = regexp.MustCompile(
	`^CREATE CONSTRAINT (\w+) IF NOT EXISTS FOR \(\s*(\w+)\s*:\s*(\w+)\s*\) REQUIRE (.+) IS UNIQUE$`,
)

// narrowUIDIdentityConstraints returns "name(label)" for every uniqueness
// constraint in statements whose label is in uidLabels and whose key is
// neither {uid} nor a superset of uidIdentityComponents.
func narrowUIDIdentityConstraints(statements []string, uidLabels map[string]struct{}) []string {
	var violations []string
	for _, statement := range statements {
		match := uniqueConstraintPattern.FindStringSubmatch(statement)
		if match == nil {
			continue
		}
		name, variable, label, keyExpr := match[1], match[2], match[3], match[4]
		if _, ok := uidLabels[label]; !ok {
			continue
		}
		keyExpr = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(keyExpr), "("), ")")
		key := make([]string, 0, 4)
		for _, part := range strings.Split(keyExpr, ",") {
			key = append(key, strings.TrimPrefix(strings.TrimSpace(part), variable+"."))
		}
		if slices.Equal(key, []string{"uid"}) {
			continue
		}
		superset := true
		for _, component := range uidIdentityComponents {
			if !slices.Contains(key, component) {
				superset = false
				break
			}
		}
		if !superset {
			violations = append(violations, name+"("+label+")")
		}
	}
	sort.Strings(violations)
	return violations
}

// nonUIDEntityPhaseLabels are the mapped labels the canonical builder routes
// away from the uid entity phase (projector/canonical/builder.go): Module
// MERGEs on (name, lang) and Parameter on (name, path, function_line_number).
var nonUIDEntityPhaseLabels = []string{"Module", "Parameter"}

// canonicalUIDMergedLabels is the set of labels the canonical entity writer
// MERGEs on uid: the storage/cypher entity upsert templates format
// `MERGE (n:<label> {uid: row.entity_id})` for every label an entity_type maps
// to, except nonUIDEntityPhaseLabels. Derived from the projector's own map,
// not a hand copy.
func canonicalUIDMergedLabels(t *testing.T) map[string]struct{} {
	t.Helper()
	labels := make(map[string]struct{})
	for _, label := range canonical.EntityTypeLabelMap() {
		labels[label] = struct{}{}
	}
	for _, label := range nonUIDEntityPhaseLabels {
		if _, ok := labels[label]; !ok {
			t.Fatalf("excluded label %s is no longer mapped; drop it from nonUIDEntityPhaseLabels", label)
		}
		delete(labels, label)
	}
	return labels
}

// TestUniqueConstraintsDoNotNarrowCanonicalUIDIdentity is the #7095 / #7097
// guard: on every backend, a uid-MERGEd canonical label must not carry a
// uniqueness constraint narrower than the uid identity, or a moved block
// fails the delta on the constraint (Neo4j ConstraintValidationFailed,
// NornicDB Transaction.Outdated) instead of leaving one node.
func TestUniqueConstraintsDoNotNarrowCanonicalUIDIdentity(t *testing.T) {
	t.Parallel()

	labels := canonicalUIDMergedLabels(t)
	for _, want := range []string{"TerraformModule", "HelmChart", "HelmValues", "KustomizeOverlay", "TerragruntConfig"} {
		if _, ok := labels[want]; !ok {
			t.Fatalf("canonical uid-MERGEd label set lacks %s; the guard no longer derives the set it protects", want)
		}
	}

	for _, backend := range []graph.SchemaBackend{graph.SchemaBackendNeo4j, graph.SchemaBackendNornicDB} {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()

			statements, err := graph.SchemaStatementsForBackend(backend)
			if err != nil {
				t.Fatalf("SchemaStatementsForBackend(%s) error = %v", backend, err)
			}
			constraints := 0
			for _, statement := range statements {
				if uniqueConstraintPattern.MatchString(statement) {
					constraints++
				}
			}
			if constraints < 100 {
				t.Fatalf("parsed %d %s uniqueness constraints, want at least 100; the statement pattern no longer matches the schema", constraints, backend)
			}

			if got := narrowUIDIdentityConstraints(statements, labels); len(got) != 0 {
				t.Fatalf("%s uniqueness constraints narrower than the canonical uid identity: %v", backend, got)
			}
		})
	}
}

// TestNarrowUIDIdentityConstraintsSeededViolation proves the guard's checker
// flags a planted narrow key and passes uid and superset keys.
func TestNarrowUIDIdentityConstraintsSeededViolation(t *testing.T) {
	t.Parallel()

	labels := map[string]struct{}{"TerraformModule": {}, "HelmValues": {}, "K8sResource": {}}
	statements := []string{
		"CREATE CONSTRAINT tf_module_unique IF NOT EXISTS FOR (m:TerraformModule) REQUIRE (m.name, m.path) IS UNIQUE",
		"CREATE CONSTRAINT helm_values_unique IF NOT EXISTS FOR (hv:HelmValues) REQUIRE hv.path IS UNIQUE",
		"CREATE CONSTRAINT terraform_module_uid_unique IF NOT EXISTS FOR (n:TerraformModule) REQUIRE n.uid IS UNIQUE",
		"CREATE CONSTRAINT k8s_resource_unique IF NOT EXISTS FOR (k:K8sResource) REQUIRE (k.name, k.kind, k.path, k.line_number) IS UNIQUE",
		"CREATE CONSTRAINT other_unique IF NOT EXISTS FOR (o:Other) REQUIRE o.path IS UNIQUE",
	}
	got := narrowUIDIdentityConstraints(statements, labels)
	want := []string{"helm_values_unique(HelmValues)", "tf_module_unique(TerraformModule)"}
	if !slices.Equal(got, want) {
		t.Fatalf("narrowUIDIdentityConstraints = %v, want %v", got, want)
	}
	if got := narrowUIDIdentityConstraints(statements[2:], labels); len(got) != 0 {
		t.Fatalf("clean statements flagged: %v", got)
	}
}
