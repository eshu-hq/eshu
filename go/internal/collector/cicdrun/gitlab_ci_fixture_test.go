// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestGitLabCIFixtureBuildsReducerConsumableFacts(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/gitlab_ci_success.json")
	observedAt := time.Date(2026, 6, 1, 10, 30, 0, 0, time.UTC)
	envelopes, err := GitLabCIFixtureEnvelopes(raw, FixtureContext{
		ScopeID:             "gitlab-ci://gitlab.com/eshu-hq/gitlab-demo-service",
		GenerationID:        "4200:1",
		CollectorInstanceID: "fixture-gitlab-ci",
		FencingToken:        11,
		ObservedAt:          observedAt,
		SourceURI:           "https://gitlab.com/api/v4/projects/501/pipelines/4200",
	})
	if err != nil {
		t.Fatalf("GitLabCIFixtureEnvelopes() error = %v", err)
	}

	byKind := envelopesByKind(envelopes)
	// No ci.pipeline_definition: GitLab's pipeline API exposes no stable
	// workflow-definition ID distinct from the pipeline itself (see
	// gitlab_ci_fixture.go doc comment). No ci.step: GitLab's jobs API has no
	// step-level breakdown. No ci.trigger_edge / ci.environment_observation:
	// out of v1 scope, matching ghactionsruntime's own live client, which also
	// does not populate RunSnapshot.Triggers or job.environment today.
	assertKindCount(t, byKind, facts.CICDRunFactKind, 1)
	assertKindCount(t, byKind, facts.CICDJobFactKind, 2)
	assertKindCount(t, byKind, facts.CICDArtifactFactKind, 1)
	assertKindCount(t, byKind, facts.CICDWarningFactKind, 1)

	run := byKind[facts.CICDRunFactKind][0]
	assertCICDEnvelope(t, run, facts.CICDRunFactKind, observedAt)
	assertPayload(t, run.Payload, "provider", string(ProviderGitLabCI))
	assertPayload(t, run.Payload, "run_id", "4200")
	assertPayload(t, run.Payload, "run_attempt", "1")
	assertPayload(t, run.Payload, "run_number", "12")
	assertPayload(t, run.Payload, "event", "push")
	assertPayload(t, run.Payload, "status", "success")
	assertPayload(t, run.Payload, "result", "success")
	assertPayload(t, run.Payload, "branch", "main")
	assertPayload(t, run.Payload, "commit_sha", "9f8e7d6c5b4a39281706f5e4d3c2b1a0f9e8d7c6")
	assertPayload(t, run.Payload, "provider_repository_id", "gitlab.com/eshu-hq/gitlab-demo-service")
	assertPayload(t, run.Payload, "actor", "linuxdynasty")
	if run.Payload["repository_id"] == "" {
		t.Fatalf("repository_id must not be blank: %#v", run.Payload)
	}

	artifact := byKind[facts.CICDArtifactFactKind][0]
	assertPayload(t, artifact.Payload, "artifact_id", "55001:build-artifacts.zip")
	assertPayload(t, artifact.Payload, "artifact_name", "build-artifacts.zip")
	assertPayload(t, artifact.Payload, "artifact_type", "archive")
	assertPayload(t, artifact.Payload, "artifact_digest", "")

	warning := byKind[facts.CICDWarningFactKind][0]
	assertPayload(t, warning.Payload, "reason", "artifact_missing_digest")

	for _, envelope := range envelopes {
		if envelope.CollectorKind != CollectorKind {
			t.Fatalf("CollectorKind = %q, want %q", envelope.CollectorKind, CollectorKind)
		}
		if envelope.SourceConfidence != facts.SourceConfidenceReported {
			t.Fatalf("SourceConfidence = %q, want reported", envelope.SourceConfidence)
		}
		if envelope.FencingToken != 11 {
			t.Fatalf("FencingToken = %d, want 11", envelope.FencingToken)
		}
		if envelope.StableFactKey == "" || envelope.FactID == "" {
			t.Fatalf("fact identifiers must not be blank: %#v", envelope)
		}
	}
}

