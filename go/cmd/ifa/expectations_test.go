// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestRunExpectationsPrintsJSONForOneKind(t *testing.T) {
	repoRoot := repoRootDir(t)

	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"expectations",
		"-specs-dir", filepath.Join(repoRoot, "specs"),
		"-snapshot", filepath.Join(repoRoot, "testdata", "golden", "e2e-20repo-snapshot.json"),
		"-kind", "aws_resource",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run(expectations -kind aws_resource) = %v, stderr=%s", err, stderr.String())
	}

	var decoded map[string]any
	if jsonErr := json.Unmarshal(stdout.Bytes(), &decoded); jsonErr != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", jsonErr, stdout.String())
	}
	if decoded["Kind"] != "aws_resource" {
		t.Errorf("decoded Kind = %v, want aws_resource\nstdout: %s", decoded["Kind"], stdout.String())
	}
}

func TestRunExpectationsUnknownKindErrors(t *testing.T) {
	repoRoot := repoRootDir(t)

	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"expectations",
		"-specs-dir", filepath.Join(repoRoot, "specs"),
		"-snapshot", filepath.Join(repoRoot, "testdata", "golden", "e2e-20repo-snapshot.json"),
		"-kind", "no_such_kind",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(expectations -kind no_such_kind) = nil error, want an error naming the missing kind")
	}
}
