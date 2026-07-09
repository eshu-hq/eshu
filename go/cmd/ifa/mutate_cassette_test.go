// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// writeTestCassette writes a small two-fact gcp_cloud_resource cassette to a
// temp file and returns its path, leaving the original committed testdata
// cassette untouched.
func writeTestCassette(t *testing.T) string {
	t.Helper()
	f := cassette.File{
		Collector:     "gcp",
		SchemaVersion: cassette.SchemaVersionV1,
		Scopes: []cassette.Scope{
			{
				ScopeID:       "gcp:project:demo",
				SourceSystem:  "gcp",
				ScopeKind:     "project",
				CollectorKind: "gcp",
				GenerationID:  "gen-1",
				ObservedAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				Facts: []cassette.Fact{
					{
						FactKind:      "gcp_cloud_resource",
						StableFactKey: "gcp:project:demo:a-resource",
						SchemaVersion: "1.0.0",
						Payload: map[string]any{
							"full_resource_name": "//example.com/a",
							"asset_type":         "example.googleapis.com/A",
						},
					},
				},
			},
		},
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal test cassette: %v", err)
	}
	path := filepath.Join(t.TempDir(), "source.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write test cassette: %v", err)
	}
	return path
}

func TestParseMutateCassetteFlagsRequiresEveryFlag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing cassette", []string{"-out", "o.json", "-fact-kind", "k", "-kind", "missing-field"}, "-cassette"},
		{"missing out", []string{"-cassette", "i.json", "-fact-kind", "k", "-kind", "missing-field"}, "-out"},
		{"missing fact-kind", []string{"-cassette", "i.json", "-out", "o.json", "-kind", "missing-field"}, "-fact-kind"},
		{"missing kind", []string{"-cassette", "i.json", "-out", "o.json", "-fact-kind", "k"}, "-kind"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			_, err := parseMutateCassetteFlags(tc.args, &stderr)
			if err == nil {
				t.Fatalf("parseMutateCassetteFlags(%v) = nil error, want an error naming %s", tc.args, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to name %s", err, tc.want)
			}
		})
	}
}

// TestRunMutateCassetteCommandProducesDeterministicOutput proves the CLI
// wrapper writes byte-identical mutated cassettes across repeated invocations
// with the same inputs, and that the written file actually carries the
// requested corruption.
func TestRunMutateCassetteCommandProducesDeterministicOutput(t *testing.T) {
	t.Parallel()

	src := writeTestCassette(t)
	out1 := filepath.Join(t.TempDir(), "mutated-1.json")
	out2 := filepath.Join(t.TempDir(), "mutated-2.json")

	var stdout1, stderr1 bytes.Buffer
	if err := runMutateCassetteCommand([]string{
		"-cassette", src,
		"-out", out1,
		"-fact-kind", "gcp_cloud_resource",
		"-kind", "schema-major",
		"-schema-major", "99.0.0",
	}, &stdout1, &stderr1); err != nil {
		t.Fatalf("runMutateCassetteCommand() error = %v", err)
	}

	var stdout2, stderr2 bytes.Buffer
	if err := runMutateCassetteCommand([]string{
		"-cassette", src,
		"-out", out2,
		"-fact-kind", "gcp_cloud_resource",
		"-kind", "schema-major",
		"-schema-major", "99.0.0",
	}, &stdout2, &stderr2); err != nil {
		t.Fatalf("runMutateCassetteCommand() second run error = %v", err)
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
		t.Fatalf("runMutateCassetteCommand() produced non-identical output across repeated runs")
	}

	mutated, err := cassette.LoadFile(out1)
	if err != nil {
		t.Fatalf("load mutated cassette: %v", err)
	}
	if got := mutated.Scopes[0].Facts[0].SchemaVersion; got != "99.0.0" {
		t.Fatalf("mutated fact schema_version = %q, want %q", got, "99.0.0")
	}

	if !strings.Contains(stdout1.String(), "gcp:project:demo:a-resource") {
		t.Errorf("stdout = %q, want it to name the mutated stable_fact_key", stdout1.String())
	}

	// The source cassette on disk must be untouched.
	srcData, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read source cassette: %v", err)
	}
	var srcFile cassette.File
	if err := json.Unmarshal(srcData, &srcFile); err != nil {
		t.Fatalf("unmarshal source cassette: %v", err)
	}
	if got := srcFile.Scopes[0].Facts[0].SchemaVersion; got != "1.0.0" {
		t.Fatalf("source cassette on disk was modified: schema_version = %q, want %q", got, "1.0.0")
	}
}

func TestRunMutateCassetteCommandMissingCassetteErrorsCleanly(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "does-not-exist.json")
	var stdout, stderr bytes.Buffer
	err := runMutateCassetteCommand([]string{
		"-cassette", missing,
		"-out", filepath.Join(t.TempDir(), "out.json"),
		"-fact-kind", "gcp_cloud_resource",
		"-kind", "schema-major",
		"-schema-major", "99.0.0",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("runMutateCassetteCommand(missing cassette) = nil error, want a load-cassette error")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error = %v, want it to name the missing cassette path %q", err, missing)
	}
}

// TestRunDispatchesMutateCassetteSubcommand proves the top-level run
// dispatcher wires "mutate-cassette" through to runMutateCassetteCommand.
func TestRunDispatchesMutateCassetteSubcommand(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"mutate-cassette"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run([]string{\"mutate-cassette\"}) = nil error, want the subcommand's -cassette-required error")
	}
	if !strings.Contains(err.Error(), "-cassette") {
		t.Errorf("error = %v, want the mutate-cassette subcommand's -cassette-required error", err)
	}
}
