package collector

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestGitSourceEmitsTerraformStateCandidateFactsWithoutPersistingStateContent(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, "app.py"), "def handler():\n    return 1\n")
	stateBody := `{"version":4,"serial":7,"lineage":"00000000-0000-0000-0000-000000000000","resources":[]}`
	writeCollectorTestFile(t, filepath.Join(repoRoot, "terraform.tfstate"), stateBody)
	writeCollectorTestFile(t, filepath.Join(repoRoot, "env", "prod.tfstate"), stateBody)
	writeCollectorTestFile(t, filepath.Join(repoRoot, "env", "prod.tfstate.backup"), stateBody)

	source := &GitSource{
		Component: "collector-git",
		Selector: &stubRepositorySelector{
			batches: []SelectionBatch{{
				ObservedAt: time.Date(2026, time.May, 10, 14, 0, 0, 0, time.UTC),
				Repositories: []SelectedRepository{{
					RepoPath: repoRoot,
				}},
			}},
		},
		Snapshotter: NativeRepositorySnapshotter{},
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}

	envelopes := drainFactChannel(collected.Facts)
	candidateFacts := factsByKind(envelopes, "terraform_state_candidate")
	if got, want := len(candidateFacts), 2; got != want {
		t.Fatalf("terraform_state_candidate fact count = %d, want %d", got, want)
	}

	relativePaths := make([]string, 0, len(candidateFacts))
	for _, fact := range candidateFacts {
		if got, want := fact.CollectorKind, "git"; got != want {
			t.Fatalf("candidate CollectorKind = %q, want %q", got, want)
		}
		if got, want := fact.SourceRef.SourceSystem, "git"; got != want {
			t.Fatalf("candidate SourceRef.SourceSystem = %q, want %q", got, want)
		}
		if strings.Contains(fact.SourceRef.SourceURI, repoRoot) {
			t.Fatalf("candidate SourceRef.SourceURI = %q, want no absolute repo path", fact.SourceRef.SourceURI)
		}

		payload := fact.Payload
		if got, want := payload["candidate_source"], "git_local_file"; got != want {
			t.Fatalf("candidate_source = %#v, want %#v", got, want)
		}
		if got, want := payload["backend_kind"], "local"; got != want {
			t.Fatalf("backend_kind = %#v, want %#v", got, want)
		}
		repoID, ok := payload["repo_id"].(string)
		if !ok || repoID == "" {
			t.Fatalf("repo_id = %#v, want non-empty string", payload["repo_id"])
		}
		relativePath, ok := payload["relative_path"].(string)
		if !ok || relativePath == "" {
			t.Fatalf("relative_path = %#v, want non-empty string", payload["relative_path"])
		}
		if filepath.IsAbs(relativePath) || strings.Contains(relativePath, repoRoot) {
			t.Fatalf("relative_path = %q, want repo-relative path only", relativePath)
		}
		pathHash, ok := payload["path_hash"].(string)
		if !ok || pathHash == "" || strings.Contains(pathHash, relativePath) {
			t.Fatalf("path_hash = %#v, want opaque non-empty hash", payload["path_hash"])
		}
		if got, want := payload["file_size"], int64(len(stateBody)); got != want {
			t.Fatalf("file_size = %#v, want %#v", got, want)
		}
		warningFlags, ok := payload["warning_flags"].([]string)
		if !ok || !collectorStringSlicesEqual(warningFlags, []string{"state_in_vcs"}) {
			t.Fatalf("warning_flags = %#v, want [state_in_vcs]", payload["warning_flags"])
		}

		relativePaths = append(relativePaths, relativePath)
	}
	sort.Strings(relativePaths)
	if got, want := relativePaths, []string{"env/prod.tfstate", "terraform.tfstate"}; !collectorStringSlicesEqual(got, want) {
		t.Fatalf("candidate relative paths = %#v, want %#v", got, want)
	}

	for _, fact := range envelopes {
		switch fact.FactKind {
		case "file":
			if relativePath, _ := fact.Payload["relative_path"].(string); strings.HasSuffix(relativePath, ".tfstate") {
				t.Fatalf(".tfstate emitted as parsed file fact: %#v", fact.Payload)
			}
		case "content":
			if contentPath, _ := fact.Payload["content_path"].(string); strings.HasSuffix(contentPath, ".tfstate") {
				t.Fatalf(".tfstate emitted as content fact: %#v", fact.Payload)
			}
			if body, _ := fact.Payload["content_body"].(string); strings.Contains(body, `"serial":7`) {
				t.Fatal(".tfstate raw content leaked through content fact")
			}
		}
	}
}

func factsByKind(envelopes []facts.Envelope, kind string) []facts.Envelope {
	var matches []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			matches = append(matches, envelope)
		}
	}
	return matches
}
