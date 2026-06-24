// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

func acceptedGenerationFixed(generationID string, ok bool) AcceptedGenerationLookup {
	return func(SharedProjectionAcceptanceKey) (string, bool) {
		return generationID, ok
	}
}

func readinessLookupFixed(ready, ok bool) GraphProjectionReadinessLookup {
	return func(GraphProjectionPhaseKey, GraphProjectionPhase) (bool, bool) {
		return ready, ok
	}
}
