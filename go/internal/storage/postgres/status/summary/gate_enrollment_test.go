// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// gateStep is the part of a workflow step this guard reads.
type gateStep struct {
	Name string            `yaml:"name"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
}

const (
	contentionGateWorkflow = "../../../../../../.github/workflows/reducer-contention-gate.yml"
	summaryPackageCommand  = "./internal/storage/postgres/status/summary/"
)

// TestStatusSummaryProofsRunInTheReducerContentionGate is the hermetic
// enrollment guard for the live proofs in this package. They skip on a machine
// with no DSN, so a proof the blocking reducer contention gate never selects
// skips just as quietly in CI. The coupling is a test name matching a regular
// expression in a YAML file, which nothing else checks. This test reads the real
// workflow and requires the step that runs this package to set the admin DSN, the
// disposable opt-in, and the fail-closed variable, and to select every live test
// declared here.
func TestStatusSummaryProofsRunInTheReducerContentionGate(t *testing.T) {
	t.Parallel()

	workflow, err := os.ReadFile(filepath.FromSlash(contentionGateWorkflow))
	if err != nil {
		t.Fatalf("read %s: %v", contentionGateWorkflow, err)
	}
	step := summaryProofStep(t, workflow)
	for name, want := range map[string]string{
		"ESHU_REQUIRE_STATUS_SUMMARY_PROOF":    "1",
		"ESHU_STATUS_SUMMARY_PROOF_DISPOSABLE": "1",
	} {
		if step.Env[name] != want {
			t.Errorf("step %q must set %s=%q, has %q", step.Name, name, want, step.Env[name])
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(strings.SplitN(step.Env["ESHU_STATUS_SUMMARY_PROOF_DSN"], "?", 2)[0]), "/postgres") {
		t.Errorf("step %q must set ESHU_STATUS_SUMMARY_PROOF_DSN to the administrative postgres database, has %q",
			step.Name, step.Env["ESHU_STATUS_SUMMARY_PROOF_DSN"])
	}
	filter := regexp.MustCompile(`-run '([^']+)'`).FindStringSubmatch(step.Run)
	if len(filter) != 2 {
		t.Fatalf("step %q no longer runs a single-quoted -run filter: %s", step.Name, step.Run)
	}
	if missing := proofsMissingFromFilter(t, filter[1], liveTestNames(t)); len(missing) != 0 {
		t.Fatalf("the contention gate filter %q does not select %v", filter[1], missing)
	}
}

// TestProofsMissingFromFilterRejectsAnUnselectedProof proves the guard can fail:
// a filter that omits one live test reports exactly that test.
func TestProofsMissingFromFilterRejectsAnUnselectedProof(t *testing.T) {
	t.Parallel()

	names := liveTestNames(t)
	if len(names) < 2 {
		t.Fatalf("found %d live tests, want the package's proofs", len(names))
	}
	if missing := proofsMissingFromFilter(t, "Live$", names); len(missing) != 0 {
		t.Fatalf("the shipped filter shape must select every live test, missing %v", missing)
	}
	narrowed := "^(" + strings.Join(names[1:], "|") + ")$"
	missing := proofsMissingFromFilter(t, narrowed, names)
	if len(missing) != 1 || missing[0] != names[0] {
		t.Fatalf("a filter without %s reported %v, want exactly that test", names[0], missing)
	}
}

func summaryProofStep(t *testing.T, workflow []byte) gateStep {
	t.Helper()
	var parsed struct {
		Jobs map[string]struct {
			Steps []gateStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(workflow, &parsed); err != nil {
		t.Fatalf("parse the contention gate workflow: %v", err)
	}
	job, ok := parsed.Jobs["contention-gate"]
	if !ok {
		t.Fatal("the contention gate workflow has no contention-gate job")
	}
	var found []gateStep
	for _, step := range job.Steps {
		if strings.Contains(step.Run, summaryPackageCommand) {
			found = append(found, step)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the contention-gate job has %d steps that run %s, want exactly 1", len(found), summaryPackageCommand)
	}
	return found[0]
}

// proofsMissingFromFilter returns the names the -run filter does not select.
func proofsMissingFromFilter(t *testing.T, filter string, names []string) []string {
	t.Helper()
	selects, err := regexp.Compile(filter)
	if err != nil {
		t.Fatalf("compile -run filter %q: %v", filter, err)
	}
	var missing []string
	for _, name := range names {
		if !selects.MatchString(name) {
			missing = append(missing, name)
		}
	}
	return missing
}

// liveTestNames returns every Test function declared in this package's
// *_live_test.go files, sorted.
func liveTestNames(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*_live_test.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("find live test files: %v (%d files)", err, len(files))
	}
	var names []string
	for _, file := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, declaration := range parsed.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				names = append(names, fn.Name.Name)
			}
		}
	}
	sort.Strings(names)
	return names
}
