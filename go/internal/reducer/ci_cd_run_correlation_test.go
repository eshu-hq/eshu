package reducer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const testCICDDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
const testCICDAmbiguousDigest = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

type stubCICDRunCorrelationFactLoader struct {
	scopeFacts []facts.Envelope
	active     []facts.Envelope
	kindCalls  [][]string
	activeCall int
}

func (s *stubCICDRunCorrelationFactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubCICDRunCorrelationFactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	kinds []string,
) ([]facts.Envelope, error) {
	s.kindCalls = append(s.kindCalls, append([]string(nil), kinds...))
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubCICDRunCorrelationFactLoader) ListActiveCICDRunCorrelationFacts(
	context.Context,
	[]string,
) ([]facts.Envelope, error) {
	s.activeCall++
	return append([]facts.Envelope(nil), s.active...), nil
}

type recordingCICDRunCorrelationWriter struct {
	write CICDRunCorrelationWrite
	calls int
}

func (w *recordingCICDRunCorrelationWriter) WriteCICDRunCorrelations(
	_ context.Context,
	write CICDRunCorrelationWrite,
) (CICDRunCorrelationWriteResult, error) {
	w.calls++
	w.write = write
	return CICDRunCorrelationWriteResult{
		CanonicalWrites: cicdRunCorrelationCanonicalWrites(write.Decisions),
		FactsWritten:    len(write.Decisions),
	}, nil
}

func TestBuildCICDRunCorrelationDecisionsClassifiesEvidence(t *testing.T) {
	t.Parallel()

	decisions := BuildCICDRunCorrelationDecisions([]facts.Envelope{
		ciRunFact("run-exact", "github_actions", "repo-api", "abc123"),
		ciArtifactFact("artifact-exact", "run-exact", testCICDDigest),
		containerImageIdentityFact("image-identity", "repo-api", "registry.example.com/team/api@"+testCICDDigest, testCICDDigest),
		ciRunFact("run-derived", "github_actions", "repo-api", "def456"),
		ciEnvironmentFact("env-derived", "run-derived", "prod"),
		ciRunFact("run-unresolved", "github_actions", "", ""),
		ciRunFact("run-ambiguous", "github_actions", "repo-api", "fedcba"),
		ciArtifactFact("artifact-ambiguous", "run-ambiguous", testCICDAmbiguousDigest),
		containerImageIdentityFact("image-identity-ambiguous-1", "repo-api", "registry.example.com/team/api@"+testCICDAmbiguousDigest, testCICDAmbiguousDigest),
		containerImageIdentityFact("image-identity-ambiguous-2", "repo-api", "registry.example.com/team/worker@"+testCICDAmbiguousDigest, testCICDAmbiguousDigest),
		ciRunFact("run-rejected", "github_actions", "repo-api", "999999"),
		ciStepShellHintFact("step-rejected", "run-rejected"),
	})

	got := cicdDecisionsByRun(decisions)
	assertCICDDecision(t, got["github_actions:run-exact:1"], CICDRunCorrelationExact, 1)
	assertCICDDecision(t, got["github_actions:run-derived:1"], CICDRunCorrelationDerived, 0)
	assertCICDDecision(t, got["github_actions:run-unresolved:1"], CICDRunCorrelationUnresolved, 0)
	assertCICDDecision(t, got["github_actions:run-ambiguous:1"], CICDRunCorrelationAmbiguous, 0)
	assertCICDDecision(t, got["github_actions:run-rejected:1"], CICDRunCorrelationRejected, 0)
}

