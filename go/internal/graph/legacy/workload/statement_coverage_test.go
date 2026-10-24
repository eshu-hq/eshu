// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workload

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

// detectCallsite is the builder identity the differential capture records for
// the candidate-page read: the go-relative path:symbol of the direct caller.
const detectCallsite = "internal/graph/legacy/workload/detect.go:Detect"

// detectEmptyRecording replays the read the golden corpus captures: the hosted
// readiness probe runs Detect against a graph holding no name-only Workload
// node, so the candidate page comes back with zero rows. The statement text is
// the one Detect really sends, not a copy.
func detectEmptyRecording(t *testing.T) map[string][]backendconformance.DifferentialRecord {
	t.Helper()
	graph := &fakeGraph{}
	if _, err := Detect(context.Background(), graph, Options{}); err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(graph.calls) == 0 || graph.calls[0] != candidatePageCypher {
		t.Fatalf("Detect() first statement = %v, want the candidate page read", graph.calls)
	}
	return map[string][]backendconformance.DifferentialRecord{"neo4j": {{
		Fingerprint: backendconformance.DifferentialFingerprint{Statement: graph.calls[0], Parameters: "{}"},
		Backend:     "neo4j",
		RowCount:    0,
		Digest:      "empty-page",
		Kind:        backendconformance.DifferentialRecordRead,
		Callsite:    detectCallsite,
	}}}
}

func loadCoverageManifest(t *testing.T) queryplan.BuilderManifest {
	t.Helper()
	goDir, err := queryplan.GoDir()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := queryplan.LoadBuilderManifest(filepath.Join(goDir, "internal", "queryplan", "testdata", "statement-builders.yaml"))
	if err != nil {
		t.Fatalf("LoadBuilderManifest() error = %v", err)
	}
	return manifest
}

// TestDetectEmptyCandidatePageIsExemptedInStatementCoverage (#8795) pins the
// statement-coverage verdict for the legacy-workload candidate read. The golden
// corpus holds no name-only Workload node, so the read is empty on every
// execution; the committed manifest must excuse it by callsite and say so.
// Without the exemption the coverage gate rejects the read as always-empty, and
// the exemption must also stay live (not stale) while the read is recorded.
func TestDetectEmptyCandidatePageIsExemptedInStatementCoverage(t *testing.T) {
	records := detectEmptyRecording(t)
	manifest := loadCoverageManifest(t)

	exempted := backendconformance.ComputeStatementCoverage(manifest, records)
	if got := exempted.ByBackend["neo4j"].AlwaysEmptyReads; len(got) != 0 {
		t.Fatalf("always-empty reads with the committed manifest = %v, want none", got)
	}
	// The recording holds only this read, so every other manifest exemption is
	// reported stale here; only the detect.go entry matters to this test.
	for _, stale := range exempted.StaleExemptions {
		if strings.HasPrefix(stale, detectCallsite+" ::") {
			t.Fatalf("detect.go exemption reported stale: %s; its anchor must keep matching the recorded read", stale)
		}
	}

	// Seeded violation: the same recording against a manifest without the
	// detect.go exemption must fail as an always-empty read, so the pass above
	// is the exemption's doing and not a vacuous check.
	stripped := manifest
	stripped.ReadExemptions = slices.DeleteFunc(slices.Clone(manifest.ReadExemptions), func(e queryplan.ReadExemption) bool {
		return e.Callsite == queryplan.CanonicalCallsite(detectCallsite)
	})
	if len(stripped.ReadExemptions) != len(manifest.ReadExemptions)-1 {
		t.Fatalf("committed manifest holds %d detect.go exemptions, want exactly 1", len(manifest.ReadExemptions)-len(stripped.ReadExemptions))
	}
	violated := backendconformance.ComputeStatementCoverage(stripped, records)
	got := violated.ByBackend["neo4j"].AlwaysEmptyReads
	if len(got) != 1 || !strings.Contains(got[0], detectCallsite) {
		t.Fatalf("always-empty reads without the exemption = %v, want exactly the detect.go candidate read", got)
	}
}
