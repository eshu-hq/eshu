package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/competitiveparity"
)

func TestCompetitiveParityValidateJSONUsesLiveInventory(t *testing.T) {
	var out bytes.Buffer
	cmd := newCompetitiveParityValidateCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--repo-root", "../../..", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("competitive parity validate error = %v", err)
	}
	var report competitiveparity.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode report JSON: %v\n%s", err, out.String())
	}
	if !report.Pass {
		t.Fatalf("report.Pass = false, want true: %#v", report.Surfaces)
	}
	if report.SchemaVersion != competitiveparity.SchemaVersion {
		t.Fatalf("report.SchemaVersion = %q", report.SchemaVersion)
	}
	if !reportHasPassedCheck(report, competitiveparity.CheckExercise, "operator_digest_artifact") {
		t.Fatalf("report missing passed operator digest exercise: %#v", report.Surfaces)
	}
}

func TestCompetitiveParityValidateMarkdownNamesPeerBaselines(t *testing.T) {
	var out bytes.Buffer
	cmd := newCompetitiveParityValidateCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--repo-root", "../../.."})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("competitive parity validate error = %v", err)
	}
	for _, want := range []string{
		"# Competitive Parity Gate",
		"graphify-style report readability",
		"CodeGraphContext-style portable artifact usability",
		"GitNexus-style agent workflow discoverability",
		"#3238",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("markdown output missing %q:\n%s", want, out.String())
		}
	}
}

func TestCompetitiveParityValidateReportsMissingDocs(t *testing.T) {
	var out bytes.Buffer
	cmd := newCompetitiveParityValidateCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--repo-root", t.TempDir(), "--json"})
	err := cmd.Execute()
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("competitive parity validate error = %T %v, want commandExitError", err, err)
	}
	if exitErr.ExitCode() != 1 {
		t.Fatalf("exitErr.ExitCode() = %d, want 1", exitErr.ExitCode())
	}
	var report competitiveparity.Report
	if decodeErr := json.Unmarshal(out.Bytes(), &report); decodeErr != nil {
		t.Fatalf("decode report JSON: %v\n%s", decodeErr, out.String())
	}
	if report.Pass {
		t.Fatal("report.Pass = true, want false")
	}
	if !reportHasFailedCheck(report, competitiveparity.CheckDoc, "docs/public/reference/capability-catalog.md") {
		t.Fatalf("report missing failed capability catalog doc check: %#v", report.Surfaces)
	}
}

func TestRootCommandIncludesCompetitiveParityValidate(t *testing.T) {
	if !commandPathExists("competitive-parity validate") {
		t.Fatal("root command missing competitive-parity validate")
	}
}

func commandPathExists(want string) bool {
	for _, path := range commandPaths(rootCmd) {
		if path == want {
			return true
		}
	}
	return false
}

func reportHasFailedCheck(report competitiveparity.Report, kind competitiveparity.CheckKind, target string) bool {
	for _, surface := range report.Surfaces {
		for _, check := range surface.Checks {
			if check.Kind == kind && check.Target == target && check.Status == competitiveparity.CheckFail {
				return true
			}
		}
	}
	return false
}

func reportHasPassedCheck(report competitiveparity.Report, kind competitiveparity.CheckKind, target string) bool {
	for _, surface := range report.Surfaces {
		for _, check := range surface.Checks {
			if check.Kind == kind && check.Target == target && check.Status == competitiveparity.CheckPass {
				return true
			}
		}
	}
	return false
}
