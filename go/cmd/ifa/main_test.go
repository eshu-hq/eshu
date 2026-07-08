// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"testing"
)

func TestRunVersionPrintsContractSkeleton(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if err := run([]string{"-version"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(-version) error = %v, stderr=%s", err, stderr.String())
	}
	if got := stdout.String(); got != "ifa: contract-layer skeleton\n" {
		t.Fatalf("stdout = %q, want contract skeleton banner", got)
	}
}
