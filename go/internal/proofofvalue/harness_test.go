package proofofvalue

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/iacreachability"
)

// loadDeadIaCCorpus loads the dead-IaC product-truth fixture corpus from disk,
// grouping files by their top-level directory (the repo ID), mirroring the
// loader the iacreachability product-truth test uses.
func loadDeadIaCCorpus(t *testing.T) (map[string][]iacreachability.File, []GroundTruth) {
	t.Helper()
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	fixtureRoot := filepath.Join(repoRoot, "tests", "fixtures", "product_truth", "dead_iac")
	expectedPath := filepath.Join(repoRoot, "tests", "fixtures", "product_truth", "expected", "dead_iac.json")

	filesByRepo := map[string][]iacreachability.File{}
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
		repoID, repoRelativePath, ok := strings.Cut(filepath.ToSlash(relative), "/")
		if !ok {
			repoID, repoRelativePath = relative, ""
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		filesByRepo[repoID] = append(filesByRepo[repoID], iacreachability.File{
			RepoID:       repoID,
			RelativePath: filepath.ToSlash(repoRelativePath),
			Content:      string(content),
		})
		return nil
	}); err != nil {
		t.Fatalf("walk fixture corpus: %v", err)
	}

	content, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read expected truth: %v", err)
	}
	var expected struct {
		Assertions []GroundTruth `json:"capability_assertions"`
	}
	if err := json.Unmarshal(content, &expected); err != nil {
		t.Fatalf("parse expected truth: %v", err)
	}
	if len(expected.Assertions) == 0 {
		t.Fatalf("no ground-truth assertions loaded")
	}
	return filesByRepo, expected.Assertions
}

func TestHarnessProvesEshuOutperformsBaselineOnRealCorpus(t *testing.T) {
	filesByRepo, truths := loadDeadIaCCorpus(t)

	questions, predictions, err := BuildRun(filesByRepo, truths)
	if err != nil {
		t.Fatalf("BuildRun: %v", err)
	}
	if len(questions) != len(truths) {
		t.Fatalf("question count = %d, want %d", len(questions), len(truths))
	}

	report, err := Score("product_truth/dead_iac", questions, predictions)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}

	// Eshu must match the curated ground truth exactly: the analyzer is the
	// product-truth oracle for this corpus.
	if report.Eshu.Correct != report.QuestionCount {
		t.Errorf("eshu correct = %d, want %d (per-question: %+v)", report.Eshu.Correct, report.QuestionCount, report.Questions)
	}

	// The whole point of the harness: Eshu must beat plain grep on accuracy.
	// This is a real measured delta over the real corpus, not a fabricated
	// number. If the corpus or analyzer changes such that grep ties Eshu, this
	// guard fails and forces a re-examination rather than a silent pass.
	if report.Delta.AccuracyDelta <= 0 {
		t.Errorf("accuracy delta = %v, want > 0; baseline acc=%v eshu acc=%v",
			report.Delta.AccuracyDelta, report.Baseline.Accuracy, report.Eshu.Accuracy)
	}

	t.Logf("baseline accuracy=%.3f eshu accuracy=%.3f accuracy_delta=%.3f dangerous_avoided=%d",
		report.Baseline.Accuracy, report.Eshu.Accuracy, report.Delta.AccuracyDelta, report.Delta.DangerousMistakesAvoided)
}
