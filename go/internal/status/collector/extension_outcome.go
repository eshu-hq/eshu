// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"slices"
	"time"
)

// ExtensionOutcome summarizes the latest completed or withheld SDK result in each scope
// of one collector instance. It carries no scope IDs or provider data.
type ExtensionOutcome struct {
	CollectorKind  string
	InstanceID     string
	ScopeCount     int
	PartialScopes  int
	WarningCount   int
	UpdatedAt      time.Time
	InFlightScopes int
}

// CloneExtensionOutcomes isolates a report from its storage snapshot.
func CloneExtensionOutcomes(rows []ExtensionOutcome) []ExtensionOutcome {
	return slices.Clone(rows)
}

// addExtensionOutcomes folds the current outcome for each extension scope into
// instance health. A later clean completion has already superseded an older
// partial for the same scope in the storage read.
func (b *runtimeStatusBuilder) addExtensionOutcomes(rows []ExtensionOutcome) {
	for _, row := range rows {
		key := runtimeStatusKey(row.CollectorKind, row.InstanceID)
		index, exists := b.byKey[key]
		if !exists || !b.extensionHosted[key] || !b.statuses[index].Enabled || b.statuses[index].StatusCategory == RuntimeDisabled {
			continue
		}
		health := "observed"
		detail := "extension claim completed"
		if row.PartialScopes > 0 {
			health = "partial"
			detail = "extension claim reported partial evidence"
		}
		if row.InFlightScopes > 0 {
			health = "partial"
			detail = "extension claim is in progress"
		}
		status := directEvidenceRuntimeStatus(row.InstanceID, row.CollectorKind,
			"extension_claim_outcome", nil, 0, health, row.UpdatedAt, row.UpdatedAt)
		status.Detail = detail
		b.add(status)
	}
}
