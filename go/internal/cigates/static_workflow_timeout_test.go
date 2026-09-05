// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// static-contract-gates.yml runs every gate through ONE matrix job, so its
// timeout is per-gate data rather than a literal on the job: append_gate takes
// an optional sixth argument and the job reads matrix.timeout. A gate that
// needs longer than the default raises only its own bound, and a genuine hang
// in any other gate is still cancelled on time.
//
// Only the timeout FIRING is Actions-only. The wiring that carries it is
// ordinary data and is asserted here, so a row that silently loses its timeout
// key -- which would make the job's timeout-minutes expression evaluate to
// nothing -- fails locally instead of in a runner.

var staticContractFilterOutputRE = regexp.MustCompile(`\$\{\{\s*steps\.filter\.outputs\.[A-Za-z0-9_]+\s*\}\}`)

// staticContractMatrixRows runs the workflow's real matrix builder with every
// path filter selected, and returns the rows it emits.
func staticContractMatrixRows(t *testing.T, raw []byte) []struct {
	Key     string `json:"key"`
	Display string `json:"display"`
	Timeout int    `json:"timeout"`
} {
	t.Helper()
	builder := staticContractMatrixBuilder(t, raw)
	script := staticContractFilterOutputRE.ReplaceAllString(builder, "true")
	script = strings.ReplaceAll(script, "${{ github.event_name }}", "pull_request")
	if strings.Contains(script, "${{") {
		t.Fatalf("matrix builder still carries an unsubstituted Actions expression:\n%s", script)
	}

	outputPath := filepath.Join(t.TempDir(), "output")
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append(os.Environ(), "GITHUB_OUTPUT="+outputPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run matrix builder: %v\n%s", err, output)
	}
	raw2, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read matrix builder output: %v", err)
	}
	var line string
	for _, candidate := range strings.Split(string(raw2), "\n") {
		if strings.HasPrefix(candidate, "matrix=") {
			line = strings.TrimPrefix(candidate, "matrix=")
		}
	}
	if line == "" {
		t.Fatalf("matrix builder emitted no matrix= line: %s", raw2)
	}
	var matrix struct {
		Include []struct {
			Key     string `json:"key"`
			Display string `json:"display"`
			Timeout int    `json:"timeout"`
		} `json:"include"`
	}
	if err := json.Unmarshal([]byte(line), &matrix); err != nil {
		t.Fatalf("decode matrix %q: %v", line, err)
	}
	if len(matrix.Include) == 0 {
		t.Fatal("matrix builder emitted no rows")
	}
	return matrix.Include
}

// TestStaticContractGateRowsAllCarryATimeout asserts every append_gate emits
// the key the job's timeout-minutes expression reads. A row missing it makes
// that expression empty, which fails at the runner rather than here.
func TestStaticContractGateRowsAllCarryATimeout(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repositoryRoot(t), ".github", "workflows", "static-contract-gates.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read static contract workflow: %v", err)
	}
	rows := staticContractMatrixRows(t, raw)
	for _, row := range rows {
		if row.Timeout <= 0 {
			t.Fatalf("gate %q emits timeout %d; every append_gate must carry one, or matrix.timeout is empty at the runner",
				row.Key, row.Timeout)
		}
	}
	t.Logf("gate rows carrying a timeout: %d", len(rows))
}

// TestStaticContractGateJobReadsTheMatrixTimeout is the other half: the wiring
// is only real if the job consumes it. A literal here would silently pin every
// gate to one bound again.
func TestStaticContractGateJobReadsTheMatrixTimeout(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repositoryRoot(t), ".github", "workflows", "static-contract-gates.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read static contract workflow: %v", err)
	}
	root, err := workflowRoot(raw)
	if err != nil {
		t.Fatalf("parse static contract workflow: %v", err)
	}
	jobs, err := mappingValue(root, "jobs")
	if err != nil {
		t.Fatal(err)
	}
	gate, err := mappingValue(jobs, "gate")
	if err != nil {
		t.Fatal(err)
	}
	timeout, err := mappingValue(gate, "timeout-minutes")
	if err != nil {
		t.Fatalf("gate job has no timeout-minutes: %v", err)
	}
	if got, want := strings.TrimSpace(timeout.Value), "${{ matrix.timeout }}"; got != want {
		t.Fatalf("gate job timeout-minutes = %q, want %q: a literal pins all gates to one bound", got, want)
	}
}

// TestStaticContractTaggedBuildsGetsTheLongerTimeout pins the exception and the
// default together, so neither can drift without the other being noticed. The
// tagged-builds sweep vets every //go:build configuration in the module
// serially and was being cancelled at the shared 15-minute bound.
func TestStaticContractTaggedBuildsGetsTheLongerTimeout(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repositoryRoot(t), ".github", "workflows", "static-contract-gates.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read static contract workflow: %v", err)
	}
	rows := staticContractMatrixRows(t, raw)
	var taggedBuilds, defaults int
	for _, row := range rows {
		switch row.Key {
		case "taggedbuilds":
			taggedBuilds++
			if row.Timeout != 30 {
				t.Fatalf("taggedbuilds timeout = %d, want 30", row.Timeout)
			}
		default:
			if row.Timeout != 15 {
				t.Fatalf("gate %q timeout = %d, want the default 15; raise one gate at a time, not the matrix",
					row.Key, row.Timeout)
			}
			defaults++
		}
	}
	if taggedBuilds != 1 {
		t.Fatalf("taggedbuilds rows = %d, want 1", taggedBuilds)
	}
	if defaults == 0 {
		t.Fatal("no gate is on the default timeout; the default arm of append_gate is unexercised")
	}
}
