// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import "github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"

// preferEPSSObservation chooses one complete score observation independently
// of input order. Dated rows outrank undated legacy rows; ties use observed
// time, stable fact identity, then payload values for duplicate identities.
func preferEPSSObservation(candidate, current supplychainmodel.RiskSignals) bool {
	if !current.EPSSSelected {
		return true
	}
	if candidate.EPSSScoreDate != current.EPSSScoreDate {
		return candidate.EPSSScoreDate > current.EPSSScoreDate
	}
	if !candidate.EPSSObservedAt.Equal(current.EPSSObservedAt) {
		return candidate.EPSSObservedAt.After(current.EPSSObservedAt)
	}
	if candidate.EPSSFactID != current.EPSSFactID {
		return candidate.EPSSFactID > current.EPSSFactID
	}
	if candidate.EPSSProbability != current.EPSSProbability {
		return candidate.EPSSProbability > current.EPSSProbability
	}
	if candidate.EPSSPercentile != current.EPSSPercentile {
		return candidate.EPSSPercentile > current.EPSSPercentile
	}
	return candidate.EPSSModelVersion > current.EPSSModelVersion
}
