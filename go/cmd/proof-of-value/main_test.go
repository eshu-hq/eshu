package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/proofofvalue"
)

// repoRootForTest resolves the repository root from the test working directory.
func repoRootForTest(t *testing.T) string {
	t.Helper()
	return filepath.Clean(filepath.Join("..", "..", ".."))
}

func TestRunPrintsScorecardOverRealCorpus(t *testing.T) {
	var buf bytes.Buffer
	if err := run(repoRootForTest(t), "product_truth/dead_iac", "", &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"baseline_grep", "eshu", "delta (eshu - baseline)", "dangerous mistakes avoided"} {
		if !strings.Contains(out, want) {
			t.Errorf("scorecard missing %q\n%s", want, out)
		}
	}
}

func TestRunWritesValidArtifact(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "evidence.json")

	var buf bytes.Buffer
	if err := run(repoRootForTest(t), "product_truth/dead_iac", artifact, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}

	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	var report proofofvalue.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("artifact is not valid report JSON: %v", err)
	}
	if report.SchemaVersion != proofofvalue.SchemaVersion {
		t.Errorf("schema version = %q, want %q", report.SchemaVersion, proofofvalue.SchemaVersion)
	}
	if report.Eshu.Correct != report.QuestionCount {
		t.Errorf("eshu correct = %d, want %d", report.Eshu.Correct, report.QuestionCount)
	}
	if report.Delta.AccuracyDelta <= 0 {
		t.Errorf("accuracy delta = %v, want > 0", report.Delta.AccuracyDelta)
	}
}

func TestRunFailsOnMissingCorpus(t *testing.T) {
	var buf bytes.Buffer
	if err := run(t.TempDir(), "missing", "", &buf); err == nil {
		t.Fatal("expected error for missing corpus, got nil")
	}
}
