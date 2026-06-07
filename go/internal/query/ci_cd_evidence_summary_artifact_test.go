package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCICDListRunCorrelationsExplainsWorkflowArtifactDigestEvidence(t *testing.T) {
	t.Parallel()

	resp := exerciseCICDRunCorrelationEvidenceSummary(t, []CICDRunCorrelationRow{{
		CorrelationID:  "correlation-digest",
		RepositoryID:   "repo://example/api",
		Provider:       "github_actions",
		RunID:          "run-1",
		Outcome:        "exact",
		ArtifactDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}})

	bridge := mustMapField(t, mustMapField(t, resp, "evidence_summary"), "run_artifact_evidence")
	if got, want := bridge["state"], "present"; got != want {
		t.Fatalf("run_artifact_evidence.state = %#v, want %#v", got, want)
	}
	if got, want := bridge["artifact_digest_count"], float64(1); got != want {
		t.Fatalf("run_artifact_evidence.artifact_digest_count = %#v, want %#v", got, want)
	}
	if got, want := bridge["image_ref_count"], float64(0); got != want {
		t.Fatalf("run_artifact_evidence.image_ref_count = %#v, want %#v", got, want)
	}
	if got, want := bridge["reason"], "artifact_digest_present"; got != want {
		t.Fatalf("run_artifact_evidence.reason = %#v, want %#v", got, want)
	}
}

func TestCICDListRunCorrelationsExplainsWorkflowImageRefEvidence(t *testing.T) {
	t.Parallel()

	resp := exerciseCICDRunCorrelationEvidenceSummary(t, []CICDRunCorrelationRow{{
		CorrelationID: "correlation-image-ref",
		RepositoryID:  "repo://example/api",
		Provider:      "github_actions",
		RunID:         "run-1",
		Outcome:       "derived",
		ImageRef:      "registry.example.com/team/api:prod",
	}})

	bridge := mustMapField(t, mustMapField(t, resp, "evidence_summary"), "run_artifact_evidence")
	if got, want := bridge["state"], "present"; got != want {
		t.Fatalf("run_artifact_evidence.state = %#v, want %#v", got, want)
	}
	if got, want := bridge["artifact_digest_count"], float64(0); got != want {
		t.Fatalf("run_artifact_evidence.artifact_digest_count = %#v, want %#v", got, want)
	}
	if got, want := bridge["image_ref_count"], float64(1); got != want {
		t.Fatalf("run_artifact_evidence.image_ref_count = %#v, want %#v", got, want)
	}
	if got, want := bridge["reason"], "image_ref_present"; got != want {
		t.Fatalf("run_artifact_evidence.reason = %#v, want %#v", got, want)
	}
}

func TestCICDListRunCorrelationsExplainsAmbiguousArtifactEvidence(t *testing.T) {
	t.Parallel()

	resp := exerciseCICDRunCorrelationEvidenceSummary(t, []CICDRunCorrelationRow{{
		CorrelationID:  "correlation-ambiguous",
		RepositoryID:   "repo://example/api",
		Provider:       "github_actions",
		RunID:          "run-1",
		Outcome:        "ambiguous",
		ArtifactDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Reason:         "artifact digest matches more than one candidate image identity",
	}})

	bridge := mustMapField(t, mustMapField(t, resp, "evidence_summary"), "run_artifact_evidence")
	if got, want := bridge["state"], "ambiguous"; got != want {
		t.Fatalf("run_artifact_evidence.state = %#v, want %#v", got, want)
	}
	if got, want := bridge["ambiguous_count"], float64(1); got != want {
		t.Fatalf("run_artifact_evidence.ambiguous_count = %#v, want %#v", got, want)
	}
	if got, want := bridge["reason"], "ambiguous_artifact_evidence"; got != want {
		t.Fatalf("run_artifact_evidence.reason = %#v, want %#v", got, want)
	}
}

func TestCICDListRunCorrelationsExplainsStaticWorkflowImageEvidence(t *testing.T) {
	t.Parallel()

	resp := exerciseCICDRunCorrelationEvidenceSummaryWithFiles(t, nil, []FileContent{{
		RepoID:       "repo://example/api",
		RelativePath: ".github/workflows/deploy.yml",
		ArtifactType: "github_actions_workflow",
		Content: `name: deploy
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: docker build -t registry.example.com/team/api:prod .
      - run: docker push registry.example.com/team/api:prod
`,
	}})

	static := mustMapField(t, mustMapField(t, resp, "evidence_summary"), "static_workflow_artifacts")
	if got, want := static["image_ref_count"], float64(1); got != want {
		t.Fatalf("static_workflow_artifacts.image_ref_count = %#v, want %#v", got, want)
	}
	if got, want := static["evidence_class"], "workflow_image_ref"; got != want {
		t.Fatalf("static_workflow_artifacts.evidence_class = %#v, want %#v", got, want)
	}
	bridge := mustMapField(t, mustMapField(t, resp, "evidence_summary"), "run_artifact_evidence")
	if got, want := bridge["reason"], "workflow_image_ref_static_only"; got != want {
		t.Fatalf("run_artifact_evidence.reason = %#v, want %#v", got, want)
	}
}

func TestCICDListRunCorrelationsExplainsUnresolvedStaticWorkflowImageEvidence(t *testing.T) {
	t.Parallel()

	resp := exerciseCICDRunCorrelationEvidenceSummaryWithFiles(t, nil, []FileContent{{
		RepoID:       "repo://example/api",
		RelativePath: ".github/workflows/deploy.yml",
		ArtifactType: "github_actions_workflow",
		Content: `name: deploy
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: docker build -t ${{ env.REGISTRY }}/team/api:${{ github.sha }} .
`,
	}})

	static := mustMapField(t, mustMapField(t, resp, "evidence_summary"), "static_workflow_artifacts")
	if got, want := static["unresolved_count"], float64(1); got != want {
		t.Fatalf("static_workflow_artifacts.unresolved_count = %#v, want %#v", got, want)
	}
	if got, want := static["evidence_class"], "workflow_image_unresolved"; got != want {
		t.Fatalf("static_workflow_artifacts.evidence_class = %#v, want %#v", got, want)
	}
}

func exerciseCICDRunCorrelationEvidenceSummary(
	t *testing.T,
	rows []CICDRunCorrelationRow,
) map[string]any {
	t.Helper()
	return exerciseCICDRunCorrelationEvidenceSummaryWithFiles(t, rows, []FileContent{{
		RepoID:       "repo://example/api",
		RelativePath: ".github/workflows/deploy.yml",
		ArtifactType: "github_actions_workflow",
	}})
}

func exerciseCICDRunCorrelationEvidenceSummaryWithFiles(
	t *testing.T,
	rows []CICDRunCorrelationRow,
	files []FileContent,
) map[string]any {
	t.Helper()
	store := &recordingCICDRunCorrelationStore{rows: rows}
	handler := &CICDHandler{
		Content:      fakePortContentStore{repoFiles: files},
		Correlations: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/ci-cd/run-correlations?repository_id=repo://example/api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return resp
}
