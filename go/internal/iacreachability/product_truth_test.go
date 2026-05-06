package iacreachability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeMatchesDeadIaCProductTruthFixtures(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	fixtureRoot := filepath.Join(repoRoot, "tests", "fixtures", "product_truth", "dead_iac")
	expectedPath := filepath.Join(repoRoot, "tests", "fixtures", "product_truth", "expected", "dead_iac.json")

	filesByRepo := map[string][]File{}
	if err := filepath.WalkDir(fixtureRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(fixtureRoot, path)
		if err != nil {
			return err
		}
		repoID, repoRelativePath := splitFixtureRepoPath(relative)
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		filesByRepo[repoID] = append(filesByRepo[repoID], File{
			RepoID:       repoID,
			RelativePath: filepath.ToSlash(repoRelativePath),
			Content:      string(content),
		})
		return nil
	}); err != nil {
		t.Fatalf("walk fixture corpus: %v", err)
	}

	rows := Analyze(filesByRepo, Options{IncludeAmbiguous: true})
	byArtifact := map[string]Row{}
	for _, row := range rows {
		byArtifact[row.RepoID+"/"+row.ArtifactPath] = row
	}

	expected := readDeadIaCExpected(t, expectedPath)
	for _, assertion := range expected.Assertions {
		row, ok := byArtifact[assertion.Artifact]
		if !ok {
			t.Fatalf("expected artifact %q missing from analyzer rows", assertion.Artifact)
		}
		if got := string(row.Reachability); got != assertion.ExpectedReachability {
			t.Fatalf("%s reachability = %q, want %q", assertion.ID, got, assertion.ExpectedReachability)
		}
		if assertion.ExpectedFinding != "" && string(row.Finding) != assertion.ExpectedFinding {
			t.Fatalf("%s finding = %q, want %q", assertion.ID, row.Finding, assertion.ExpectedFinding)
		}
		if assertion.MustNotReportAsDead && row.Finding != FindingInUse {
			t.Fatalf("%s finding = %q, want in_use", assertion.ID, row.Finding)
		}
	}
}

type deadIaCExpected struct {
	Assertions []deadIaCAssertion `json:"capability_assertions"`
}

type deadIaCAssertion struct {
	ID                   string `json:"id"`
	Artifact             string `json:"artifact"`
	ExpectedReachability string `json:"expected_reachability"`
	ExpectedFinding      string `json:"expected_finding"`
	MustNotReportAsDead  bool   `json:"must_not_report_as_dead"`
}

func readDeadIaCExpected(t *testing.T, path string) deadIaCExpected {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read expected truth: %v", err)
	}
	var expected deadIaCExpected
	if err := json.Unmarshal(content, &expected); err != nil {
		t.Fatalf("parse expected truth: %v", err)
	}
	return expected
}

func splitFixtureRepoPath(relative string) (string, string) {
	repoID, repoRelativePath, ok := strings.Cut(filepath.ToSlash(relative), "/")
	if !ok {
		return relative, ""
	}
	return repoID, repoRelativePath
}
