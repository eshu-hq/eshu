package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/proofofvalue"
)

func main() {
	repoRoot := flag.String("repo-root", defaultRepoRoot(), "repository root containing tests/fixtures")
	corpusName := flag.String("corpus", "product_truth/dead_iac", "corpus label recorded in the report")
	out := flag.String("out", "", "optional path to write the JSON evidence artifact")
	flag.Parse()

	if err := run(*repoRoot, *corpusName, *out, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "proof-of-value:", err)
		os.Exit(1)
	}
}

// run loads the corpus and ground truth, builds both strategies' answers,
// scores them, prints a human summary, and optionally writes the JSON artifact.
func run(repoRoot, corpusName, out string, w io.Writer) error {
	fixtureRoot := filepath.Join(repoRoot, "tests", "fixtures", "product_truth", "dead_iac")
	expectedPath := filepath.Join(repoRoot, "tests", "fixtures", "product_truth", "expected", "dead_iac.json")

	filesByRepo, err := loadCorpus(fixtureRoot)
	if err != nil {
		return err
	}
	truths, err := loadGroundTruth(expectedPath)
	if err != nil {
		return err
	}

	questions, predictions, err := proofofvalue.BuildRun(filesByRepo, truths)
	if err != nil {
		return err
	}
	report, err := proofofvalue.Score(corpusName, questions, predictions)
	if err != nil {
		return err
	}

	summary := renderReport(report)
	if out != "" {
		if err := writeArtifact(out, report); err != nil {
			return err
		}
		summary += fmt.Sprintf("\nwrote evidence artifact: %s\n", out)
	}
	if _, err := io.WriteString(w, summary); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

// renderReport formats a human-readable scorecard.
func renderReport(report proofofvalue.Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Proof-of-value harness: %s\n", report.Corpus)
	fmt.Fprintf(&b, "questions: %d\n\n", report.QuestionCount)
	fmt.Fprintf(&b, "%-14s  %8s  %8s  %9s  %6s  %6s  %6s\n",
		"strategy", "accuracy", "correct", "dead_prec", "d_rec", "dead_fp", "dead_fn")
	b.WriteString(renderStrategy("baseline_grep", report.Baseline))
	b.WriteString(renderStrategy("eshu", report.Eshu))
	b.WriteString("\ndelta (eshu - baseline):\n")
	fmt.Fprintf(&b, "  accuracy:                   %+.3f\n", report.Delta.AccuracyDelta)
	fmt.Fprintf(&b, "  dead precision:             %+.3f\n", report.Delta.DeadPrecisionDelta)
	fmt.Fprintf(&b, "  dead recall:                %+.3f\n", report.Delta.DeadRecallDelta)
	fmt.Fprintf(&b, "  dangerous mistakes avoided: %d\n", report.Delta.DangerousMistakesAvoided)
	return b.String()
}

func renderStrategy(name string, m proofofvalue.StrategyMetrics) string {
	return fmt.Sprintf("%-14s  %8.3f  %5d/%-2d  %9.3f  %6.3f  %6d  %6d\n",
		name, m.Accuracy, m.Correct, m.Total, m.DeadPrecision, m.DeadRecall, m.DeadFalsePositive, m.DeadFalseNegative)
}

// writeArtifact marshals the report as indented JSON to path.
func writeArtifact(path string, report proofofvalue.Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// defaultRepoRoot resolves the repository root from this command's working
// directory, assuming the standard go/cmd/proof-of-value location.
func defaultRepoRoot() string {
	if wd, err := os.Getwd(); err == nil {
		// When run via `go run ./cmd/proof-of-value` from go/, walk up to the
		// repo root that holds tests/fixtures.
		for dir := wd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			if _, err := os.Stat(filepath.Join(dir, "tests", "fixtures")); err == nil {
				return dir
			}
		}
	}
	return "."
}
