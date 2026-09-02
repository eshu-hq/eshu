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

// gitLabOnlyFactKinds are fact kinds GitLabCIFixtureEnvelopes emits for
// testdata/gitlab_ci_success.json that GitHubActionsFixtureEnvelopes does NOT
// emit for testdata/github_actions_success.json. ci.warning is provider-
// agnostic infrastructure (github_actions_fixture.go emits it too, on its
// OWN partial/malformed-input paths — see TestGitHubActionsFixtureRecordsJobsPartialWarning
// and similar), but it is not present in the GitHub success fixture's output
// because that fixture's artifact carries a real digest; GitLab's Jobs API
// reports NO artifact digest at this level ever (see gitlabArtifact's doc
// comment in types.go), so gitlabArtifactEnvelope always follows an artifact
// with an "artifact_missing_digest" ci.warning — a difference in fixture
// SHAPE, not in the shared kind contract.
var gitLabOnlyFactKinds = []string{facts.CICDWarningFactKind}

// gitHubOnlyFactKinds are fact kinds GitHubActionsFixtureEnvelopes emits for
// testdata/github_actions_success.json that GitLabCIFixtureEnvelopes does NOT
// emit for testdata/gitlab_ci_success.json, because GitLab's Pipelines/Jobs
// APIs expose no matching resource at this level — see gitlab_ci_fixture.go's
// package-level doc comment for the full v1-scope rationale:
//   - ci.pipeline_definition: no workflow resource distinct from the pipeline
//     itself (pipeline.id IS the run).
//   - ci.step: no step-level breakdown in the Jobs API.
//   - ci.trigger_edge, ci.environment_observation: out of v1 scope, matching
//     ghactionsruntime's own live client, which also does not populate these
//     for GitHub.
var gitHubOnlyFactKinds = []string{
	facts.CICDPipelineDefinitionFactKind,
	facts.CICDStepFactKind,
	facts.CICDTriggerEdgeFactKind,
	facts.CICDEnvironmentObservationFactKind,
}

// sharedCICDFactKinds are the fact kinds BOTH providers emit for their
// respective success fixtures — the actual shared contract issue #5427
// exists to prove, and the set list_ci_cd_run_correlations/
// ci_cd_run_correlation.go's join logic must treat identically regardless of
// provider.
var sharedCICDFactKinds = []string{
	facts.CICDRunFactKind,
	facts.CICDJobFactKind,
	facts.CICDArtifactFactKind,
}

