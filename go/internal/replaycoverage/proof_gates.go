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
	detail, _ := validateProofGate(id, context, known)
	if detail == "" {
		return errs
	}
	if _, done := seen[id]; done {
		return errs
	}
	seen[id] = struct{}{}
	return append(errs, fmt.Errorf("%s", detail))
}

// validateProofGate returns the reason a proof gate is broken (invalid) or, when
// the row is sound but switched off, the reason it is off. At most one is set.
//
// A gate is off exactly when its registry row is tier manual and non-blocking:
// that is how an owner order disables a CI job in this repository, and the
// registry validator already refuses a disabled job whose row stays blocking.
// A non-blocking row on any live tier is an advisory gate and stays invalid, as
// do blank, unknown, and local-command-less rows.
func validateProofGate(id string, context string, known map[string]cigates.Gate) (invalid string, off string) {
	if id == "" {
		return fmt.Sprintf("%s has blank proof_gate", context), ""
	}
	gate, ok := known[id]
	if !ok {
		return fmt.Sprintf("%s references unknown proof_gate %q", context, id), ""
	}
	if !gate.Blocking {
		if gate.Tier != cigates.TierManual {
			return fmt.Sprintf("%s proof_gate %q is not blocking", context, id), ""
		}
		reason := strings.TrimSpace(gate.CIOnlyReason)
		if reason == "" {
			reason = "no reason recorded in the registry row"
		}
		off = fmt.Sprintf("proof gate %q is off (tier manual, non-blocking): %s", id, reason)
	}
	if gate.Local == nil || strings.TrimSpace(gate.Local.Command) == "" {
		return fmt.Sprintf("%s proof_gate %q has no local command", context, id), ""
	}
	if strings.TrimSpace(gate.CI.Workflow) == "" && strings.TrimSpace(gate.LocalOnlyReason) == "" {
		return fmt.Sprintf("%s proof_gate %q has no CI workflow and no local_only_reason", context, id), ""
	}
	return "", off
}

// proofGateValidationDetails maps a proof gate (or authz ref) to why it is
// invalid, or to why it is off. The two maps never share a key.
type proofGateValidationDetails struct {
	byProofGate    map[string]string
	byAuthzRef     map[string]string
	offByProofGate map[string]string
	offByAuthzRef  map[string]string
}

func proofGateValidationDetailsByScenario(m Manifest, authz AuthzProofLedger, registry *cigates.Registry) proofGateValidationDetails {
	details := proofGateValidationDetails{
		byProofGate:    map[string]string{},
		byAuthzRef:     map[string]string{},
		offByProofGate: map[string]string{},
		offByAuthzRef:  map[string]string{},
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
		invalid, off := validateProofGate(id, context, known)
		if invalid != "" {
			details.byProofGate[id] = invalid
		}
		if off != "" {
			details.offByProofGate[id] = off
		}
	}
	for _, scenario := range authz.Scenarios {
		id := strings.TrimSpace(scenario.ProofGate)
		context := fmt.Sprintf("authorization proof ledger %s:%s", scenario.Family, scenario.GrantMode)
		ref := strings.TrimSpace(scenario.Family) + ":" + strings.TrimSpace(scenario.GrantMode)
		invalid, off := validateProofGate(id, context, known)
		if invalid != "" {
			details.byAuthzRef[ref] = invalid
		}
		if off != "" {
			details.offByAuthzRef[ref] = off
		}
	}
	return details
}
