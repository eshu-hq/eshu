// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

func TestParseSynthCassetteFlagsRequiresOut(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	_, err := parseSynthCassetteFlags([]string{"-seed", "1", "-projects", "2", "-resources", "3"}, &stderr)
	if err == nil {
		t.Fatal("parseSynthCassetteFlags without -out = nil error, want an error naming -out")
	}
	if !strings.Contains(err.Error(), "-out") {
		t.Errorf("error = %v, want it to name -out", err)
	}
}

func TestParseSynthCassetteFlagsAppliesDefaults(t *testing.T) {
	t.Parallel()

	o, err := parseSynthCassetteFlags([]string{"-out", "x.json"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseSynthCassetteFlags: %v", err)
	}
	if o.projects != synthCassetteDefaultProjects {
		t.Errorf("projects = %d, want default %d", o.projects, synthCassetteDefaultProjects)
	}
	if o.resources != synthCassetteDefaultResources {
		t.Errorf("resources = %d, want default %d", o.resources, synthCassetteDefaultResources)
	}
	if o.seed != 0 {
		t.Errorf("seed = %d, want default 0", o.seed)
	}
}

// TestRunSynthCassetteCommandProducesDeterministicOutput proves the CLI
// wrapper writes byte-identical cassettes across repeated invocations with
// the same inputs, and that the written file actually validates as a v1
// cassette with the requested number of scopes.
func TestRunSynthCassetteCommandProducesDeterministicOutput(t *testing.T) {
	t.Parallel()

	out1 := filepath.Join(t.TempDir(), "synth-1.json")
	out2 := filepath.Join(t.TempDir(), "synth-2.json")

	var stdout1, stderr1 bytes.Buffer
	if err := runSynthCassetteCommand([]string{
		"-seed", "4396",
		"-projects", "3",
		"-resources", "5",
		"-out", out1,
	}, &stdout1, &stderr1); err != nil {
		t.Fatalf("runSynthCassetteCommand() error = %v", err)
	}

	var stdout2, stderr2 bytes.Buffer
	if err := runSynthCassetteCommand([]string{
		"-seed", "4396",
		"-projects", "3",
		"-resources", "5",
		"-out", out2,
	}, &stdout2, &stderr2); err != nil {
		t.Fatalf("runSynthCassetteCommand() second run error = %v", err)
	}

	data1, err := os.ReadFile(out1)
	if err != nil {
		t.Fatalf("read %s: %v", out1, err)
	}
	data2, err := os.ReadFile(out2)
	if err != nil {
		t.Fatalf("read %s: %v", out2, err)
	}
	if !bytes.Equal(data1, data2) {
		t.Fatal("runSynthCassetteCommand() produced non-identical output across repeated runs with identical inputs")
	}

	file, err := cassette.LoadFile(out1)
	if err != nil {
		t.Fatalf("load generated cassette: %v", err)
	}
	if len(file.Scopes) != 3 {
		t.Fatalf("len(file.Scopes) = %d, want 3", len(file.Scopes))
	}

	if !strings.Contains(stdout1.String(), "scopes=3") {
		t.Errorf("stdout = %q, want it to report scopes=3", stdout1.String())
	}
}

// TestRunSynthCassetteCommandDifferentSeedsProduceDifferentOutput guards
// against a flag-wiring regression where -seed is parsed but silently
// ignored.
func TestRunSynthCassetteCommandDifferentSeedsProduceDifferentOutput(t *testing.T) {
	t.Parallel()

	out1 := filepath.Join(t.TempDir(), "synth-seed-1.json")
	out2 := filepath.Join(t.TempDir(), "synth-seed-2.json")

	if err := runSynthCassetteCommand([]string{"-seed", "1", "-projects", "2", "-resources", "4", "-out", out1}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runSynthCassetteCommand(seed=1): %v", err)
	}
	if err := runSynthCassetteCommand([]string{"-seed", "2", "-projects", "2", "-resources", "4", "-out", out2}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runSynthCassetteCommand(seed=2): %v", err)
	}

	data1, err := os.ReadFile(out1)
	if err != nil {
		t.Fatalf("read %s: %v", out1, err)
	}
	data2, err := os.ReadFile(out2)
	if err != nil {
		t.Fatalf("read %s: %v", out2, err)
	}
	if bytes.Equal(data1, data2) {
		t.Fatal("runSynthCassetteCommand(seed=1) and (seed=2) produced identical bytes; -seed is not wired through")
	}
}

// TestRunSynthCassetteCommandRejectsNonPositiveProjects proves a bad
// -projects value fails fast with no file written, mirroring
// GenerateMultiScope's own fail-closed validation.
func TestRunSynthCassetteCommandRejectsNonPositiveProjects(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "should-not-exist.json")
	err := runSynthCassetteCommand([]string{"-projects", "0", "-resources", "4", "-out", out}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("runSynthCassetteCommand(-projects 0) = nil error, want a fail-closed validation error")
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Error("runSynthCassetteCommand(-projects 0) wrote an output file despite failing validation")
	}
}

// TestRunDispatchesSynthCassetteSubcommand proves the top-level run
// dispatcher wires "synth-cassette" through to runSynthCassetteCommand.
func TestRunDispatchesSynthCassetteSubcommand(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "dispatch.json")
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"synth-cassette", "-projects", "1", "-resources", "2", "-out", out}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run([]string{\"synth-cassette\", ...}) error = %v", err)
	}
	if _, statErr := os.Stat(out); statErr != nil {
		t.Errorf("run() did not write the expected output file: %v", statErr)
	}
}