func TestGitLabCIFixtureEmitsPartialWarnings(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/gitlab_ci_partial.json")
	envelopes, err := GitLabCIFixtureEnvelopes(raw, FixtureContext{
		ScopeID:             "gitlab-ci://gitlab.com/eshu-hq/gitlab-demo-service",
		GenerationID:        "4300:1",
		CollectorInstanceID: "fixture-gitlab-ci",
		ObservedAt:          time.Date(2026, 6, 2, 10, 10, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("GitLabCIFixtureEnvelopes() error = %v", err)
	}

	byKind := envelopesByKind(envelopes)
	assertKindCount(t, byKind, facts.CICDRunFactKind, 1)
	assertKindCount(t, byKind, facts.CICDJobFactKind, 1)
	assertKindCount(t, byKind, facts.CICDWarningFactKind, 1)

	warning := byKind[facts.CICDWarningFactKind][0]
	assertPayload(t, warning.Payload, "reason", "partial_jobs_payload")
	assertPayload(t, warning.Payload, "partial_generation", true)
}

func TestGitLabCIFixtureWarnsWhenRunAnchorsMissing(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"pipeline": {"id": 900}}`)
	envelopes, err := GitLabCIFixtureEnvelopes(raw, FixtureContext{
		ScopeID:             "gitlab-ci://gitlab.com/eshu-hq/gitlab-demo-service",
		GenerationID:        "900:1",
		CollectorInstanceID: "fixture-gitlab-ci",
	})
	if err != nil {
		t.Fatalf("GitLabCIFixtureEnvelopes() error = %v", err)
	}

	byKind := envelopesByKind(envelopes)
	assertKindCount(t, byKind, facts.CICDRunFactKind, 1)
	assertKindCount(t, byKind, facts.CICDWarningFactKind, 1)
	assertPayload(t, byKind[facts.CICDWarningFactKind][0].Payload, "reason", "run_missing_repository_or_commit")
}

func TestGitLabCIFixtureRejectsBlankPipelineID(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"pipeline": {}}`)
	_, err := GitLabCIFixtureEnvelopes(raw, FixtureContext{
		ScopeID:             "gitlab-ci://gitlab.com/eshu-hq/gitlab-demo-service",
		GenerationID:        "0:1",
		CollectorInstanceID: "fixture-gitlab-ci",
	})
	if err == nil {
		t.Fatalf("GitLabCIFixtureEnvelopes() error = nil, want blank pipeline.id rejected")
	}
}

