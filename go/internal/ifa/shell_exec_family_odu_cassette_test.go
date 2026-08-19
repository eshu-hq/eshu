// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// TestShellExecFamilyCassetteMatchesGoOdu guards against drift between the
// hand-authored live-drive JSON cassette
// (testdata/cassettes/shellexec/ifa-shell-exec-family.json, driven by
// `ifa drive` under the live lanes once wired) and the in-memory Go Odù
// (shell_exec_family_odu.go, used by the pure vacuity guard): both describe
// the SAME fixture facts, authored twice for two different consumers. This
// test proves they actually agree by running the cassette's file facts
// through the SAME pure reducer.ExtractShellExecRows seam the Go Odù's own
// resolver test uses, and asserting the derived edge set is identical,
// mirroring TestSQLFamilyCassetteMatchesGoOdu.
func TestShellExecFamilyCassetteMatchesGoOdu(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	cassetteEnvelopes := loadShellExecCassetteEnvelopes(t, shellExecFamilyCassetteFullPath(repoRoot))
	oduEnvelopes := CatalogByName()[shellExecFamilyOduName].Facts
	if len(oduEnvelopes) == 0 {
		t.Fatalf("CatalogByName omits %q", shellExecFamilyOduName)
	}

	_, cassetteRows := reducer.ExtractShellExecRows(cassetteEnvelopes)
	_, oduRows := reducer.ExtractShellExecRows(oduEnvelopes)

	cassetteSet := shellExecEdgeSet(shellExecRowsToExpectedEdges(cassetteRows))
	oduSet := shellExecEdgeSet(shellExecRowsToExpectedEdges(oduRows))

	if len(cassetteSet) != len(oduSet) {
		t.Fatalf("cassette derives %d edges, Go Odù derives %d; the two fixture authorings have drifted", len(cassetteSet), len(oduSet))
	}
	for key := range oduSet {
		if _, ok := cassetteSet[key]; !ok {
			t.Errorf("edge %s present in the Go Odù but not the JSON cassette", key)
		}
	}
	for key := range cassetteSet {
		if _, ok := oduSet[key]; !ok {
			t.Errorf("edge %s present in the JSON cassette but not the Go Odù", key)
		}
	}
}

// shellExecEdgeSet builds a set keyed by ExpectedEdge.Key() so exact
// set-equality (not just subset) can be asserted, mirroring
// sqlRelationshipEdgeSet.
func shellExecEdgeSet(edges []ExpectedEdge) map[string]struct{} {
	out := make(map[string]struct{}, len(edges))
	for _, e := range edges {
		out[e.Key()] = struct{}{}
	}
	return out
}

func loadShellExecCassetteEnvelopes(t *testing.T, path string) []facts.Envelope {
	t.Helper()
	src, err := cassette.NewSource(path)
	if err != nil {
		t.Fatalf("cassette.NewSource(%s): %v", path, err)
	}
	var out []facts.Envelope
	for {
		gen, ok, err := src.Next(context.Background())
		if err != nil {
			t.Fatalf("cassette Next: %v", err)
		}
		if !ok {
			break
		}
		for env := range gen.Facts {
			out = append(out, env)
		}
	}
	return out
}