func TestCICDRunCorrelationHandlerLoadsActiveImageIdentityFacts(t *testing.T) {
	t.Parallel()

	loader := &stubCICDRunCorrelationFactLoader{
		scopeFacts: []facts.Envelope{
			ciRunFact("run-exact", "github_actions", "repo-api", "abc123"),
			ciArtifactFact("artifact-exact", "run-exact", testCICDDigest),
		},
		active: []facts.Envelope{
			containerImageIdentityFact("image-identity", "repo-api", "registry.example.com/team/api@"+testCICDDigest, testCICDDigest),
		},
	}
	writer := &recordingCICDRunCorrelationWriter{}
	handler := CICDRunCorrelationHandler{
		FactLoader: loader,
		Writer:     writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-cicd",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "run-exact:1",
		SourceSystem: "ci_cd_run",
		Domain:       DomainCICDRunCorrelation,
		Cause:        "ci run observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteCICDRunCorrelations() calls = %d, want 1", writer.calls)
	}
	if loader.activeCall != 1 {
		t.Fatalf("ListActiveCICDRunCorrelationFacts() calls = %d, want 1", loader.activeCall)
	}
	if got, want := strings.Join(loader.kindCalls[0], ","), strings.Join(cicdRunCorrelationFactKinds(), ","); got != want {
		t.Fatalf("ListFactsByKind() kinds = %q, want %q", got, want)
	}
	if !strings.Contains(result.EvidenceSummary, "exact=1") {
		t.Fatalf("EvidenceSummary = %q, want exact count", result.EvidenceSummary)
	}
}

