package ghactionsruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestNewClaimedSourceRejectsUnboundedTargets(t *testing.T) {
	t.Parallel()

	_, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              fakeClient{},
		Targets: []TargetConfig{{
			ScopeID:             "ci-cd:github-actions:example/repo",
			Repository:          "example/repo",
			Token:               "token",
			AllowedRepositories: []string{"example/repo"},
			MaxRuns:             maxRunPages + 1,
			MaxJobs:             10,
			MaxArtifacts:        10,
		}},
	})
	if err == nil {
		t.Fatal("NewClaimedSource() error = nil, want max_runs rejection")
	}
}

func TestClaimedSourceCollectsGitHubActionsRunAndArtifacts(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 7, 15, 0, 0, 0, time.UTC)
	client := fakeClient{snapshot: RunSnapshot{
		Workflow: map[string]any{
			"id":    42,
			"name":  "Publish",
			"path":  ".github/workflows/publish.yml",
			"state": "active",
		},
		Run: map[string]any{
			"id":             1001,
			"run_attempt":    2,
			"run_number":     88,
			"name":           "Publish",
			"event":          "push",
			"status":         "completed",
			"conclusion":     "success",
			"head_branch":    "main",
			"head_sha":       "0123456789abcdef0123456789abcdef01234567",
			"run_started_at": "2026-06-07T14:59:00Z",
			"updated_at":     "2026-06-07T15:00:00Z",
			"html_url":       "https://github.com/example/repo/actions/runs/1001",
			"repository": map[string]any{
				"full_name": "example/repo",
				"html_url":  "https://github.com/example/repo",
			},
			"actor": map[string]any{"login": "builder"},
		},
		Artifacts: []map[string]any{{
			"id":                   501,
			"name":                 "image-digest",
			"size_in_bytes":        128,
			"digest":               "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"archive_download_url": "https://api.github.com/repos/example/repo/actions/artifacts/501/zip?token=secret",
			"created_at":           "2026-06-07T15:00:01Z",
			"expires_at":           "2026-06-14T15:00:01Z",
			"workflow_run": map[string]any{
				"id":       1001,
				"head_sha": "0123456789abcdef0123456789abcdef01234567",
			},
		}},
	}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              client,
		Now:                 func() time.Time { return observedAt },
		Targets: []TargetConfig{{
			ScopeID:             "ci-cd:github-actions:example/repo",
			Repository:          "example/repo",
			Token:               "token",
			AllowedRepositories: []string{"example/repo"},
			SourceURI:           "https://github.com/example/repo",
			MaxRuns:             1,
			MaxJobs:             10,
			MaxArtifacts:        10,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             "ci-cd:github-actions:example/repo",
		GenerationID:        "generation-1",
		CurrentFencingToken: 7,
	})
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got, want := collected.Scope.CollectorKind, scope.CollectorCICDRun; got != want {
		t.Fatalf("Scope.CollectorKind = %q, want %q", got, want)
	}
	if got, want := collected.Generation.ScopeID, "ci-cd:github-actions:example/repo"; got != want {
		t.Fatalf("Generation.ScopeID = %q, want %q", got, want)
	}

	envelopes := drainFacts(t, collected.Facts)
	requireFactKind(t, envelopes, facts.CICDRunFactKind)
	artifact := requireFactKind(t, envelopes, facts.CICDArtifactFactKind)
	if got, want := artifact.Payload["artifact_digest"], "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"; got != want {
		t.Fatalf("artifact_digest = %#v, want %#v", got, want)
	}
	if got, want := artifact.Payload["download_url"], "https://api.github.com/repos/example/repo/actions/artifacts/501/zip"; got != want {
		t.Fatalf("download_url = %#v, want stripped URL %#v", got, want)
	}
}

func TestClaimedSourceClassifiesProviderErrors(t *testing.T) {
	t.Parallel()

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              fakeClient{err: ErrRateLimited},
		Targets: []TargetConfig{{
			ScopeID:             "ci-cd:github-actions:example/repo",
			Repository:          "example/repo",
			Token:               "token",
			AllowedRepositories: []string{"example/repo"},
			MaxRuns:             1,
			MaxJobs:             10,
			MaxArtifacts:        10,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}
	_, _, err = source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "ci-cd-primary",
		ScopeID:             "ci-cd:github-actions:example/repo",
		GenerationID:        "generation-1",
		CurrentFencingToken: 7,
	})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("NextClaimed() error = %v, want ErrRateLimited", err)
	}
}

type fakeClient struct {
	snapshot RunSnapshot
	err      error
}

func (f fakeClient) FetchLatestRun(context.Context, TargetConfig) (RunSnapshot, error) {
	return f.snapshot, f.err
}

func drainFacts(t *testing.T, ch <-chan facts.Envelope) []facts.Envelope {
	t.Helper()
	var out []facts.Envelope
	for envelope := range ch {
		out = append(out, envelope)
	}
	return out
}

func requireFactKind(t *testing.T, envelopes []facts.Envelope, factKind string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind == factKind {
			return envelope
		}
	}
	t.Fatalf("missing fact kind %q in %#v", factKind, envelopes)
	return facts.Envelope{}
}
