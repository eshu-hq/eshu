// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "fmt"

func validateAdmissionDecision(d AdmissionDecision) error {
	if !isAdmissionDecisionState(d.State) {
		return fmt.Errorf("admission decision state %q is not supported", d.State)
	}
	return nil
}

func isAdmissionDecisionState(state AdmissionDecisionState) bool {
	for _, valid := range AdmissionDecisionStateValues() {
		if state == valid {
			return true
		}
	}
	return false
}
