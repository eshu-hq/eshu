package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func stubChangeImpactFetch(t *testing.T, envelope changeImpactEnvelope, err error) func() {
	t.Helper()
	original := changeImpactFetch
	changeImpactFetch = func(_ *APIClient, _ changeImpactOptions) (changeImpactEnvelope, error) {
		return envelope, err
	}
	return func() { changeImpactFetch = original }
}

func TestChangeImpactCommandIsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"change", "impact"})
	if err != nil {
		t.Fatalf("rootCmd.Find(change impact) error = %v", err)
	}
	if cmd == nil || cmd.Name() != "impact" {
		t.Fatalf("resolved command = %#v, want impact", cmd)
	}
	for _, name := range []string{"json", "repo-id", "base", "head", "file", "repo-path", "topic", "service-name", "limit", "max-depth", "service-url"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("change impact flag %q missing", name)
		}
	}
}

func TestFetchChangeImpactRequestsCanonicalEnvelope(t *testing.T) {
	var gotAccept string
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.EscapedPath()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"workflow":"pre_change_impact"},"truth":{"freshness":{"state":"fresh"}},"error":null}`))
	}))
	defer server.Close()

	client := &APIClient{BaseURL: server.URL, HTTPClient: server.Client()}
	if _, err := fetchChangeImpact(client, changeImpactOptions{
		RepoID:       "repo-1",
		BaseRef:      "main",
		HeadRef:      "feature/pre-change",
		ChangedPaths: []string{"go/internal/query/prechange_impact.go"},
		Changes: []changeImpactFileChange{{
			Path:    "go/internal/query/prechange_impact.go",
			OldPath: "go/internal/query/change_impact.go",
			Status:  "renamed",
		}},
		Topic:    "pre-change workflow",
		MaxDepth: 3,
		Limit:    25,
	}); err != nil {
		t.Fatalf("fetchChangeImpact() error = %v", err)
	}
	if gotAccept != eshuEnvelopeMIMEType {
		t.Fatalf("Accept = %q, want %q", gotAccept, eshuEnvelopeMIMEType)
	}
	if gotPath != "/api/v0/impact/pre-change" {
		t.Fatalf("path = %q", gotPath)
	}
	for key, want := range map[string]any{
		"repo_id":   "repo-1",
		"base_ref":  "main",
		"head_ref":  "feature/pre-change",
		"topic":     "pre-change workflow",
		"max_depth": float64(3),
		"limit":     float64(25),
	} {
		if got := gotBody[key]; got != want {
			t.Fatalf("body[%s] = %#v, want %#v", key, got, want)
		}
	}
	changes := gotBody["changes"].([]any)
	first := changes[0].(map[string]any)
	if got, want := first["status"], "renamed"; got != want {
		t.Fatalf("changes[0].status = %#v, want %#v", got, want)
	}
}

func TestParseGitNameStatusDiffPreservesDeletedAndRenamedFiles(t *testing.T) {
	t.Parallel()

	changes := parseGitNameStatusDiff("M\tgo/a.go\nD\tgo/deleted.go\nR100\tgo/old.go\tgo/new.go\n")
	if got, want := len(changes), 3; got != want {
		t.Fatalf("len(changes) = %d, want %d", got, want)
	}
	if got, want := changes[1].Status, "deleted"; got != want {
		t.Fatalf("deleted status = %q, want %q", got, want)
	}
	if got, want := changes[2].OldPath, "go/old.go"; got != want {
		t.Fatalf("renamed old path = %q, want %q", got, want)
	}
	if got, want := changes[2].Path, "go/new.go"; got != want {
		t.Fatalf("renamed path = %q, want %q", got, want)
	}
}

func TestGitDiffNameStatusDetectsCopiedFiles(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	runGit(t, repoPath, "init")
	runGit(t, repoPath, "config", "user.email", "test"+"@example.invalid")
	runGit(t, repoPath, "config", "user.name", "Test User")
	if err := os.MkdirAll(filepath.Join(repoPath, "go"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "go", "original.go"), []byte("package fixture\n\nfunc Original() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(original) error = %v", err)
	}
	runGit(t, repoPath, "add", "go/original.go")
	runGit(t, repoPath, "commit", "-m", "seed original")
	original, err := os.ReadFile(filepath.Join(repoPath, "go", "original.go"))
	if err != nil {
		t.Fatalf("ReadFile(original) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "go", "copy.go"), original, 0o644); err != nil {
		t.Fatalf("WriteFile(copy) error = %v", err)
	}
	runGit(t, repoPath, "add", "go/copy.go")

	changes, err := gitDiffNameStatus(repoPath, "HEAD", "")
	if err != nil {
		t.Fatalf("gitDiffNameStatus() error = %v", err)
	}
	if got, want := len(changes), 1; got != want {
		t.Fatalf("len(changes) = %d, want %d: %+v", got, want, changes)
	}
	if got, want := changes[0].Status, "copied"; got != want {
		t.Fatalf("copy status = %q, want %q; changes=%+v", got, want, changes)
	}
	if got, want := changes[0].OldPath, "go/original.go"; got != want {
		t.Fatalf("copy old path = %q, want %q", got, want)
	}
	if got, want := changes[0].Path, "go/copy.go"; got != want {
		t.Fatalf("copy path = %q, want %q", got, want)
	}
}

func runGit(t *testing.T, repoPath string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s error = %v output=%s", strings.Join(args, " "), err, string(out))
	}
}

func TestRunChangeImpactRendersSummary(t *testing.T) {
	reset := stubChangeImpactFetch(t, changeImpactEnvelope{
		Data: map[string]any{
			"changed_file_count": float64(2),
			"truncated":          false,
			"code_surface": map[string]any{
				"symbol_count": float64(3),
			},
			"impact_summary": map[string]any{
				"direct_count":     float64(1),
				"transitive_count": float64(2),
			},
			"missing_evidence": []any{},
			"coverage": map[string]any{
				"state": "supported",
			},
		},
		Truth: map[string]any{"freshness": map[string]any{"state": "fresh"}},
	}, nil)
	defer reset()

	out := &bytes.Buffer{}
	cmd := newChangeImpactCommand()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"--repo-id", "repo-1", "--file", "go/a.go"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("change impact command error = %v", err)
	}
	output := out.String()
	for _, want := range []string{"Truth freshness: fresh", "Pre-change impact: 2 changed files", "symbols=3 direct=1 transitive=2"} {
		if !strings.Contains(output, want) {
			t.Fatalf("summary missing %q: %q", want, output)
		}
	}
}

func TestRunChangeImpactRendersPartialSummaryBeforeFailClosed(t *testing.T) {
	reset := stubChangeImpactFetch(t, changeImpactEnvelope{
		Data: map[string]any{
			"changed_file_count": float64(1),
			"truncated":          true,
			"code_surface": map[string]any{
				"symbol_count": float64(1),
			},
			"impact_summary": map[string]any{
				"direct_count":     float64(0),
				"transitive_count": float64(0),
			},
			"missing_evidence": []any{map[string]any{"reason": "changed_path_no_symbol_evidence"}},
			"coverage": map[string]any{
				"state": "partial",
			},
			"answer_packet": map[string]any{
				"partial": true,
			},
		},
		Truth: map[string]any{"freshness": map[string]any{"state": "fresh"}},
	}, nil)
	defer reset()

	out := &bytes.Buffer{}
	cmd := newChangeImpactCommand()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"--repo-id", "repo-1", "--file", "go/a.go"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("change impact command error = nil, want fail-closed partial error")
	}
	output := out.String()
	for _, want := range []string{"Pre-change impact: 1 changed files", "coverage=partial truncated=true", "missing_evidence=1"} {
		if !strings.Contains(output, want) {
			t.Fatalf("partial summary missing %q: %q", want, output)
		}
	}
}

func TestPreChangeImpactDogfoodFixtureProvesWorkflowAdvantage(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/prechange_impact_dogfood.json")
	if err != nil {
		t.Fatalf("read dogfood fixture: %v", err)
	}
	var document struct {
		Fixture struct {
			Task struct {
				ExpectedAffectedEntityIDs []string `json:"expected_affected_entity_ids"`
			} `json:"task"`
			Baselines map[string]struct {
				Steps                    int      `json:"steps"`
				Tokens                   int      `json:"tokens"`
				AffectedEntityIDs        []string `json:"affected_entity_ids"`
				MissingEvidenceReasons   []string `json:"missing_evidence_reasons"`
				RecommendedNextCallCount int      `json:"recommended_next_call_count"`
			} `json:"baselines"`
		} `json:"fixture"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	fixture := document.Fixture
	rawRelationships := fixture.Baselines["raw_relationship_queries"]
	preChange := fixture.Baselines["pre_change_impact"]
	if preChange.Steps >= rawRelationships.Steps {
		t.Fatalf("pre-change steps = %d, raw steps = %d", preChange.Steps, rawRelationships.Steps)
	}
	if preChange.Tokens >= rawRelationships.Tokens {
		t.Fatalf("pre-change tokens = %d, raw tokens = %d", preChange.Tokens, rawRelationships.Tokens)
	}
	if len(preChange.MissingEvidenceReasons) == 0 || len(rawRelationships.MissingEvidenceReasons) != 0 {
		t.Fatalf("missing evidence reasons pre-change=%v raw=%v", preChange.MissingEvidenceReasons, rawRelationships.MissingEvidenceReasons)
	}
	if preChange.RecommendedNextCallCount == 0 {
		t.Fatal("pre-change dogfood baseline must include bounded next calls")
	}
	if strings.Join(preChange.AffectedEntityIDs, ",") != strings.Join(fixture.Task.ExpectedAffectedEntityIDs, ",") {
		t.Fatalf("pre-change affected ids = %v, want %v", preChange.AffectedEntityIDs, fixture.Task.ExpectedAffectedEntityIDs)
	}
}
