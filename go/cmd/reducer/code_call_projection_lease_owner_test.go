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

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const codeCallProjectionLeaseOwnerHelperEnv = "ESHU_TEST_CODE_CALL_PROJECTION_LEASE_OWNER_HELPER"

func TestCodeCallProjectionLeaseOwnerDiffersAcrossProcessBoots(t *testing.T) {
	if os.Getenv(codeCallProjectionLeaseOwnerHelperEnv) == "1" {
		fmt.Println(loadCodeCallProjectionConfig(func(string) string { return "" }).LeaseOwner)
		return
	}

	first := codeCallProjectionLeaseOwnerFromHelperProcess(t)
	second := codeCallProjectionLeaseOwnerFromHelperProcess(t)
	if first == second {
		t.Fatalf("code-call projection lease owner reused across process boots: %q", first)
	}
	firstParts := strings.Split(first, ":")
	secondParts := strings.Split(second, ":")
	firstNonce := firstParts[len(firstParts)-1]
	secondNonce := secondParts[len(secondParts)-1]
	if firstNonce == secondNonce {
		t.Fatalf("code-call projection boot nonce reused across process boots: %q", firstNonce)
	}
}

func codeCallProjectionLeaseOwnerFromHelperProcess(t *testing.T) string {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestCodeCallProjectionLeaseOwnerDiffersAcrossProcessBoots$")
	command.Env = append(os.Environ(), codeCallProjectionLeaseOwnerHelperEnv+"=1")
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		t.Fatalf("code-call projection lease owner helper process: %v: %s", err, output.String())
	}
	for _, line := range strings.Split(output.String(), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, reducer.DefaultCodeCallProjectionLeaseOwnerPrefix+":") {
			return line
		}
	}
	t.Fatalf("code-call projection lease owner helper output omitted owner: %q", output.String())
	return ""
}
