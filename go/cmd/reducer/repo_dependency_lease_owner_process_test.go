// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const repoDependencyLeaseOwnerHelperEnv = "ESHU_TEST_REPO_DEPENDENCY_LEASE_OWNER_HELPER"

func TestRepoDependencyLeaseOwnerDiffersAcrossProcessBoots(t *testing.T) {
	if os.Getenv(repoDependencyLeaseOwnerHelperEnv) == "1" {
		fmt.Println(loadRepoDependencyProjectionConfig(func(string) string { return "" }).LeaseOwner)
		return
	}

	first := repoDependencyLeaseOwnerFromHelperProcess(t)
	second := repoDependencyLeaseOwnerFromHelperProcess(t)
	if first == second {
		t.Fatalf("lease owner reused across process boots: %q", first)
	}
	if !strings.HasPrefix(first, defaultRepoDependencyProjectionLeaseOwner+":") ||
		!strings.HasPrefix(second, defaultRepoDependencyProjectionLeaseOwner+":") {
		t.Fatalf("lease owner prefixes = %q / %q, want configured default prefix", first, second)
	}
}

func repoDependencyLeaseOwnerFromHelperProcess(t *testing.T) string {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestRepoDependencyLeaseOwnerDiffersAcrossProcessBoots$")
	command.Env = append(os.Environ(), repoDependencyLeaseOwnerHelperEnv+"=1")
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stdout
	if err := command.Run(); err != nil {
		t.Fatalf("lease owner helper process: %v: %s", err, stdout.String())
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, defaultRepoDependencyProjectionLeaseOwner+":") {
			return line
		}
	}
	t.Fatalf("lease owner helper output omitted owner: %q", stdout.String())
	return ""
}
