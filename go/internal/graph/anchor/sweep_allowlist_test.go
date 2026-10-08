// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dynamicLabelWriter names a Cypher template whose node label is chosen at run
// time. The static sweep cannot decide which labels it writes, so each one is
// listed here with the reason it is acceptable and the proof that covers it. A
// new dynamic-label writer fails the sweep until someone adds a row, which is
// the review point. Editing a listed template changes its Hash and also fails,
// so the row is re-read.
type dynamicLabelWriter struct {
	// File is the repo-go-relative path of the Go file.
	File string
	// Hash is templateHash of the template text.
	Hash string
	// Reason says why the writer is acceptable and what covers it.
	Reason string
}

const (
	reasonEntityTemplate = "label is chosen from the canonical entity label set at write time and the id arrives through the row props; the writer-coverage replay phase proves each executed label and props map from the recording"
	reasonSemanticEntity = "label is chosen from the semantic entity label set at write time and the id arrives through the row props or an explicit set clause; the writer-coverage replay phase proves each executed label from the recording"
	reasonGraphBatch     = "label comes from the caller's entity label in the batch helper; identity is uid- or name-keyed and the id arrives through the shared entity set clause; the writer-coverage replay phase proves each executed label"
	reasonSeedTool       = "synthetic seeding tool for the read-API latency gate; it writes benchmark nodes into an isolated benchmark graph and never runs in a deployed runtime"
)

// dynamicLabelWriters is the named list of dynamic-label writers in go/.
var dynamicLabelWriters = []dynamicLabelWriter{
	{"cmd/read-api-latency-gate/seed_graph.go", "1d83d8366c38", reasonSeedTool},
	{"cmd/read-api-latency-gate/seed_graph.go", "3f270bcdfc5b", reasonSeedTool},
	{"internal/graph/batch.go", "b18fd6a9fb3e", reasonGraphBatch},
	{"internal/graph/batch.go", "690c6100d91b", reasonGraphBatch},
	{"internal/graph/entity.go", "cb5a90ecce6e", reasonGraphBatch},
	{"internal/storage/cypher/canonical_node_cypher.go", "1a6268a7b37a", reasonEntityTemplate},
	{"internal/storage/cypher/canonical_node_cypher.go", "53b63d7ff55e", reasonEntityTemplate},
	{"internal/storage/cypher/canonical_node_cypher.go", "31b9eddc364d", reasonEntityTemplate},
	{"internal/storage/cypher/canonical_node_cypher.go", "786c886d3101", reasonEntityTemplate},
	{"internal/storage/cypher/canonical_node_cypher.go", "ebed2b2759e3", reasonEntityTemplate},
	{"internal/storage/cypher/semantic_entity_statements.go", "f37e0baeefbf", reasonSemanticEntity},
	{"internal/storage/cypher/semantic_entity_statements.go", "3533e619b43d", reasonSemanticEntity},
	{"internal/storage/cypher/semantic_entity_statements.go", "d519a87064d9", reasonSemanticEntity},
}

// unlistedDynamicWriters returns the dynamic sites with no allowlist row, and
// the allowlist rows with no site.
func unlistedDynamicWriters(sites []cypherSite, list []dynamicLabelWriter) (unlisted, stale []string) {
	listed := make(map[string]bool, len(list))
	for _, row := range list {
		listed[row.File+"#"+row.Hash] = true
	}
	seen := make(map[string]bool)
	for _, site := range sites {
		key := site.File + "#" + site.Hash
		seen[key] = true
		if !listed[key] {
			unlisted = append(unlisted, site.Key+" hash="+site.Hash)
		}
	}
	for _, row := range list {
		if !seen[row.File+"#"+row.Hash] {
			stale = append(stale, row.File+" hash="+row.Hash)
		}
	}
	return unlisted, stale
}

func TestEveryDynamicLabelWriterIsNamed(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	result := sweepSource(t, root, Labels())
	if len(result.Dynamic) < 10 {
		t.Fatalf("static sweep found %d dynamic-label writers; the sweep is not reading go/", len(result.Dynamic))
	}
	for _, row := range dynamicLabelWriters {
		if strings.TrimSpace(row.Reason) == "" {
			t.Errorf("allowlist row %s %s has no reason", row.File, row.Hash)
		}
	}
	unlisted, stale := unlistedDynamicWriters(result.Dynamic, dynamicLabelWriters)
	if len(unlisted) > 0 {
		t.Errorf("dynamic-label writer(s) with no allowlist row (add one with a reason, or give the writer a static label):\n%s", strings.Join(unlisted, "\n"))
	}
	if len(stale) > 0 {
		t.Errorf("allowlist row(s) that match no writer (remove them):\n%s", strings.Join(stale, "\n"))
	}
}

// TestUnlistedDynamicLabelWriterFails is the seeded violation: a new template
// writer in a scratch tree is reported, and listing it clears the report.
func TestUnlistedDynamicLabelWriterFails(t *testing.T) {
	root := t.TempDir()
	source := "package planted\n\nconst plantedTemplate = `UNWIND $rows AS row MERGE (n:%s {uid: row.uid}) SET n += row.props`\n"
	if err := os.WriteFile(filepath.Join(root, "planted.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	result := sweepSource(t, root, Labels())
	if len(result.Dynamic) != 1 {
		t.Fatalf("dynamic sites = %d, want the planted template", len(result.Dynamic))
	}
	unlisted, stale := unlistedDynamicWriters(result.Dynamic, nil)
	if len(unlisted) != 1 || len(stale) != 0 {
		t.Fatalf("unlisted=%v stale=%v, want the planted template unlisted", unlisted, stale)
	}
	listed := []dynamicLabelWriter{{File: "planted.go", Hash: result.Dynamic[0].Hash, Reason: "test"}}
	if unlisted, stale := unlistedDynamicWriters(result.Dynamic, listed); len(unlisted) != 0 || len(stale) != 0 {
		t.Fatalf("a listed writer still reported: unlisted=%v stale=%v", unlisted, stale)
	}
	if _, stale := unlistedDynamicWriters(nil, listed); len(stale) != 1 {
		t.Fatalf("a row with no writer must be stale, got %v", stale)
	}
}

// TestSweepAdmitsTheUnlabeledAndDynamicMapShapes plants the two shapes the
// first prefilter dropped: an unlabeled id write, and a dynamic property map
// on an uncovered label. Both must reach CheckWriters and fail.
func TestSweepAdmitsTheUnlabeledAndDynamicMapShapes(t *testing.T) {
	for name, cypher := range map[string]string{
		"unlabeled id write":               "MERGE (n {id: $id}) SET n.name = $name",
		"dynamic map on uncovered label":   "MERGE (n:Unconstrained {uid: $uid}) SET n += $props",
		"concatenated label is a template": "\"MERGE (n:\" + label + \" {uid: $uid}) SET n.id = $uid\"",
	} {
		root := t.TempDir()
		var source string
		if strings.HasPrefix(cypher, "\"") {
			source = "package planted\n\nvar label = \"Unconstrained\"\n\nvar plantedConcat = " + cypher + "\n"
		} else {
			source = "package planted\n\nconst planted = `" + cypher + "`\n"
		}
		if err := os.WriteFile(filepath.Join(root, "planted.go"), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		result := sweepSource(t, root, Labels())
		if len(result.Failures) == 0 && len(result.Dynamic) == 0 {
			t.Errorf("%s: the sweep did not admit %q", name, cypher)
		}
	}
}