func TestPostgresCICDRunCorrelationWriterPersistsReducerFacts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 15, 17, 0, 0, 0, time.UTC)
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresCICDRunCorrelationWriter{
		DB:  db,
		Now: func() time.Time { return now },
	}

	result, err := writer.WriteCICDRunCorrelations(context.Background(), CICDRunCorrelationWrite{
		IntentID:     "intent-cicd",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "run-exact:1",
		SourceSystem: "ci_cd_run",
		Cause:        "ci run observed",
		Decisions: []CICDRunCorrelationDecision{
			{
				Provider:         "github_actions",
				RunID:            "run-exact",
				RunAttempt:       "1",
				RepositoryID:     "repo-api",
				CommitSHA:        "abc123",
				ArtifactDigest:   testCICDDigest,
				ImageRef:         "registry.example.com/team/api@" + testCICDDigest,
				Outcome:          CICDRunCorrelationExact,
				Reason:           "artifact digest matches one container image identity row",
				CanonicalWrites:  1,
				EvidenceFactIDs:  []string{"run-fact", "artifact-fact", "image-fact"},
				CanonicalTarget:  "container_image",
				CorrelationKind:  "artifact_image",
				ProvenanceOnly:   false,
				SourceLayerKinds: []string{"reported", "observed_resource"},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteCICDRunCorrelations() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := result.FactsWritten, 1; got != want {
		t.Fatalf("FactsWritten = %d, want %d", got, want)
	}
	payload := unmarshalCICDRunCorrelationPayload(t, db.execs[0].args[14])
	if got, want := payload["correlation_kind"], "artifact_image"; got != want {
		t.Fatalf("correlation_kind = %#v, want %#v", got, want)
	}
	if got, want := payload["canonical_writes"], float64(1); got != want {
		t.Fatalf("canonical_writes = %#v, want %#v", got, want)
	}
	if got, want := payload["provenance_only"], false; got != want {
		t.Fatalf("provenance_only = %#v, want %#v", got, want)
	}
}

func TestPostgresCICDRunCorrelationWriterDoesNotAddObservedLayerForDerivedRows(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresCICDRunCorrelationWriter{
		DB: db,
		Now: func() time.Time {
			return time.Date(2026, 5, 15, 17, 0, 0, 0, time.UTC)
		},
	}

	_, err := writer.WriteCICDRunCorrelations(context.Background(), CICDRunCorrelationWrite{
		IntentID:     "intent-cicd",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "run-derived:1",
		SourceSystem: "ci_cd_run",
		Cause:        "ci run observed",
		Decisions: []CICDRunCorrelationDecision{
			{
				Provider:        "github_actions",
				RunID:           "run-derived",
				RunAttempt:      "1",
				RepositoryID:    "repo-api",
				CommitSHA:       "abc123",
				Outcome:         CICDRunCorrelationDerived,
				Reason:          "run has provider evidence but no explicit artifact identity anchor",
				ProvenanceOnly:  true,
				CanonicalWrites: 0,
				EvidenceFactIDs: []string{"run-fact"},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteCICDRunCorrelations() error = %v, want nil", err)
	}
	payload := unmarshalCICDRunCorrelationPayload(t, db.execs[0].args[14])
	sourceLayers, ok := payload["source_layers"].([]any)
	if !ok {
		t.Fatalf("source_layers type = %T, want []any", payload["source_layers"])
	}
	if got, want := len(sourceLayers), 1; got != want {
		t.Fatalf("len(source_layers) = %d, want %d: %#v", got, want, sourceLayers)
	}
	if got, want := sourceLayers[0], "source_declaration"; got != want {
		t.Fatalf("source_layers[0] = %#v, want %#v", got, want)
	}
}

func assertCICDDecision(
	t *testing.T,
	decision CICDRunCorrelationDecision,
	wantOutcome CICDRunCorrelationOutcome,
	wantWrites int,
) {
	t.Helper()
	if got := decision.Outcome; got != wantOutcome {
		t.Fatalf("Outcome = %q, want %q", got, wantOutcome)
	}
	if got := decision.CanonicalWrites; got != wantWrites {
		t.Fatalf("CanonicalWrites = %d, want %d", got, wantWrites)
	}
}

func cicdDecisionsByRun(decisions []CICDRunCorrelationDecision) map[string]CICDRunCorrelationDecision {
	out := make(map[string]CICDRunCorrelationDecision, len(decisions))
	for _, decision := range decisions {
		out[decision.Provider+":"+decision.RunID+":"+decision.RunAttempt] = decision
	}
	return out
}

func ciRunFact(runID, provider, repositoryID, commitSHA string) facts.Envelope {
	return facts.Envelope{
		FactID:           "ci.run:" + runID,
		FactKind:         facts.CICDRunFactKind,
		SourceRef:        facts.Ref{SourceSystem: "ci_cd_run"},
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"provider":      provider,
			"run_id":        runID,
			"run_attempt":   "1",
			"repository_id": repositoryID,
			"commit_sha":    commitSHA,
			"status":        "completed",
			"result":        "success",
		},
	}
}

func ciArtifactFact(factID, runID, digest string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		FactKind:         facts.CICDArtifactFactKind,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"provider":        "github_actions",
			"run_id":          runID,
			"run_attempt":     "1",
			"artifact_type":   "container_image",
			"artifact_digest": digest,
		},
	}
}

func ciEnvironmentFact(factID, runID, environment string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		FactKind:         facts.CICDEnvironmentObservationFactKind,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"provider":    "github_actions",
			"run_id":      runID,
			"run_attempt": "1",
			"environment": environment,
		},
	}
}

func ciStepShellHintFact(factID, runID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		FactKind:         facts.CICDStepFactKind,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"provider":               "github_actions",
			"run_id":                 runID,
			"run_attempt":            "1",
			"deployment_hint_source": "shell",
		},
	}
}

func containerImageIdentityFact(factID, repositoryID, imageRef, digest string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		FactKind:         containerImageIdentityFactKind,
		SourceConfidence: facts.SourceConfidenceInferred,
		Payload: map[string]any{
			"repository_id": repositoryID,
			"image_ref":     imageRef,
			"digest":        digest,
		},
	}
}

func unmarshalCICDRunCorrelationPayload(t *testing.T, raw any) map[string]any {
	t.Helper()
	bytes, ok := raw.([]byte)
	if !ok {
		t.Fatalf("payload arg type = %T, want []byte", raw)
	}
	var payload map[string]any
	if err := json.Unmarshal(bytes, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload): %v", err)
	}
	return payload
}
