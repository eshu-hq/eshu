// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunVersionPrintsContractSkeleton(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), []string{"-version"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(-version) error = %v, stderr=%s", err, stderr.String())
	}
	if got := stdout.String(); got != "ifa: contract-layer skeleton\n" {
		t.Fatalf("stdout = %q, want contract skeleton banner", got)
	}
}

func TestRunUnknownSubcommandPrintsUsageAndErrors(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"bogus-subcommand"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(bogus-subcommand) = nil error, want an error naming the unknown subcommand")
	}
	if !strings.Contains(err.Error(), "bogus-subcommand") {
		t.Errorf("error = %v, want it to name the unknown subcommand", err)
	}
	if stderr.Len() == 0 {
		t.Error("stderr is empty, want usage output for an unknown subcommand")
	}
}
