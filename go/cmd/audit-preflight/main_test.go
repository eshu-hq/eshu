// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestRunPassesCompletedDogfoodFixtures dogfoods the preflight against the three
// competitor examples (graphify, GitNexus, CodeGraphContext). Each is a complete,
// classified audit and must pass.
func TestRunPassesCompletedDogfoodFixtures(t *testing.T) {
	t.Parallel()
	fixtures := []string{
		"testdata/graphify-foundation-exists.md",
		"testdata/gitnexus-missing.md",
		"testdata/codegraphcontext-already-tracked.md",
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if err := run([]string{"-file", fixture}, nil, &stdout, &stderr); err != nil {
				t.Fatalf("run(%s) error = %v\nstdout:\n%s", fixture, err, stdout.String())
			}
			if !strings.Contains(stdout.String(), "audit preflight passed") {
				t.Fatalf("run(%s) output = %s", fixture, stdout.String())
			}
		})
	}
}

// TestRunFailsIncompleteFixture proves an audit lacking evidence and using an
// invalid gap class is rejected.
func TestRunFailsIncompleteFixture(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := run([]string{"-file", "testdata/incomplete.md"}, nil, &stdout, &stderr)
	if err == nil {
		t.Fatalf("run(incomplete) error = nil, want failure\nstdout:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "missing_field") {
		t.Fatalf("expected missing_field findings: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "invalid_gap_class") {
		t.Fatalf("expected invalid_gap_class finding: %s", stdout.String())
	}
}

// TestRunReadsStdin proves the default stdin path works.
func TestRunReadsStdin(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := run(nil, strings.NewReader("### Gap class\n\nmissing\n"), &stdout, &stderr)
	if err == nil {
		t.Fatal("run(stdin incomplete) error = nil, want failure")
	}
}
