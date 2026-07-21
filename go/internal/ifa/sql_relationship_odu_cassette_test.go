// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// TestSQLFamilyCassetteMatchesGoOdu guards against drift between the
// hand-authored live-drive JSON cassette
// (testdata/cassettes/sqlrelationships/ifa-sql-family.json, driven by
// `ifa drive` under the P2/P4 live lanes) and the in-memory Go Odù
// (sql_relationship_odu.go, used by the pure vacuity guard): both describe
// the SAME fixture facts, authored twice for two different consumers. This
// test proves they actually agree by running the cassette's content_entity
// and file facts through the SAME pure reducer.ExtractSQLRelationshipRows
// seam the Go Odù's own lockstep test uses, and asserting the derived edge
// set is identical.
func TestSQLFamilyCassetteMatchesGoOdu(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	cassetteEnvelopes := loadCassetteEnvelopes(t, filepath.Join(repoRoot, "testdata", "cassettes", "sqlrelationships", "ifa-sql-family.json"))
	oduEnvelopes := CatalogByName()[sqlFamilyOduName].Facts

	_, cassetteRows, _ := reducer.ExtractSQLRelationshipRows(cassetteEnvelopes)
	_, oduRows, _ := reducer.ExtractSQLRelationshipRows(oduEnvelopes)

	cassetteSet := sqlRelationshipEdgeSet(sqlRelationshipRowsToExpectedEdges(cassetteRows))
	oduSet := sqlRelationshipEdgeSet(sqlRelationshipRowsToExpectedEdges(oduRows))

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

// TestSQLFamilyDeltaCassetteMatchesGoOdu is the gen-2 delta counterpart.
func TestSQLFamilyDeltaCassetteMatchesGoOdu(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	cassetteEnvelopes := loadCassetteEnvelopes(t, filepath.Join(repoRoot, "testdata", "cassettes", "sqlrelationships", "ifa-sql-family-delta.json"))
	oduEnvelopes := CatalogByName()[sqlFamilyDeltaOduName].Facts

	_, cassetteRows, _ := reducer.ExtractSQLRelationshipRows(cassetteEnvelopes)
	_, oduRows, _ := reducer.ExtractSQLRelationshipRows(oduEnvelopes)

	cassetteSet := sqlRelationshipEdgeSet(sqlRelationshipRowsToExpectedEdges(cassetteRows))
	oduSet := sqlRelationshipEdgeSet(sqlRelationshipRowsToExpectedEdges(oduRows))

	if len(cassetteSet) != len(oduSet) {
		t.Fatalf("delta cassette derives %d edges, delta Go Odù derives %d; the two fixture authorings have drifted", len(cassetteSet), len(oduSet))
	}
	for key := range oduSet {
		if _, ok := cassetteSet[key]; !ok {
			t.Errorf("edge %s present in the delta Go Odù but not the delta JSON cassette", key)
		}
	}
	for key := range cassetteSet {
		if _, ok := oduSet[key]; !ok {
			t.Errorf("edge %s present in the delta JSON cassette but not the delta Go Odù", key)
		}
	}
}

func loadCassetteEnvelopes(t *testing.T, path string) []facts.Envelope {
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
