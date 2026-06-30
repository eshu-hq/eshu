// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// ValidateRequiredProofGates checks that every proof_gate referenced by replay
// coverage inputs maps to a known, enforceable CI-gate registry entry.
func ValidateRequiredProofGates(m Manifest, authz AuthzProofLedger, registry *cigates.Registry) []error {
	if registry == nil {
		return []error{fmt.Errorf("replay proof gates: missing ci-gates registry")}
	}
	known := map[string]cigates.Gate{}
	for _, gate := range registry.Gates {
		known[gate.ID] = gate
	}

	var errs []error
	seen := map[string]struct{}{}
	for _, entry := range m.Coverage {
		context := fmt.Sprintf("coverage manifest surface %q", entry.Surface)
		errs = appendProofGateErrors(errs, seen, known, strings.TrimSpace(entry.ProofGate), context)
	}
	for _, scenario := range authz.Scenarios {
		context := fmt.Sprintf("authorization proof ledger %s:%s", scenario.Family, scenario.GrantMode)
		errs = appendProofGateErrors(errs, seen, known, strings.TrimSpace(scenario.ProofGate), context)
	}
	return errs
}

func appendProofGateErrors(errs []error, seen map[string]struct{}, known map[string]cigates.Gate, id string, context string) []error {
	detail := validateProofGate(id, context, known)
	if detail == "" {
		return errs
	}
	if _, done := seen[id]; done {
		return errs
	}
	seen[id] = struct{}{}
	return append(errs, fmt.Errorf("%s", detail))
}

func validateProofGate(id string, context string, known map[string]cigates.Gate) string {
	if id == "" {
		return fmt.Sprintf("%s has blank proof_gate", context)
	}
	gate, ok := known[id]
	if !ok {
		return fmt.Sprintf("%s references unknown proof_gate %q", context, id)
	}
	if !gate.Blocking {
		return fmt.Sprintf("%s proof_gate %q is not blocking", context, id)
	}
	if gate.Local == nil || strings.TrimSpace(gate.Local.Command) == "" {
		return fmt.Sprintf("%s proof_gate %q has no local command", context, id)
	}
	if strings.TrimSpace(gate.CI.Workflow) == "" && strings.TrimSpace(gate.LocalOnlyReason) == "" {
		return fmt.Sprintf("%s proof_gate %q has no CI workflow and no local_only_reason", context, id)
	}
	return ""
}

type proofGateValidationDetails struct {
	byProofGate map[string]string
	byAuthzRef  map[string]string
}

func proofGateValidationDetailsByScenario(m Manifest, authz AuthzProofLedger, registry *cigates.Registry) proofGateValidationDetails {
	details := proofGateValidationDetails{
		byProofGate: map[string]string{},
		byAuthzRef:  map[string]string{},
	}
	if registry == nil {
		return details
	}
	known := map[string]cigates.Gate{}
	for _, gate := range registry.Gates {
		known[gate.ID] = gate
	}
	for _, entry := range m.Coverage {
		id := strings.TrimSpace(entry.ProofGate)
		context := fmt.Sprintf("coverage manifest surface %q", entry.Surface)
		if detail := validateProofGate(id, context, known); detail != "" {
			details.byProofGate[id] = detail
		}
	}
	for _, scenario := range authz.Scenarios {
		id := strings.TrimSpace(scenario.ProofGate)
		context := fmt.Sprintf("authorization proof ledger %s:%s", scenario.Family, scenario.GrantMode)
		ref := strings.TrimSpace(scenario.Family) + ":" + strings.TrimSpace(scenario.GrantMode)
		if detail := validateProofGate(id, context, known); detail != "" {
			details.byAuthzRef[ref] = detail
		}
	}
	return details
}
