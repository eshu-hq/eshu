// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"fmt"
	"strings"
)

func vulnScanExitErrorForResult(result vulnScanRepoResult) error {
	code, reason := vulnScanExitClassification(result.ReadinessState, result.Count)
	if code == 0 {
		return nil
	}
	return commandExitError{message: vulnScanExitMessage(reason, result.ReadinessState), code: code}
}

func isVulnScanFindingsExit(err error) bool {
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	return exitErr.ExitCode() == 3
}

func isVulnScanScannerExit(err error) bool {
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	switch exitErr.ExitCode() {
	case 3, 4, 5:
		return true
	default:
		return false
	}
}

func vulnScanExitClassification(state string, count int) (int, string) {
	switch strings.TrimSpace(state) {
	case "ready_zero_findings":
		return 0, "ready_zero_findings"
	case "ready_with_findings":
		return 3, "findings_present"
	case "unsupported":
		return 5, "unsupported"
	case "not_configured", "target_incomplete", "evidence_incomplete", "readiness_unavailable":
		return 4, strings.TrimSpace(state)
	default:
		if count > 0 {
			return 3, "findings_present"
		}
		return 4, "readiness_unavailable"
	}
}

func vulnScanExitMessage(reason, state string) string {
	switch reason {
	case "findings_present":
		return "vulnerability findings present"
	case "unsupported":
		return "vulnerability scan encountered unsupported target evidence"
	default:
		return fmt.Sprintf("vulnerability scan did not reach a clean ready-zero result: %s", state)
	}
}
