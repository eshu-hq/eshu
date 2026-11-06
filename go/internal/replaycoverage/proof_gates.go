// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// JobDisabledFunc reports whether the workflow job a CI-gate registry row names
// is permanently disabled. cigates.WorkflowJobDisabled is the production
// implementation; callers bind it to a repository root.
type JobDisabledFunc func(cigates.Gate) (bool, error)

// ValidateRequiredProofGates checks that every proof_gate referenced by replay
// coverage inputs maps to a known, enforceable CI-gate registry entry. A gate
// that carries the registry's disabled marker passes only when jobDisabled
// confirms the workflow job it names is off.
func ValidateRequiredProofGates(m Manifest, authz AuthzProofLedger, registry *cigates.Registry, jobDisabled JobDisabledFunc) []error {
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
		errs = appendProofGateErrors(errs, seen, known, jobDisabled, strings.TrimSpace(entry.ProofGate), context)
	}
	for _, scenario := range authz.Scenarios {
		context := fmt.Sprintf("authorization proof ledger %s:%s", scenario.Family, scenario.GrantMode)
		errs = appendProofGateErrors(errs, seen, known, jobDisabled, strings.TrimSpace(scenario.ProofGate), context)
	}
	return errs
}

func appendProofGateErrors(errs []error, seen map[string]struct{}, known map[string]cigates.Gate, jobDisabled JobDisabledFunc, id string, context string) []error {
	detail, _ := validateProofGate(id, context, known, jobDisabled)
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
// A gate is off only when its registry row carries the disabled marker (a
// reason and a tracking issue; cigates.Load requires it to sit on a manual,
// non-blocking row) AND the workflow job the row names is permanently disabled.
// Both halves are required so the row and the job cannot disagree: a bare
// manual, non-blocking row is an advisory gate and stays invalid, and a marker
// on a job that can still run is refused. Blank, unknown, and local-command-less
// rows stay invalid too.
func validateProofGate(id string, context string, known map[string]cigates.Gate, jobDisabled JobDisabledFunc) (invalid string, off string) {
	if id == "" {
		return fmt.Sprintf("%s has blank proof_gate", context), ""
	}
	gate, ok := known[id]
	if !ok {
		return fmt.Sprintf("%s references unknown proof_gate %q", context, id), ""
	}
	if gate.Disabled != nil {
		invalid, off = checkDisabledProofGate(context, gate, jobDisabled)
		if invalid != "" {
			return invalid, ""
		}
	} else if !gate.Blocking {
		return fmt.Sprintf("%s proof_gate %q is not blocking (an owner order that switched it off needs a disabled marker with a reason and a tracking issue on the registry row)", context, id), ""
	}
	if gate.Local == nil || strings.TrimSpace(gate.Local.Command) == "" {
		return fmt.Sprintf("%s proof_gate %q has no local command", context, id), ""
	}
	if strings.TrimSpace(gate.CI.Workflow) == "" && strings.TrimSpace(gate.LocalOnlyReason) == "" {
		return fmt.Sprintf("%s proof_gate %q has no CI workflow and no local_only_reason", context, id), ""
	}
	return "", off
}

// checkDisabledProofGate validates a row that carries the disabled marker and
// returns why the gate is off. The marker is a claim about a workflow job, so it
// is trusted only when jobDisabled says that job is off.
func checkDisabledProofGate(context string, gate cigates.Gate, jobDisabled JobDisabledFunc) (invalid string, off string) {
	if gate.Blocking || gate.Tier != cigates.TierManual {
		return fmt.Sprintf("%s proof_gate %q has a disabled marker but is tier %q, blocking=%t; a disabled gate must be manual and non-blocking", context, gate.ID, gate.Tier, gate.Blocking), ""
	}
	if jobDisabled == nil {
		return fmt.Sprintf("%s proof_gate %q has a disabled marker that cannot be checked against its workflow job", context, gate.ID), ""
	}
	disabled, err := jobDisabled(gate)
	if err != nil {
		return fmt.Sprintf("%s proof_gate %q has a disabled marker whose workflow job cannot be checked: %v", context, gate.ID, err), ""
	}
	if !disabled {
		return fmt.Sprintf("%s proof_gate %q has a disabled marker but job %q in %q can still run", context, gate.ID, gate.CI.Job, gate.CI.Workflow), ""
	}
	return "", fmt.Sprintf("proof gate %q is off (%s, tracked in %s)", gate.ID, gate.Disabled.Reason, gate.Disabled.Issue)
}

// proofGateValidationDetails maps a proof gate (or authz ref) to why it is
// invalid, or to why it is off. The two maps never share a key.
type proofGateValidationDetails struct {
	byProofGate    map[string]string
	byAuthzRef     map[string]string
	offByProofGate map[string]string
	offByAuthzRef  map[string]string
}

func proofGateValidationDetailsByScenario(m Manifest, authz AuthzProofLedger, registry *cigates.Registry, jobDisabled JobDisabledFunc) proofGateValidationDetails {
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
		invalid, off := validateProofGate(id, context, known, jobDisabled)
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
		invalid, off := validateProofGate(id, context, known, jobDisabled)
		if invalid != "" {
			details.byAuthzRef[ref] = invalid
		}
		if off != "" {
			details.offByAuthzRef[ref] = off
		}
	}
	return details
}
