// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func TestRunPrePRWholeModuleFlagExecutesUnselectedCore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	registry := writeRegistry(t, root, `version: v1
gates:
  - id: go-fmt
    name: Go format
    category: hygiene
    tier: pre-commit
    blocking: true
    triggers: ["go/**"]
    local: {command: "printf 'fmt\\n' >> fmt.log", test_command: ""}
    ci: {workflow: test.yml, job: go-core}
    requirements: []
    ci_only_reason: ""
  - id: go-lint
    name: Go lint
    category: hygiene
    tier: pre-commit
    blocking: true
    triggers: ["go/**"]
    local: {command: "printf 'lint\\n' >> lint.log", test_command: ""}
    ci: {workflow: test.yml, job: go-core}
    requirements: []
    ci_only_reason: ""
  - id: go-build
    name: Go build
    category: build
    tier: pre-push
    blocking: true
    triggers: ["go/**"]
    local: {command: "printf 'build\\n' >> build.log", test_command: ""}
    ci: {workflow: test.yml, job: go-core}
    requirements: []
    ci_only_reason: ""
  - id: go-vet
    name: Go vet
    category: hygiene
    tier: pre-push
    blocking: true
    triggers: ["go/**"]
    local: {command: "printf 'vet\\n' >> vet.log", test_command: ""}
    ci: {workflow: test.yml, job: go-core}
    requirements: []
    ci_only_reason: ""
  - id: later-gate
    name: Later exactness gate
    category: exactness
    tier: pre-pr
    blocking: true
    triggers: ["docs/**"]
    local: {command: "printf 'later\\n' >> later.log", test_command: ""}
    ci: {workflow: test.yml, job: later}
    requirements: []
    ci_only_reason: ""
