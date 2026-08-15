// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"fmt"
	"strings"
)

// Failure is the error the vuln-scan functions return when the command must
// exit non-zero. It carries the operator-facing message and the numeric exit
// code this family has always used, but not the CLI's exit-error type: mapping
// Failure onto that type is go/cmd/eshu's job, since the type is defined there.
type Failure struct {
	Message string
	Code    int
}

// Error implements error so a Failure can travel as a return value and be
// recovered with errors.As.
func (f *Failure) Error() string { return f.Message }

// ExitClassification maps a readiness state and finding count onto the
// scanner exit contract this family publishes: 0 for a clean ready-zero
// answer, 3 when findings are present, 5 for unsupported target evidence, and
// 4 for every state that did not prove enough evidence to answer. The reason
// string is reported in the JSON report's summary, so it is part of the wire
// contract and not only an internal label.
//
// A state the CLI does not recognize falls back to the count: findings present
// means 3, and anything else is treated as readiness the CLI could not
// establish rather than as success.
func ExitClassification(state string, count int) (int, string) {
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

// ExitMessage is the operator-facing sentence for a classification reason. The
// default arm names the state so a reader can tell which readiness class
// stopped the run without re-reading the JSON.
func ExitMessage(reason, state string) string {
	switch reason {
	case "findings_present":
		return "vulnerability findings present"
	case "unsupported":
		return "vulnerability scan encountered unsupported target evidence"
	default:
		return fmt.Sprintf("vulnerability scan did not reach a clean ready-zero result: %s", state)
	}
}

// ExitFailure returns the Failure a finished Result should exit with, or nil
// when the result reached a clean ready-zero answer.
func ExitFailure(result Result) *Failure {
	code, reason := ExitClassification(result.ReadinessState, result.Count)
	if code == 0 {
		return nil
	}
	return &Failure{Message: ExitMessage(reason, result.ReadinessState), Code: code}
}
