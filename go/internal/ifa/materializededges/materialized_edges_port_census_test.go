// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

var unclassifiedPortPrefixes = []string{"Retract", "Sweep", "Execute"}

// TestPortClassificationResidueIsIntentional bounds the deliberately
// one-directional direct-edge classification. Ports that are neither direct
// edge writers nor declared node-only writers must be command-shaped, or one
// of the two explicit cross-cutting exceptions pinned below.
func TestPortClassificationResidueIsIntentional(t *testing.T) {
	t.Parallel()

	src, ports := parseCypherPackageWithReducerPorts(t, filepath.Join(repoRootDir(t), "go"))
	classified := classifyCypherPorts(src, ports)
	if len(classified) == 0 {
		t.Fatal("no reducer interface port resolved to a cypher implementation; the production typed scan is vacuous")
	}

	nodeOnly := setOf(reducer.DirectMaterializedEdgeNodeOnlyWritePorts())
	var residue []string
	for _, row := range classified {
		if _, isEdge := reducer.DirectMaterializedEdgeFamilyForPort(row.Port); isEdge {
			continue
		}
		if _, isNode := nodeOnly[row.Port]; isNode {
			continue
		}
		if hasAnyPrefix(row.Port, unclassifiedPortPrefixes) {
			continue
		}
		residue = append(residue, row.Port)
	}
	sort.Strings(residue)

	want := []string{
		"HasCanonicalCodeTargets",
		reducer.SharedProjectionEdgeWritePort(),
	}
	sort.Strings(want)
	if strings.Join(residue, ",") != strings.Join(want, ",") {
		t.Fatalf("unclassified non-command ports = %v, want %v; classify a new graph port explicitly or justify its residue", residue, want)
	}
}

func hasAnyPrefix(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
