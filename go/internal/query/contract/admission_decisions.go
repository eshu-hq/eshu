// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import (
	"github.com/eshu-hq/eshu/go/internal/query/evidence"
)

func init() {
	// Declared by the evidence family (evidence/capabilities.go, #6642),
	// not copied here.
	register(admissionDecisionCapability, evidence.AdmissionDecisionSupport())
}
