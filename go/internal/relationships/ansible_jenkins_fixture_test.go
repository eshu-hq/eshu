package relationships

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestAnsibleJenkinsFixtureEmitsControllerRepoEvidence(t *testing.T) {
	t.Parallel()

	jenkinsfile := readAnsibleJenkinsFixture(t, "Jenkinsfile")
	evidence := DiscoverEvidence(
		[]facts.Envelope{
			{
				ScopeID: "controller-service",
				Payload: map[string]any{
					"artifact_type": "groovy",
					"relative_path": "Jenkinsfile",
					"content":       jenkinsfile,
					"parsed_file_data": parser.ExtractGroovyPipelineMetadata(
						jenkinsfile,
					),
				},
			},
			{
				ScopeID: "controller-service",
				Payload: map[string]any{
					"artifact_type": "ansible_playbook",
					"relative_path": "deploy.yml",
					"content":       readAnsibleJenkinsFixture(t, "deploy.yml"),
				},
			},
		},
		[]CatalogEntry{
			{RepoID: "controller-pipelines", Aliases: []string{"controller-pipelines"}},
			{RepoID: "automation-configs", Aliases: []string{"automation-configs"}},
			{RepoID: "portal-websites", Aliases: []string{"portal-websites"}},
		},
	)

	assertFixtureEvidence(t, evidence, EvidenceKindJenkinsSharedLibrary, "controller-pipelines", RelDiscoversConfigIn)
	assertFixtureEvidence(t, evidence, EvidenceKindJenkinsGitHubRepository, "automation-configs", RelDiscoversConfigIn)
	assertFixtureEvidence(t, evidence, EvidenceKindAnsibleRoleReference, "portal-websites", RelDependsOn)
}

func assertFixtureEvidence(
	t *testing.T,
	evidence []EvidenceFact,
	kind EvidenceKind,
	targetRepoID string,
	relationshipType RelationshipType,
) {
	t.Helper()

	for _, fact := range evidence {
		if fact.EvidenceKind != kind {
			continue
		}
		if fact.TargetRepoID != targetRepoID {
			continue
		}
		if fact.RelationshipType != relationshipType {
			t.Fatalf(
				"%s relationship type = %q, want %q",
				kind, fact.RelationshipType, relationshipType,
			)
		}
		return
	}

	t.Fatalf("missing %s evidence targeting %q", kind, targetRepoID)
}

func readAnsibleJenkinsFixture(t *testing.T, parts ...string) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}

	allParts := append(
		[]string{filepath.Dir(file), "..", "..", "..", "tests", "fixtures", "ecosystems", "ansible_jenkins_automation"},
		parts...,
	)
	body, err := os.ReadFile(filepath.Join(allParts...))
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", filepath.Join(allParts...), err)
	}
	return string(body)
}