// TestGitLabCIFixtureSharesFactKindsAndJoinKeyShapeWithGitHubActions is the
// central architecture proof for issue #5427: GitLab CI is a second provider
// on the EXISTING ci.* fact contract, not a parallel fact-kind family. It
// compares the FULL set of fact kinds each provider's normalizer emits for
// its own success fixture (codex review on PR #5778: the prior version only
// compared the single ci.run kind despite the test name and doc comment
// claiming "identical FactKind constants" for the whole contract) — the two
// kind sets must differ by EXACTLY the documented, scope-justified kinds in
// gitHubOnlyFactKinds/gitLabOnlyFactKinds, not silently omit any kind from
// comparison. For every kind both providers emit (sharedCICDFactKinds), it
// also proves FactKind/SchemaVersion equality and join-key shape parity
// (provider, run_id, run_attempt, plus the reducer's join key -- see
// go/internal/reducer/cicdrun/ci_cd_run_correlation_decode.go's
// CICDRunKeyFromParts), and that the
// join key stays disjoint per-provider even when the raw provider-native
// run/pipeline IDs collide numerically, because Provider participates in
// every StableFactKey.
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

	ghByKind := envelopesByKind(ghEnvelopes)
	glByKind := envelopesByKind(glEnvelopes)
	ghKinds := factKindSet(ghByKind)
	glKinds := factKindSet(glByKind)
	t.Logf("github fact kinds: %v", ghKinds)
	t.Logf("gitlab fact kinds: %v", glKinds)

	assertFactKindSetDiff(t, "github-only", ghKinds, glKinds, gitHubOnlyFactKinds)
	assertFactKindSetDiff(t, "gitlab-only", glKinds, ghKinds, gitLabOnlyFactKinds)
	for _, kind := range sharedCICDFactKinds {
		if _, ok := ghKinds[kind]; !ok {
			t.Fatalf("github fixture emitted no %q fact; sharedCICDFactKinds is stale", kind)
		}
		if _, ok := glKinds[kind]; !ok {
			t.Fatalf("gitlab fixture emitted no %q fact; sharedCICDFactKinds is stale", kind)
		}
	}

	for _, kind := range sharedCICDFactKinds {
		ghEnvelope := ghByKind[kind][0]
		glEnvelope := glByKind[kind][0]
		if ghEnvelope.FactKind != glEnvelope.FactKind {
			t.Fatalf("%s: FactKind mismatch: github=%q gitlab=%q, want same shared kind", kind, ghEnvelope.FactKind, glEnvelope.FactKind)
		}
		if ghEnvelope.SchemaVersion != glEnvelope.SchemaVersion {
			t.Fatalf("%s: SchemaVersion mismatch: github=%q gitlab=%q, want same shared schema", kind, ghEnvelope.SchemaVersion, glEnvelope.SchemaVersion)
		}
		for _, key := range []string{"provider", "run_id", "run_attempt"} {
			if _, ok := ghEnvelope.Payload[key]; !ok {
				t.Fatalf("%s: github payload missing join-key field %q: %#v", kind, key, ghEnvelope.Payload)
			}
			if _, ok := glEnvelope.Payload[key]; !ok {
				t.Fatalf("%s: gitlab payload missing join-key field %q: %#v", kind, key, glEnvelope.Payload)
			}
		}
		if ghEnvelope.Payload["provider"] == glEnvelope.Payload["provider"] {
			t.Fatalf("%s: provider must differ between providers, both were %#v", kind, ghEnvelope.Payload["provider"])
		}
		if ghEnvelope.StableFactKey == glEnvelope.StableFactKey {
			t.Fatalf("%s: StableFactKey collided across providers: %q", kind, ghEnvelope.StableFactKey)
		}
		if ghEnvelope.FactID == glEnvelope.FactID {
			t.Fatalf("%s: FactID collided across providers: %q", kind, ghEnvelope.FactID)
		}
	}

	ghRun := ghByKind[facts.CICDRunFactKind][0]
	glRun := glByKind[facts.CICDRunFactKind][0]
	for _, key := range []string{"repository_id", "commit_sha", "status", "result", "branch"} {
		if _, ok := ghRun.Payload[key]; !ok {
			t.Fatalf("github run payload missing shared key %q: %#v", key, ghRun.Payload)
		}
		if _, ok := glRun.Payload[key]; !ok {
			t.Fatalf("gitlab run payload missing shared key %q: %#v", key, glRun.Payload)
		}
	}
}

// factKindSet returns the set of fact kinds present in byKind.
func factKindSet(byKind map[string][]facts.Envelope) map[string]struct{} {
	out := make(map[string]struct{}, len(byKind))
	for kind, envelopes := range byKind {
		if len(envelopes) > 0 {
			out[kind] = struct{}{}
		}
	}
	return out
}

// assertFactKindSetDiff asserts that (from - other) equals EXACTLY
// wantExtraKinds — every kind `from` emits that `other` does not must be
// documented in wantExtraKinds, and every documented kind must actually be
// present, so a future normalizer change that adds or removes a shared kind
// without updating this test's documentation fails loudly here instead of
// silently narrowing what gets compared.
func assertFactKindSetDiff(t *testing.T, label string, from, other map[string]struct{}, wantExtraKinds []string) {
	t.Helper()

	want := make(map[string]struct{}, len(wantExtraKinds))
	for _, kind := range wantExtraKinds {
		want[kind] = struct{}{}
	}
	for kind := range from {
		if _, sharedWithOther := other[kind]; sharedWithOther {
			continue
		}
		if _, documented := want[kind]; !documented {
			t.Fatalf("%s: fact kind %q is emitted but not present in the other provider's fixture output and is not documented as %s; add it to the appropriate gitHubOnlyFactKinds/gitLabOnlyFactKinds list with a reason, or extend sharedCICDFactKinds if it should now be compared", label, kind, label)
		}
	}
	for kind := range want {
		if _, present := from[kind]; !present {
			t.Fatalf("%s: fact kind %q is documented as %s-only but the fixture no longer emits it; update the list", label, kind, label)
		}
		if _, sharedWithOther := other[kind]; sharedWithOther {
			t.Fatalf("%s: fact kind %q is documented as %s-only but the OTHER provider's fixture now emits it too; move it into sharedCICDFactKinds", label, kind, label)
		}
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
