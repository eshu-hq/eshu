// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package conformance

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/schema"
)

// starterCassette and starterSpec are the committed starter artifacts a
// contributor regenerates and runs the suite against.
const (
	starterCassette = "testdata/starter-cassette.json"
	starterSpec     = "testdata/starter-spec.yaml"
)

// TestConformance is the headline check a contributor runs from their own clone:
//
//	go test ./conformance -count=1
//
// It replays the starter cassette offline (zero provider credentials, zero
// Docker), derives the projected node/edge/correlation observation in memory,
// and asserts it against the starter spec using the SAME goldengate.Evaluate*
// logic the in-repo B-7 gate uses. A green run is the credential-free
// deterministic proof that the collector extraction is correct.
func TestConformance(t *testing.T) {
	report, err := Run(starterCassette, starterSpec)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Findings) == 0 {
		t.Fatal("conformance produced no findings: an empty report proves nothing")
	}
	if report.Failed() {
		var buf bytes.Buffer
		report.Write(&buf)
		t.Fatalf("conformance failed:\n%s", buf.String())
	}
}

// TestObserveDerivesStarterCounts locks the offline observation: the starter
// tape must project exactly one repository, two directories, three files, five
// CONTAINS edges, one repository->directory top-level correlation, and two
// language-carrying files.
func TestObserveDerivesStarterCounts(t *testing.T) {
	envs := replayStarterFacts(t)
	obs, err := Observe(envs)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}

	wantNodes := map[string]int64{"Repository": 1, "Directory": 2, "File": 3}
	for label, want := range wantNodes {
		if got := obs.NodeCounts[label]; got != want {
			t.Errorf("node %s = %d, want %d", label, got, want)
		}
	}
	if got := obs.EdgeCounts["CONTAINS"]; got != 5 {
		t.Errorf("CONTAINS edges = %d, want 5", got)
	}
	if got := obs.CorrelationCount("Repository", "CONTAINS", "Directory"); got != 1 {
		t.Errorf("Repository-CONTAINS-Directory (top level) = %d, want 1", got)
	}
	langs := obs.NodeProperty("File", "language")
	carriers := 0
	for _, v := range langs {
		if v != "" {
			carriers++
		}
	}
	if carriers != 2 {
		t.Errorf("language-carrying files = %d, want 2 (values %v)", carriers, langs)
	}
}

// TestEvaluateFailsWhenADirectoryIsDropped proves the shared assertions actually
// bite: an observation that lost a nested directory (the #4019 class of
// projection drop) must fail the required Directory count floor. A conformance
// suite whose assertions cannot fail would be false-green.
func TestEvaluateFailsWhenADirectoryIsDropped(t *testing.T) {
	snap, err := LoadSpec(starterSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}

	// Healthy observation passes.
	healthy := Observation{
		NodeCounts: map[string]int64{"Repository": 1, "Directory": 2, "File": 3},
		EdgeCounts: map[string]int64{"CONTAINS": 5},
		Correlations: map[string]int64{
			correlationKey("Repository", "CONTAINS", "Directory"): 1,
		},
		NodeProps: map[string]map[string][]string{
			"File": {"language": {"", "go", "go"}},
		},
	}
	if r := Evaluate(healthy, snap); r.Failed() {
		var buf bytes.Buffer
		r.Write(&buf)
		t.Fatalf("healthy observation unexpectedly failed:\n%s", buf.String())
	}

	// Drop one directory (and its CONTAINS edge): the Directory floor [2,2] and
	// the CONTAINS floor [5,5] must now fail.
	dropped := healthy
	dropped.NodeCounts = map[string]int64{"Repository": 1, "Directory": 1, "File": 3}
	dropped.EdgeCounts = map[string]int64{"CONTAINS": 4}
	r := Evaluate(dropped, snap)
	if !r.Failed() {
		var buf bytes.Buffer
		r.Write(&buf)
		t.Fatalf("dropped-directory observation should fail the gate but passed:\n%s", buf.String())
	}
}

// TestObserveRejectsMalformedFact proves a cassette fact missing a required
// payload field fails loudly instead of projecting a silently-empty graph that
// would look green.
func TestObserveRejectsMalformedFact(t *testing.T) {
	envs := []facts.Envelope{
		{FactKind: factKindRepository, Payload: map[string]any{"repo_id": "x", "name": "x", "path": "/x"}},
		// directory missing parent_path
		{FactKind: factKindDirectory, Payload: map[string]any{"path": "/x/src", "name": "src", "repo_id": "x", "depth": 0}},
	}
	if _, err := Observe(envs); err == nil {
		t.Fatal("Observe accepted a directory fact with no parent_path")
	}
}

// TestStarterCassetteIsSchemaValid proves the committed starter tape validates
// against the R-3 cassette JSON Schema (the offline author-time validator), so
// the documented `validate` step in the README cannot silently rot.
func TestStarterCassetteIsSchemaValid(t *testing.T) {
	data, err := os.ReadFile(filepath.Clean(starterCassette))
	if err != nil {
		t.Fatalf("read starter cassette: %v", err)
	}
	if err := schema.ValidateCassetteBytes(starterCassette, data); err != nil {
		t.Fatalf("starter cassette is not schema-valid: %v", err)
	}
}

// TestRunIsDeterministic proves two replays of the same tape produce the same
// pass/fail outcome and finding count — the determinism the framework promises.
func TestRunIsDeterministic(t *testing.T) {
	first, err := Run(starterCassette, starterSpec)
	if err != nil {
		t.Fatalf("Run #1: %v", err)
	}
	second, err := Run(starterCassette, starterSpec)
	if err != nil {
		t.Fatalf("Run #2: %v", err)
	}
	if first.Failed() != second.Failed() || len(first.Findings) != len(second.Findings) {
		t.Fatalf("non-deterministic: run1 failed=%v findings=%d, run2 failed=%v findings=%d",
			first.Failed(), len(first.Findings), second.Failed(), len(second.Findings))
	}
}

// replayStarterFacts drains the starter cassette into the flat envelope slice the
// observation consumes, using the shared cassette replay Source.
func replayStarterFacts(t *testing.T) []facts.Envelope {
	t.Helper()
	envs, err := replayFacts(starterCassette)
	if err != nil {
		t.Fatalf("replayFacts: %v", err)
	}
	return envs
}