func TestGitLabCIFixtureWarnsAndSkipsJobsMissingID(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"pipeline": {
			"id": 4201,
			"ref": "main",
			"sha": "0123456789abcdef0123456789abcdef01234567",
			"web_url": "https://gitlab.com/eshu-hq/gitlab-demo-service/-/pipelines/4201"
		},
		"jobs": [{"name": "no-id-job"}]
	}`)
	envelopes, err := GitLabCIFixtureEnvelopes(raw, FixtureContext{
		ScopeID:             "gitlab-ci://gitlab.com/eshu-hq/gitlab-demo-service",
		GenerationID:        "4201:1",
		CollectorInstanceID: "fixture-gitlab-ci",
	})
	if err != nil {
		t.Fatalf("GitLabCIFixtureEnvelopes() error = %v", err)
	}

	byKind := envelopesByKind(envelopes)
	assertKindCount(t, byKind, facts.CICDJobFactKind, 0)
	assertKindCount(t, byKind, facts.CICDWarningFactKind, 1)
	assertPayload(t, byKind[facts.CICDWarningFactKind][0].Payload, "reason", "job_missing_id")
}

// TestGitLabCIFixtureSharesFactKindsAndJoinKeyShapeWithGitHubActions is the
// central architecture proof for issue #5427: GitLab CI is a second provider
// on the EXISTING ci.* fact contract, not a parallel fact-kind family. Both
// providers emit identical FactKind constants, and the reducer's join key
// (provider, run_id, run_attempt -- see
// go/internal/reducer/ci_cd_run_correlation.go's cicdRunKey) stays disjoint
// per-provider even when the raw provider-native run/pipeline IDs collide
// numerically, because Provider participates in every StableFactKey.
func TestGitLabCIFixtureSharesFactKindsAndJoinKeyShapeWithGitHubActions(t *testing.T) {
	t.Parallel()

	ghRaw := readFixture(t, "testdata/github_actions_success.json")
	ghEnvelopes, err := GitHubActionsFixtureEnvelopes(ghRaw, FixtureContext{
		ScopeID:             "github-actions://github.com/eshu-hq/eshu/ci.yml",
		GenerationID:        "123456789:2",
		CollectorInstanceID: "fixture-gh-actions",
	})
	if err != nil {
		t.Fatalf("GitHubActionsFixtureEnvelopes() error = %v", err)
	}
	glRaw := readFixture(t, "testdata/gitlab_ci_success.json")
	glEnvelopes, err := GitLabCIFixtureEnvelopes(glRaw, FixtureContext{
		ScopeID:             "gitlab-ci://gitlab.com/eshu-hq/gitlab-demo-service",
		GenerationID:        "4200:1",
		CollectorInstanceID: "fixture-gitlab-ci",
	})
	if err != nil {
		t.Fatalf("GitLabCIFixtureEnvelopes() error = %v", err)
	}

	ghRun := envelopesByKind(ghEnvelopes)[facts.CICDRunFactKind][0]
	glRun := envelopesByKind(glEnvelopes)[facts.CICDRunFactKind][0]
	if ghRun.FactKind != glRun.FactKind {
		t.Fatalf("FactKind mismatch: github=%q gitlab=%q, want same shared kind", ghRun.FactKind, glRun.FactKind)
	}
	if ghRun.SchemaVersion != glRun.SchemaVersion {
		t.Fatalf("SchemaVersion mismatch: github=%q gitlab=%q, want same shared schema", ghRun.SchemaVersion, glRun.SchemaVersion)
	}
	for _, key := range []string{"run_id", "run_attempt", "repository_id", "commit_sha", "status", "result", "branch"} {
		if _, ok := ghRun.Payload[key]; !ok {
			t.Fatalf("github run payload missing shared key %q: %#v", key, ghRun.Payload)
		}
		if _, ok := glRun.Payload[key]; !ok {
			t.Fatalf("gitlab run payload missing shared key %q: %#v", key, glRun.Payload)
		}
	}
	if ghRun.Payload["provider"] == glRun.Payload["provider"] {
		t.Fatalf("provider must differ between providers, both were %#v", ghRun.Payload["provider"])
	}
	if ghRun.StableFactKey == glRun.StableFactKey {
		t.Fatalf("StableFactKey collided across providers: %q", ghRun.StableFactKey)
	}
	if ghRun.FactID == glRun.FactID {
		t.Fatalf("FactID collided across providers: %q", ghRun.FactID)
	}
}

// BenchmarkGitLabCIEnvelopesEndToEnd measures the full envelope-build path for
// a realistic success fixture (1 run + 2 jobs + 1 artifact), the GitLab
// counterpart to BenchmarkGitHubActionsEnvelopesEndToEnd.
func BenchmarkGitLabCIEnvelopesEndToEnd(b *testing.B) {
	raw, err := os.ReadFile("testdata/gitlab_ci_success.json")
	if err != nil {
		b.Fatalf("read fixture: %v", err)
	}
	ctx := FixtureContext{
		ScopeID:             "gitlab-ci://gitlab.com/eshu-hq/gitlab-demo-service",
		GenerationID:        "4200:1",
		CollectorInstanceID: "fixture-gitlab-ci",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		envelopes, err := GitLabCIFixtureEnvelopes(raw, ctx)
		if err != nil {
			b.Fatalf("GitLabCIFixtureEnvelopes error: %v", err)
		}
		_ = envelopes
	}
}
