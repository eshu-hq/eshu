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

// JobScheduledFunc reports whether the workflow a CI-gate registry row names
// defines a non-empty on.schedule cron trigger and every matched workflow job
// is live (not permanently off). cigates.WorkflowJobScheduled is the production
// implementation; callers bind it to a repository root.
type JobScheduledFunc func(cigates.Gate) (bool, error)

// ValidateRequiredProofGates checks that every proof_gate referenced by replay
// coverage inputs maps to a known, enforceable CI-gate registry entry. A gate
// that carries the registry's disabled marker passes only when jobDisabled
// confirms the workflow job it names is off. A non-blocking tier: ci-heavy,
// non-secondary gate passes only when jobScheduled confirms the workflow has a
// cron schedule and every matched job is live.
func ValidateRequiredProofGates(m Manifest, authz AuthzProofLedger, registry *cigates.Registry, jobDisabled JobDisabledFunc, jobScheduled JobScheduledFunc) []error {
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
		errs = appendProofGateErrors(errs, seen, known, jobDisabled, jobScheduled, strings.TrimSpace(entry.ProofGate), context)
	}
	for _, scenario := range authz.Scenarios {
		context := fmt.Sprintf("authorization proof ledger %s:%s", scenario.Family, scenario.GrantMode)
		errs = appendProofGateErrors(errs, seen, known, jobDisabled, jobScheduled, strings.TrimSpace(scenario.ProofGate), context)
	}
	return errs
}

func appendProofGateErrors(errs []error, seen map[string]struct{}, known map[string]cigates.Gate, jobDisabled JobDisabledFunc, jobScheduled JobScheduledFunc, id string, context string) []error {
	detail, _ := validateProofGate(id, context, known, jobDisabled, jobScheduled)
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
// A non-blocking tier: ci-heavy, non-secondary row without a disabled marker is
// accepted as an active scheduled proof gate only when jobScheduled confirms
// its workflow has a cron schedule and its matched job is live. Bare pre-pr
// advisory, unmarked manual, and watch-only secondary rows stay invalid, and a
// nil or failing checker fails closed. Blank, unknown, and local-command-less
// rows stay invalid too.
func validateProofGate(id string, context string, known map[string]cigates.Gate, jobDisabled JobDisabledFunc, jobScheduled JobScheduledFunc) (invalid string, off string) {
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
		if invalid = checkScheduledProofGate(context, gate, jobScheduled); invalid != "" {
			return invalid, ""
		}
	}
	if gate.Local == nil || strings.TrimSpace(gate.Local.Command) == "" {
		return fmt.Sprintf("%s proof_gate %q has no local command", context, id), ""
	}
	if strings.TrimSpace(gate.CI.Workflow) == "" && strings.TrimSpace(gate.LocalOnlyReason) == "" {
		return fmt.Sprintf("%s proof_gate %q has no CI workflow and no local_only_reason", context, id), ""
	}
	return "", off
}

// checkScheduledProofGate validates a non-blocking, non-disabled row as an
// active scheduled CI-heavy proof gate. Only tier: ci-heavy, non-secondary rows
// whose workflow defines a cron schedule and whose matched job is live qualify;
// every other non-blocking posture fails closed.
func checkScheduledProofGate(context string, gate cigates.Gate, jobScheduled JobScheduledFunc) string {
	if gate.Tier != cigates.TierCIHeavy || gate.Layer == cigates.LayerSecondary {
		return fmt.Sprintf("%s proof_gate %q is not blocking (an owner order that switched it off needs a disabled marker with a reason and a tracking issue on the registry row)", context, gate.ID)
	}
	if jobScheduled == nil {
		return fmt.Sprintf("%s proof_gate %q is not blocking and its scheduled workflow cannot be verified", context, gate.ID)
	}
	scheduled, err := jobScheduled(gate)
	if err != nil {
		return fmt.Sprintf("%s proof_gate %q scheduled workflow cannot be verified: %v", context, gate.ID, err)
	}
	if !scheduled {
		return fmt.Sprintf("%s proof_gate %q is not blocking and job %q in %q is not scheduled to run", context, gate.ID, gate.CI.Job, gate.CI.Workflow)
	}
	return ""
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

func proofGateValidationDetailsByScenario(m Manifest, authz AuthzProofLedger, registry *cigates.Registry, jobDisabled JobDisabledFunc, jobScheduled JobScheduledFunc) proofGateValidationDetails {
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
		invalid, off := validateProofGate(id, context, known, jobDisabled, jobScheduled)
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
		invalid, off := validateProofGate(id, context, known, jobDisabled, jobScheduled)
		if invalid != "" {
			details.byAuthzRef[ref] = invalid
		}
		if off != "" {
			details.offByAuthzRef[ref] = off
		}
	}
	return details
}
