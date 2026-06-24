// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"
)

// Validate checks that a replatforming plan honors the contract invariants:
// pinned version, valid taxonomy states, identified items, refusal-bearing
// refused import candidates, and explicit ambiguity reasons whenever ownership
// is not singular. It returns the first violation found, or nil when the plan
// is well formed. Validation is structural only; it never mutates the plan.
func (p *ReplatformingPlan) Validate() error {
	if p == nil {
		return fmt.Errorf("replatforming plan is nil")
	}
	if p.ContractVersion != ReplatformingPlanContractVersion {
		return fmt.Errorf(
			"replatforming plan contract_version = %q, want %q",
			p.ContractVersion, ReplatformingPlanContractVersion,
		)
	}
	if len(p.NonGoals) == 0 {
		return fmt.Errorf("replatforming plan must carry explicit non-goals")
	}
	seen := map[string]struct{}{}
	for i := range p.Items {
		if err := p.Items[i].validate(); err != nil {
			return fmt.Errorf("item %d: %w", i, err)
		}
		if _, dup := seen[p.Items[i].ItemID]; dup {
			return fmt.Errorf("item %d: duplicate item_id %q", i, p.Items[i].ItemID)
		}
		seen[p.Items[i].ItemID] = struct{}{}
	}
	return nil
}

// validate checks one packet item's required fields and ownership-ambiguity
// invariant.
func (item MigrationPacketItem) validate() error {
	if strings.TrimSpace(item.ItemID) == "" {
		return fmt.Errorf("item_id is required")
	}
	if strings.TrimSpace(item.Provider) == "" {
		return fmt.Errorf("provider is required")
	}
	if strings.TrimSpace(item.ResourceType) == "" {
		return fmt.Errorf("resource_type is required")
	}
	if strings.TrimSpace(item.StableID) == "" {
		return fmt.Errorf("stable_id is required")
	}
	if !item.SourceState.Valid() {
		return fmt.Errorf("source_state %q is not a taxonomy state", item.SourceState)
	}
	if err := item.validateImportCandidate(); err != nil {
		return err
	}
	return item.validateOwnerAmbiguity()
}

// validateImportCandidate enforces that a refused candidate carries reasons and
// no import block, and a ready candidate carries an import block.
func (item MigrationPacketItem) validateImportCandidate() error {
	candidate := item.ImportCandidate
	if candidate == nil {
		return nil
	}
	switch candidate.Status {
	case ReplatformingImportStatusReady:
		if strings.TrimSpace(candidate.ImportBlock) == "" {
			return fmt.Errorf("ready import candidate must carry an import block")
		}
	case ReplatformingImportStatusRefused:
		if len(candidate.RefusalReasons) == 0 {
			return fmt.Errorf("refused import candidate must carry refusal reasons")
		}
		if strings.TrimSpace(candidate.ImportBlock) != "" {
			return fmt.Errorf("refused import candidate must not carry an import block")
		}
	default:
		return fmt.Errorf("import candidate status %q is invalid", candidate.Status)
	}
	return nil
}

// validateOwnerAmbiguity enforces that whenever more than one owner candidate of
// the same kind exists, every such candidate names its ambiguity reasons, and
// that an item whose source state is ambiguous never presents singular,
// reason-free ownership.
func (item MigrationPacketItem) validateOwnerAmbiguity() error {
	byKind := map[string]int{}
	for _, owner := range item.OwnerCandidates {
		byKind[normalizedOwnerKind(owner.Kind)]++
	}
	for _, owner := range item.OwnerCandidates {
		competing := byKind[normalizedOwnerKind(owner.Kind)] > 1
		if competing && len(owner.AmbiguityReasons) == 0 {
			return fmt.Errorf(
				"owner candidate kind %q competes but has no ambiguity reasons",
				owner.Kind,
			)
		}
		if item.SourceState == ReplatformingSourceStateAmbiguous &&
			len(owner.AmbiguityReasons) == 0 {
			return fmt.Errorf(
				"ambiguous item owner candidate kind %q must name ambiguity reasons",
				owner.Kind,
			)
		}
	}
	return nil
}