`)
	paths := writePathsFile(t, root, []string{"docs/readme.md"})
	reportPath := filepath.Join(root, "report.json")
	cmd := exec.Command(
		buildBinary(t),
		"run",
		"--registry", registry,
		"--tier", "pre-pr",
		"--paths-from", paths,
		"--repo-root", root,
		"--category", "exactness",
		"--pre-pr-whole-module",
		"--report-file", reportPath,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ci-gates run error = %v:\n%s", err, output)
	}
	for _, name := range []string{"fmt", "lint", "build", "vet", "later"} {
		assertFile(t, root, name+".log", name+"\n")
	}
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var report gateRunReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if !report.PrePRWholeModule || report.Summary.CommandsRun != 5 {
		t.Fatalf("report = %+v, want whole-module mode and five executions", report)
	}
}

func TestPrePRWholeModuleRunsCoreOnceAndReusesSelectedResults(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	selections := prePRCoreSelections(true, map[string]string{
		"go-fmt": "i=0; while [ ! -f build.started ] || [ ! -f vet.started ]; do " +
			"i=$((i + 1)); [ \"$i\" -lt 500 ] || exit 31; sleep 0.01; done; " +
			"touch fmt.done; printf 'fmt\\n' >> trace.log",
		"go-lint": "test -f fmt.done && touch lint.started && printf 'lint\\n' >> trace.log",
		"go-build": "touch build.started; i=0; while [ ! -f lint.started ]; do " +
			"i=$((i + 1)); [ \"$i\" -lt 500 ] || exit 32; sleep 0.01; done; " +
			"printf 'build\\n' >> build.log",
		"go-vet": "touch vet.started; i=0; while [ ! -f lint.started ]; do " +
			"i=$((i + 1)); [ \"$i\" -lt 500 ] || exit 33; sleep 0.01; done; " +
			"printf 'vet\\n' >> vet.log",
	})
	// The production category filter excludes go-build from the selected
	// hygiene/exactness lane; the forced prelude still owns and reports it.
	selections[2].Selected = false
	selections[2].Reason = "category build not requested"
	selections = append(selections, selectedGateWithCI(
		"package-docs",
		"printf 'package-docs\\n' >> package-docs.log",
		true,
		"test.yml",
		"verify-contracts",
	))

	var output bytes.Buffer
	report, err := executeGatesWithOptions(&output, selections, root, executeOptions{
		selfTests:        selfTestsChanged,
		blockingOnly:     true,
		prePRWholeModule: true,
	})
	if err != nil {
		t.Fatalf("executeGatesWithOptions() error = %v\n%s", err, output.String())
	}
	assertTrace(t, root, "fmt\nlint\n")
	assertFile(t, root, "build.log", "build\n")
	assertFile(t, root, "vet.log", "vet\n")
	assertFile(t, root, "package-docs.log", "package-docs\n")
	for _, id := range []string{"go-fmt", "go-lint", "go-vet"} {
		if !strings.Contains(output.String(), "REUSE   "+id+":") {
			t.Errorf("output did not report %s reuse:\n%s", id, output.String())
		}
	}
	if !strings.Contains(output.String(), "WHOLE   pre-pr: fmt then lint; build and vet run in parallel") {
		t.Fatalf("output did not announce the parallel whole-module work:\n%s", output.String())
	}
	if report.Summary.CommandsRun != 5 || report.Summary.CommandsReused != 3 {
		t.Fatalf("summary = %+v, want 5 executions and 3 selected-row reuses", report.Summary)
	}
	if !report.PrePRWholeModule {
		t.Fatal("report did not record pre-pr whole-module mode")
	}
}

func TestPrePRWholeModuleRunsCoreWhenPathSelectionSkippedIt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	selections := prePRCoreSelections(false, map[string]string{
		"go-fmt":   "printf 'fmt\\n' >> fmt.log",
		"go-lint":  "printf 'lint\\n' >> lint.log",
		"go-build": "printf 'build\\n' >> build.log",
		"go-vet":   "printf 'vet\\n' >> vet.log",
	})

	var output bytes.Buffer
	report, err := executeGatesWithOptions(&output, selections, root, executeOptions{
		selfTests:        selfTestsChanged,
		blockingOnly:     true,
		prePRWholeModule: true,
	})
	if err != nil {
		t.Fatalf("executeGatesWithOptions() error = %v\n%s", err, output.String())
	}
	for _, id := range prePRWholeModuleGateIDs {
		assertFile(t, root, strings.TrimPrefix(id, "go-")+".log", strings.TrimPrefix(id, "go-")+"\n")
	}
	if report.Summary.CommandsRun != 4 || report.Summary.CommandsReused != 0 {
		t.Fatalf("summary = %+v, want four forced executions and no reuse", report.Summary)
	}
}

func TestPrePRWholeModuleFailureRemainsBlockingAndDoesNotStopLaterGates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		selected bool
	}{
		{name: "forced-unselected", selected: false},
		{name: "selected-reuse", selected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			selections := prePRCoreSelections(true, map[string]string{
				"go-fmt":   "true",
				"go-lint":  "true",
				"go-build": "true",
				"go-vet":   "printf 'vet\\n' >> vet.log; exit 19",
			})
			selections[3].Selected = tc.selected
			if !tc.selected {
				selections[3].Reason = "no changed Go path selected it"
			}
			selections = append(selections, selectedGateWithCI(
				"later-gate",
				"printf 'later\\n' >> later.log",
				true,
				"later.yml",
				"later",
			))

			var output bytes.Buffer
			report, err := executeGatesWithOptions(&output, selections, root, executeOptions{
				selfTests:        selfTestsChanged,
				blockingOnly:     true,
				prePRWholeModule: true,
			})
			if err == nil {
				t.Fatalf("executeGatesWithOptions() error = nil, want blocking prelude failure\n%s", output.String())
			}
			assertFile(t, root, "vet.log", "vet\n")
			assertFile(t, root, "later.log", "later\n")
			if report.Summary.BlockingFailures != 1 {
				t.Fatalf("summary = %+v, want one logical blocking failure", report.Summary)
			}
			if !strings.Contains(output.String(), "FAIL     go-vet (blocking)") {
				t.Fatalf("output did not retain the prelude failure:\n%s", output.String())
			}
			if tc.selected && !strings.Contains(output.String(), "REUSE   go-vet:") {
				t.Fatalf("selected failing core result was not reused:\n%s", output.String())
			}
		})
	}
}

func TestPrePRWholeModuleMissingCoreGateFailsClosed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	selections := prePRCoreSelections(false, map[string]string{
		"go-fmt":   "touch must-not-run",
		"go-lint":  "touch must-not-run",
		"go-build": "touch must-not-run",
		"go-vet":   "touch must-not-run",
	})
	selections = selections[:len(selections)-1]

	var output bytes.Buffer
	_, err := executeGatesWithOptions(&output, selections, root, executeOptions{
		selfTests:        selfTestsChanged,
		blockingOnly:     true,
		prePRWholeModule: true,
	})
	if err == nil || !strings.Contains(err.Error(), `gate "go-vet" is missing`) {
		t.Fatalf("error = %v, want missing go-vet failure", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "must-not-run")); !os.IsNotExist(statErr) {
		t.Fatalf("prelude command ran before validation completed: stat error = %v", statErr)
	}
}

func TestPrePRWholeModuleRejectsCIOnlyCoreGateBeforeExecution(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	selections := prePRCoreSelections(false, map[string]string{
		"go-fmt":   "touch must-not-run",
		"go-lint":  "touch must-not-run",
		"go-build": "touch must-not-run",
		"go-vet":   "touch must-not-run",
	})
	selections[3].Gate.CIOnlyReason = "hosted only"

	var output bytes.Buffer
	_, err := executeGatesWithOptions(&output, selections, root, executeOptions{
		selfTests:        selfTestsChanged,
		blockingOnly:     true,
		prePRWholeModule: true,
	})
	if err == nil || !strings.Contains(err.Error(), `gate "go-vet" must be locally runnable`) {
		t.Fatalf("error = %v, want CI-only go-vet failure", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "must-not-run")); !os.IsNotExist(statErr) {
		t.Fatalf("prelude command ran before validation completed: stat error = %v", statErr)
	}
}

func prePRCoreSelections(selected bool, commands map[string]string) []cigates.Selection {
	selections := make([]cigates.Selection, 0, len(prePRWholeModuleGateIDs))
	for _, id := range prePRWholeModuleGateIDs {
		selection := selectedGateWithCI(id, commands[id], true, "test.yml", "go-core")
		selection.Selected = selected
		if !selected {
			selection.Reason = "outside selected category"
		}
		selections = append(selections, selection)
	}
	return selections
}

func assertFile(t *testing.T, root, name, want string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if string(raw) != want {
		t.Fatalf("%s = %q, want %q", name, raw, want)
	}
}
