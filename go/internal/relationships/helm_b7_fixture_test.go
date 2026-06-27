// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestHelmUmbrellaChartB7FixtureResolves guards the B-7 corpus fixture
// tests/fixtures/ecosystems/helm-umbrella-chart against drift. It reads the
// committed Chart.yaml, runs the same discovery+resolution the pipeline runs,
// and asserts the Helm subchart dependency repository resolves to the in-corpus
// deployable-source repository as a DEPLOYS_FROM edge carrying
// HELM_CHART_REFERENCE evidence. The golden-corpus gate's rc-34 asserts the same
// edge on the real graph backend, filtered by that evidence kind; this test is
// the fast, Docker-free proof that the fixture content produces the edge the
// gate requires.
func TestHelmUmbrellaChartB7FixtureResolves(t *testing.T) {
	t.Parallel()

	chartPath := filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems",
		"helm-umbrella-chart", "Chart.yaml")
	content, err := os.ReadFile(chartPath)
	if err != nil {
		t.Fatalf("read B-7 helm fixture: %v", err)
	}

	envelopes := []facts.Envelope{
		{
			ScopeID: "helm-umbrella-chart",
			Payload: map[string]any{
				"relative_path": "Chart.yaml",
				"content":       string(content),
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "deployable-source", Aliases: []string{"deployable-source"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	var helmEvidence *EvidenceFact
	for i := range evidence {
		if evidence[i].EvidenceKind == EvidenceKindHelmChart &&
			evidence[i].TargetRepoID == "deployable-source" {
			helmEvidence = &evidence[i]
			break
		}
	}
	if helmEvidence == nil {
		t.Fatalf("fixture produced no HELM_CHART_REFERENCE evidence resolving to deployable-source; got %+v", evidence)
	}
	if helmEvidence.RelationshipType != RelDeploysFrom {
		t.Fatalf("evidence relationship = %q, want %q", helmEvidence.RelationshipType, RelDeploysFrom)
	}

	_, resolved := Resolve(evidence, nil, 0)
	found := false
	for _, r := range resolved {
		if r.RelationshipType == RelDeploysFrom &&
			r.SourceRepoID == "helm-umbrella-chart" &&
			r.TargetRepoID == "deployable-source" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("fixture did not resolve a DEPLOYS_FROM helm-umbrella-chart -> deployable-source; resolved=%+v", resolved)
	}
}
