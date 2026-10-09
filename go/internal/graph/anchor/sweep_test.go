// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// sweepResult is the outcome of the static sweep over a source tree.
type sweepResult struct {
	// Writes is the number of id writes CheckWriters saw in static-label sites.
	Writes int
	// Failures has one line per uncovered id write in a static-label site.
	Failures []string
	// Dynamic lists the template sites whose node label is a placeholder.
	Dynamic []cypherSite
}

// sweepSource runs CheckWriters over every static-label Cypher literal under
// root and collects the dynamic-label template sites for the allowlist check.
func sweepSource(t *testing.T, root string, labels map[string]bool) sweepResult {
	t.Helper()
	var result sweepResult
	for _, site := range productionCypherSites(t, root) {
		if site.DynamicLabel {
			result.Dynamic = append(result.Dynamic, site)
			continue
		}
		report := CheckWriters([]Statement{{Text: site.Text, Callsite: site.Key}}, labels)
		result.Writes += report.IDWrites
		for _, finding := range report.Findings {
			result.Failures = append(result.Failures, site.Key+" "+finding.Kind+" on "+finding.Variable+" labels="+strings.Join(finding.Labels, ":"))
		}
	}
	sort.Strings(result.Failures)
	sort.Slice(result.Dynamic, func(i, j int) bool { return result.Dynamic[i].Key < result.Dynamic[j].Key })
	return result
}

// TestEveryProductionIDWriterNamesAnAnchorLabel is the static half of the
// writer-coverage gate (#7212): an independent sweep over the Cypher literals
// in go/ that does not depend on what a replay happens to execute. A write that
// sets an id on a label outside UIDLabels and IDLabels would leave an
// id-bearing node the labeled entity-context anchor cannot reach.
//
// Static-label sites are decided here. A statement whose label is built at run
// time ("MERGE (n:" + label + ...) or a fmt template (`(n:%s`) is a dynamic-label
// site: TestEveryDynamicLabelWriterIsNamed requires a named allowlist row for
// each, and the replay half of the gate covers what the corpus executes.
func TestEveryProductionIDWriterNamesAnAnchorLabel(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("go module root %s: %v", root, err)
	}
	result := sweepSource(t, root, Labels())
	if result.Writes < 20 {
		t.Fatalf("static sweep saw %d id writes; the sweep is not reading go/", result.Writes)
	}
	if len(result.Failures) > 0 {
		t.Fatalf("%d id write(s) outside the anchor label set:\n%s", len(result.Failures), strings.Join(result.Failures, "\n"))
	}
}

// TestStaticSweepFailsOnAPlantedUnconstrainedIDWriter is the seeded violation:
// a Go file under a scratch root plants `MERGE (n:Unconstrained {id: ...})`
// and the sweep must name it. The same scratch root with the planted label
// covered passes, so the failure is the label and not the harness.
func TestStaticSweepFailsOnAPlantedUnconstrainedIDWriter(t *testing.T) {
	root := t.TempDir()
	source := "package planted\n\nconst plantedCypher = `MERGE (n:Unconstrained {id: $entity_id}) SET n.name = $name`\n"
	if err := os.WriteFile(filepath.Join(root, "planted.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	failures := sweepSource(t, root, Labels()).Failures
	if len(failures) != 1 || !strings.HasPrefix(failures[0], "planted.go:3 map_key on n labels=Unconstrained") {
		t.Fatalf("failures = %v, want exactly the planted writer at planted.go:3", failures)
	}
	covered := Labels()
	covered["Unconstrained"] = true
	if failures := sweepSource(t, root, covered).Failures; len(failures) != 0 {
		t.Fatalf("failures with the planted label covered = %v, want none", failures)
	}
}

// TestSweepSeesTheShapesItWasTaught plants the shapes the sweep gained in
// review: a static writer assembled from named constants only, a positional-verb
// label, and a parameter property map. Each must reach the analyzer.
func TestSweepSeesTheShapesItWasTaught(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		wantFailure bool
		wantDynamic bool
	}{
		{
			name: "const-only chain",
			source: "package planted\n\nconst head = \"MERGE (n:Unconstrained {uid: $u}) \"\nconst tail = \"SET n.id = $u\"\n\n" +
				"var statement = head + tail\n",
			wantFailure: true,
		},
		{
			name:        "positional verb label",
			source:      "package planted\n\nconst planted = `MERGE (n:%[1]s {uid: $u}) SET n.id = $u`\n",
			wantDynamic: true,
		},
		{
			name:        "Cypher 5 IS label form",
			source:      "package planted\n\nconst planted = `CREATE (n IS Unconstrained {id: $id})`\n",
			wantFailure: true,
		},
		{
			name:        "label removal that strands the node",
			source:      "package planted\n\nconst planted = `MATCH (n:Function {uid: $u}) REMOVE n:Function`\n",
			wantFailure: true,
		},
		{
			name:        "parameter property map",
			source:      "package planted\n\nconst planted = `CREATE (n:Unconstrained $props)`\n",
			wantFailure: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := sweepSource(t, plantedTree(t, tc.source, ""), Labels())
			if tc.wantFailure && len(result.Failures) == 0 {
				t.Errorf("no failure for %q", tc.source)
			}
			if tc.wantDynamic && len(result.Dynamic) != 1 {
				t.Errorf("dynamic sites = %d for %q, want 1", len(result.Dynamic), tc.source)
			}
		})
	}
}
