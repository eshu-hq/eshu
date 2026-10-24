// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
)

// transferProof is the compact, machine-readable publisher status contract.
// The status SHA supplies its head; the referenced run supplies repo, PR and
// workflow identity. A reuse status carries its full-run root so chains do not
// require an unbounded walk through intermediate heads.
type transferProof struct {
	Mode        string
	Input       testedInput
	FullRootSHA string
}

var transferProofPattern = regexp.MustCompile(`^v3 (full|reuse|smoke) m=([0-9a-f]{40}) r=([1-9][0-9]*) a=([1-9][0-9]*)(?: s=([0-9a-f]{40}))?$`)

func formatTransferProof(proof transferProof) (string, error) {
	if proof.Mode != "full" && proof.Mode != "reuse" && proof.Mode != "smoke" {
		return "", fmt.Errorf("unsupported transfer proof mode %q", proof.Mode)
	}
	if proof.Input.RunID <= 0 || proof.Input.RunID > 1_000_000_000_000_000 || proof.Input.Attempt <= 0 || proof.Input.Attempt > 1000 || !fullLowerSHA(proof.Input.MergeSHA) {
		return "", fmt.Errorf("transfer proof lacks exact invocation and merge")
	}
	if proof.Mode == "full" && proof.FullRootSHA != "" || proof.Mode != "full" && !fullLowerSHA(proof.FullRootSHA) {
		return "", fmt.Errorf("transfer proof has invalid full-run root")
	}
	description := fmt.Sprintf("v3 %s m=%s r=%d a=%d", proof.Mode, proof.Input.MergeSHA, proof.Input.RunID, proof.Input.Attempt)
	if proof.Mode != "full" {
		description += " s=" + proof.FullRootSHA
	}
	if len(description) > 140 {
		return "", fmt.Errorf("transfer proof exceeds commit-status description limit")
	}
	return description, nil
}

func parseTransferProof(description string) (transferProof, bool) {
	match := transferProofPattern.FindStringSubmatch(description)
	if match == nil {
		return transferProof{}, false
	}
	runID, err := strconv.ParseInt(match[3], 10, 64)
	if err != nil || runID <= 0 || runID > 1_000_000_000_000_000 {
		return transferProof{}, false
	}
	attempt, err := strconv.Atoi(match[4])
	if err != nil || attempt <= 0 || attempt > 1000 {
		return transferProof{}, false
	}
	if match[1] == "full" && match[5] != "" || match[1] != "full" && match[5] == "" {
		return transferProof{}, false
	}
	return transferProof{Mode: match[1], Input: testedInput{RunID: runID, Attempt: attempt, MergeSHA: match[2]}, FullRootSHA: match[5]}, true
}

func writeTransferProof(path string, proof transferProof) error {
	if path == "" {
		return nil
	}
	description, err := formatTransferProof(proof)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(description+"\n"), 0o600)
}
