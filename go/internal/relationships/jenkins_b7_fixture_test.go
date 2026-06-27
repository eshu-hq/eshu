// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestJenkinsCIPipelinesB7FixtureResolves guards the B-7 corpus fixture
// tests/fixtures/ecosystems/jenkins-ci-pipelines against drift. It reads the
// committed Jenkinsfile, runs the same discovery+resolution the pipeline runs,
// and asserts the explicit GitHub checkout URL resolves to the in-corpus
// deployable-source repository as a DISCOVERS_CONFIG_IN edge carrying
// JENKINS_GITHUB_REPOSITORY evidence. The golden-corpus gate asserts the same
// edge on the real graph backend, filtered by that evidence kind; this test is
// the fast, Docker-free proof that the fixture content produces the edge the
// gate requires.
func TestJenkinsCIPipelinesB7FixtureResolves(t *testing.T) {
	t.Parallel()

	jenkinsfilePath := filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems",
		"jenkins-ci-pipelines", "Jenkinsfile")
	content, err := os.ReadFile(jenkinsfilePath)
	if err != nil {
		t.Fatalf("read B-7 jenkins fixture: %v", err)
	}

	envelopes := []facts.Envelope{
		{
			ScopeID: "jenkins-ci-pipelines",
			Payload: map[string]any{
				"relative_path": "Jenkinsfile",
				"content":       string(content),
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "deployable-source", Aliases: []string{"deployable-source"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	var jenkinsEvidence *EvidenceFact
	for i := range evidence {
		if evidence[i].EvidenceKind == EvidenceKindJenkinsGitHubRepository &&
			evidence[i].TargetRepoID == "deployable-source" {
			jenkinsEvidence = &evidence[i]
			break
		}
	}
	if jenkinsEvidence == nil {
		t.Fatalf("fixture produced no JENKINS_GITHUB_REPOSITORY evidence resolving to deployable-source; got %+v", evidence)
	}
	if jenkinsEvidence.RelationshipType != RelDiscoversConfigIn {
		t.Fatalf("evidence relationship = %q, want %q", jenkinsEvidence.RelationshipType, RelDiscoversConfigIn)
	}

	_, resolved := Resolve(evidence, nil, 0)
	found := false
	for _, r := range resolved {
		if r.RelationshipType == RelDiscoversConfigIn &&
			r.SourceRepoID == "jenkins-ci-pipelines" &&
			r.TargetRepoID == "deployable-source" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("fixture did not resolve a DISCOVERS_CONFIG_IN jenkins-ci-pipelines -> deployable-source; resolved=%+v", resolved)
	}
}
