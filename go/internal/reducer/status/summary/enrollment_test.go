// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// contentionGateWorkflow is the blocking workflow that runs this package's
// live proofs against its PostgreSQL service.
var contentionGateWorkflow = filepath.Join("..", "..", "..", "..", "..", ".github", "workflows", "reducer-contention-gate.yml")

// packageTestPath is how the gate's go test invocation names this package.
const packageTestPath = "./internal/reducer/status/summary/"

// workflowStep is the part of a GitHub Actions step the guard reads.
type workflowStep struct {
	Name string            `yaml:"name"`
	Env  map[string]string `yaml:"env"`
	Run  string            `yaml:"run"`
}

// TestWriterLiveProofsRunInTheReducerContentionGate is the enrollment guard
// for this package's live proofs. They skip without a DSN, and the coupling
// between a test name and the gate's -run filter is a regular expression in a
// YAML file that nothing else checks. This test reads the real workflow and
// the real live test files: every live proof must be selected by the step
// that runs this package, and that step must pass the DSN, the disposable
// opt-in, and the skip-is-failure switch. It needs no database.
func TestWriterLiveProofsRunInTheReducerContentionGate(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(contentionGateWorkflow)
	if err != nil {
		t.Fatalf("read %s: %v", contentionGateWorkflow, err)
	}
	step, err := packageProofStep(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProofStepEnv(step); err != nil {
		t.Fatal(err)
	}
	filter, err := runFilter(step.Run)
	if err != nil {
		t.Fatal(err)
	}
	proofs := declaredLiveProofs(t)
	if len(proofs) < 8 {
		t.Fatalf("found %d live proofs %v, want at least the 8 the README lists", len(proofs), proofs)
	}
	if missing := proofsMissingFromFilter(filter, proofs); len(missing) > 0 {
		t.Fatalf("the reducer contention gate's -run filter %q does not select %v", filter, missing)
	}
}

// TestEnrollmentGuardRejectsAGapInTheGate is the seeded RED/GREEN pair for
// the guard: a filter that misses a proof, and a step that drops any of the
// three environment settings, must be reported; a complete step must not.
func TestEnrollmentGuardRejectsAGapInTheGate(t *testing.T) {
	t.Parallel()
	proofs := []string{"TestWriterRowEqualsLiveStatementLive", "TestWritersBesideTheProductionClaimLoopLive"}
	if missing := proofsMissingFromFilter("^(TestWriterRowEqualsLiveStatementLive)$", proofs); len(missing) != 1 {
		t.Fatalf("RED: a filter missing one proof reported %v, want exactly that proof", missing)
	}
	if missing := proofsMissingFromFilter("^(TestWriterRowEqualsLiveStatementLive|TestWritersBesideTheProductionClaimLoopLive)$", proofs); len(missing) != 0 {
		t.Fatalf("GREEN: a complete filter reported %v", missing)
	}
	complete := workflowStep{Env: map[string]string{proofDSNEnv: "postgres://x/postgres", proofDisposableEnv: "1", proofRequiredEnv: "1"}}
	if err := checkProofStepEnv(complete); err != nil {
		t.Fatalf("GREEN: a complete step was rejected: %v", err)
	}
	for _, drop := range []string{proofDSNEnv, proofDisposableEnv, proofRequiredEnv} {
		env := map[string]string{}
		for key, value := range complete.Env {
			if key != drop {
				env[key] = value
			}
		}
		if err := checkProofStepEnv(workflowStep{Env: env}); err == nil {
			t.Fatalf("RED: a step without %s was accepted", drop)
		}
	}
	if _, err := packageProofStep([]byte("jobs:\n  gate:\n    steps:\n      - run: go test ./internal/other/\n")); err == nil {
		t.Fatal("RED: a workflow with no step for this package was accepted")
	}
}

// TestLiveProofMissingDSNFailsOnlyWhenRequired pins the skip-is-failure
// decision the live helper makes before it opens a database.
func TestLiveProofMissingDSNFailsOnlyWhenRequired(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		dsn, required string
		fails         bool
	}{
		{"", "1", true},
		{" ", "1", true},
		{"", "", false},
		{"", "0", false},
		{"postgres://x/postgres", "1", false},
	} {
		if got := liveProofMissingDSNFails(tc.dsn, tc.required); got != tc.fails {
			t.Fatalf("liveProofMissingDSNFails(%q, %q) = %v, want %v", tc.dsn, tc.required, got, tc.fails)
		}
	}
}

// packageProofStep returns the one workflow step whose run line tests this
// package.
func packageProofStep(raw []byte) (workflowStep, error) {
	var workflow struct {
		Jobs map[string]struct {
			Steps []workflowStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return workflowStep{}, fmt.Errorf("parse workflow: %w", err)
	}
	var found []workflowStep
	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if strings.Contains(step.Run, "go test "+packageTestPath) {
				found = append(found, step)
			}
		}
	}
	if len(found) != 1 {
		return workflowStep{}, fmt.Errorf("found %d steps running go test %s, want exactly 1", len(found), packageTestPath)
	}
	return found[0], nil
}

// checkProofStepEnv requires the DSN, the disposable opt-in, and the
// skip-is-failure switch on the step.
func checkProofStepEnv(step workflowStep) error {
	if !strings.HasSuffix(strings.SplitN(step.Env[proofDSNEnv], "?", 2)[0], "/postgres") {
		return fmt.Errorf("step %q must set %s to the administrative postgres database", step.Name, proofDSNEnv)
	}
	if step.Env[proofDisposableEnv] != "1" {
		return fmt.Errorf("step %q must set %s=1", step.Name, proofDisposableEnv)
	}
	if step.Env[proofRequiredEnv] != "1" {
		return fmt.Errorf("step %q must set %s=1 so an unset DSN fails instead of skipping", step.Name, proofRequiredEnv)
	}
	return nil
}

// runFilter extracts the -run pattern from a go test command line.
func runFilter(run string) (string, error) {
	match := regexp.MustCompile(`-run '([^']+)'`).FindStringSubmatch(run)
	if match == nil {
		return "", fmt.Errorf("go test line %q has no quoted -run filter", run)
	}
	return match[1], nil
}

// declaredLiveProofs lists the top-level Test functions in this package's
// *_live_test.go files.
func declaredLiveProofs(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*_live_test.go")
	if err != nil {
		t.Fatalf("glob live test files: %v", err)
	}
	declaration := regexp.MustCompile(`(?m)^func (Test\w+)\(t \*testing\.T\)`)
	var proofs []string
	for _, file := range files {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, match := range declaration.FindAllStringSubmatch(string(source), -1) {
			proofs = append(proofs, match[1])
		}
	}
	sort.Strings(proofs)
	return proofs
}

// proofsMissingFromFilter returns the proofs the filter does not select.
func proofsMissingFromFilter(filter string, proofs []string) []string {
	pattern := regexp.MustCompile(filter)
	var missing []string
	for _, proof := range proofs {
		if !pattern.MatchString(proof) {
			missing = append(missing, proof)
		}
	}
	return missing
}
