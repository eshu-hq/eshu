package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/auditreport"
)

var update = flag.Bool("update", false, "update golden files")

const goldenPath = "testdata/expected-report.md"

// TestRunMarkdownMatchesGolden dogfoods the generator on the three competitor
// fixtures and pins the deterministic Markdown report to a golden file.
func TestRunMarkdownMatchesGolden(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{
		"-input", "testdata/audit-input.yaml",
		"-issues", "testdata/open-issues.json",
		"-format", "md",
	}
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v\nstderr:\n%s", err, stderr.String())
	}
	got := stdout.String()
	if *update {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got != string(want) {
		t.Fatalf("report mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// TestRunJSONClassifies asserts the JSON report carries the expected
// recommendations for the dogfood fixtures.
func TestRunJSONClassifies(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{
		"-input", "testdata/audit-input.yaml",
		"-issues", "testdata/open-issues.json",
		"-format", "json",
	}
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	var report auditreport.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	byFeature := map[string]auditreport.ReportEntry{}
	for _, entry := range report.Entries {
		byFeature[entry.Feature] = entry
	}
	if got := byFeature["symbol relationship lookup"]; got.Recommendation != auditreport.RecNoIssue || !got.CapabilityFound {
		t.Fatalf("symbol lookup = %+v", got)
	}
	if got := byFeature["commit history timeline"]; got.Recommendation != auditreport.RecLinkExisting || len(got.DuplicateIssues) == 0 {
		t.Fatalf("commit timeline = %+v", got)
	}
}

func TestRunRequiresInput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-format", "json"}, &stdout, &stderr); err == nil {
		t.Fatal("run() error = nil, want missing -input error")
	}
}

func TestRunRejectsUnknownFormat(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-input", "testdata/audit-input.yaml", "-format", "xml"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("err = %v, want unsupported format", err)
	}
}
