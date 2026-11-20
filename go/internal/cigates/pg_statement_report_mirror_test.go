// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestPgStatementReportContract is the converted pg-statement-report gate
// (#10988, part of #9315): it executes the pg-statement-report stub-psql
// contract suite — read-only discipline, no-DDL-without-install,
// privacy filter, not-preloaded behavior — under `go test`, so test.yml and
// the re-pointed static-contract cell share one runner. The script stays the
// assertion body; the Go test is the invocation contract. A subject-script
// change that breaks the stub contract fails here exactly as it failed the
// old gate cell.
func TestPgStatementReportContract(t *testing.T) {
	root := repositoryRoot(t)
	script := filepath.Join(root, "scripts", "test-pg-statement-report.sh")

	// Fail closed on a hang: the suite normally finishes in seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", script)
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("pg-statement-report contract suite failed: %v\n%s", err, out.String())
	}
}
