package cicdrun

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestGitHubActionsFixtureBuildsReducerConsumableFacts(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/github_actions_success.json")
	observedAt := time.Date(2026, 5, 16, 3, 30, 0, 0, time.UTC)
	envelopes, err := GitHubActionsFixtureEnvelopes(raw, FixtureContext{
		ScopeID:             "github-actions://github.com/eshu-hq/eshu/ci.yml",
		GenerationID:        "123456789:2",
		CollectorInstanceID: "fixture-gh-actions",
		FencingToken:        77,
		ObservedAt:          observedAt,
		SourceURI:           "https://api.github.com/repos/eshu-hq/eshu/actions/runs/123456789",
	})
	if err != nil {
		t.Fatalf("GitHubActionsFixtureEnvelopes() error = %v", err)
	}

	byKind := envelopesByKind(envelopes)
	assertKindCount(t, byKind, facts.CICDPipelineDefinitionFactKind, 1)
	assertKindCount(t, byKind, facts.CICDRunFactKind, 1)
	assertKindCount(t, byKind, facts.CICDJobFactKind, 2)
	assertKindCount(t, byKind, facts.CICDStepFactKind, 3)
	assertKindCount(t, byKind, facts.CICDArtifactFactKind, 1)
	assertKindCount(t, byKind, facts.CICDTriggerEdgeFactKind, 1)
	assertKindCount(t, byKind, facts.CICDEnvironmentObservationFactKind, 1)

	run := byKind[facts.CICDRunFactKind][0]
	assertCICDEnvelope(t, run, facts.CICDRunFactKind, observedAt)
	assertPayload(t, run.Payload, "provider", string(ProviderGitHubActions))
	assertPayload(t, run.Payload, "run_id", "123456789")
	assertPayload(t, run.Payload, "run_attempt", "2")
	assertPayload(t, run.Payload, "repository_id", "github.com/eshu-hq/eshu")
	assertPayload(t, run.Payload, "commit_sha", "0123456789abcdef0123456789abcdef01234567")
	assertPayload(t, run.Payload, "status", "completed")
	assertPayload(t, run.Payload, "result", "success")

	artifact := byKind[facts.CICDArtifactFactKind][0]
	assertPayload(t, artifact.Payload, "artifact_digest", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	assertPayload(t, artifact.Payload, "artifact_type", "container_image")
	if got := artifact.Payload["download_url"]; got != "" {
		t.Fatalf("artifact download_url = %#v, want tokenless URL stripped", got)
	}

	environment := byKind[facts.CICDEnvironmentObservationFactKind][0]
	assertPayload(t, environment.Payload, "environment", "prod")
	assertPayload(t, environment.Payload, "deployment_status", "success")

	for _, envelope := range envelopes {
		if envelope.ScopeID != "github-actions://github.com/eshu-hq/eshu/ci.yml" {
			t.Fatalf("ScopeID = %q, want fixture scope", envelope.ScopeID)
		}
		if envelope.GenerationID != "123456789:2" {
			t.Fatalf("GenerationID = %q, want run attempt generation", envelope.GenerationID)
		}
		if envelope.CollectorKind != CollectorKind {
			t.Fatalf("CollectorKind = %q, want %q", envelope.CollectorKind, CollectorKind)
		}
		if envelope.SourceConfidence != facts.SourceConfidenceReported {
			t.Fatalf("SourceConfidence = %q, want reported", envelope.SourceConfidence)
		}
		if envelope.FencingToken != 77 {
			t.Fatalf("FencingToken = %d, want 77", envelope.FencingToken)
		}
		if envelope.StableFactKey == "" || envelope.FactID == "" {
			t.Fatalf("fact identifiers must not be blank: %#v", envelope)
		}
	}
}

func TestGitHubActionsFixturePreservesAttemptsInFactIdentity(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/github_actions_success.json")
	firstAttemptRaw := bytes.Replace(raw, []byte(`"run_attempt": 2`), []byte(`"run_attempt": 1`), 1)
	ctx := FixtureContext{
		ScopeID:             "github-actions://github.com/eshu-hq/eshu/ci.yml",
		GenerationID:        "123456789:2",
		CollectorInstanceID: "fixture-gh-actions",
		ObservedAt:          time.Date(2026, 5, 16, 3, 30, 0, 0, time.UTC),
	}
	firstAttempt := ctx
	firstAttempt.GenerationID = "123456789:1"
	firstAttemptFacts, err := GitHubActionsFixtureEnvelopes(firstAttemptRaw, firstAttempt)
	if err != nil {
		t.Fatalf("GitHubActionsFixtureEnvelopes(firstAttempt) error = %v", err)
	}
	secondAttemptFacts, err := GitHubActionsFixtureEnvelopes(raw, ctx)
	if err != nil {
		t.Fatalf("GitHubActionsFixtureEnvelopes(secondAttempt) error = %v", err)
	}

	firstRun := envelopesByKind(firstAttemptFacts)[facts.CICDRunFactKind][0]
	secondRun := envelopesByKind(secondAttemptFacts)[facts.CICDRunFactKind][0]
	if firstRun.StableFactKey == secondRun.StableFactKey {
		t.Fatalf("StableFactKey = %q for both attempts, want attempts preserved separately", firstRun.StableFactKey)
	}
	if firstRun.FactID == secondRun.FactID {
		t.Fatalf("FactID = %q for both attempts, want generation-specific identity", firstRun.FactID)
	}
}

func TestGitHubActionsFixtureEmitsPartialWarnings(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "testdata/github_actions_partial.json")
	envelopes, err := GitHubActionsFixtureEnvelopes(raw, FixtureContext{
		ScopeID:             "github-actions://github.com/eshu-hq/eshu/deploy.yml",
		GenerationID:        "987654321:1",
		CollectorInstanceID: "fixture-gh-actions",
		ObservedAt:          time.Date(2026, 5, 16, 4, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("GitHubActionsFixtureEnvelopes() error = %v", err)
	}

	byKind := envelopesByKind(envelopes)
	assertKindCount(t, byKind, facts.CICDRunFactKind, 1)
	assertKindCount(t, byKind, facts.CICDArtifactFactKind, 1)
	assertKindCount(t, byKind, facts.CICDWarningFactKind, 2)

	artifact := byKind[facts.CICDArtifactFactKind][0]
	if got := artifact.Payload["artifact_digest"]; got != "" {
		t.Fatalf("artifact_digest = %#v, want blank when provider omitted digest", got)
	}

	warnings := byKind[facts.CICDWarningFactKind]
	wantReasons := map[string]bool{
		"partial_jobs_payload":    false,
		"artifact_missing_digest": false,
	}
	for _, warning := range warnings {
		wantReasons[warning.Payload["reason"].(string)] = true
		assertPayload(t, warning.Payload, "partial_generation", true)
	}
	for reason, found := range wantReasons {
		if !found {
			t.Fatalf("warning reason %q missing from %#v", reason, warnings)
		}
	}
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return raw
}

func envelopesByKind(envelopes []facts.Envelope) map[string][]facts.Envelope {
	out := map[string][]facts.Envelope{}
	for _, envelope := range envelopes {
		out[envelope.FactKind] = append(out[envelope.FactKind], envelope)
	}
	return out
}

func assertKindCount(t *testing.T, byKind map[string][]facts.Envelope, kind string, want int) {
	t.Helper()

	if got := len(byKind[kind]); got != want {
		t.Fatalf("len(%s) = %d, want %d; all=%#v", kind, got, want, byKind)
	}
}

func assertCICDEnvelope(t *testing.T, envelope facts.Envelope, factKind string, observedAt time.Time) {
	t.Helper()

	if envelope.FactKind != factKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, factKind)
	}
	if envelope.SchemaVersion != facts.CICDSchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, facts.CICDSchemaVersion)
	}
	if !envelope.ObservedAt.Equal(observedAt) {
		t.Fatalf("ObservedAt = %s, want %s", envelope.ObservedAt, observedAt)
	}
}

func assertPayload(t *testing.T, payload map[string]any, key string, want any) {
	t.Helper()

	if got := payload[key]; got != want {
		t.Fatalf("Payload[%q] = %#v, want %#v; payload=%#v", key, got, want, payload)
	}
}
