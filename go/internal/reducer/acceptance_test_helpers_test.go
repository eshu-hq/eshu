// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

func acceptedGenerationFixed(generationID string, ok bool) sharedintent.AcceptedGenerationLookup {
	return func(sharedintent.AcceptanceKey) (string, bool) {
		return generationID, ok
	}
}

func readinessLookupFixed(ready, ok bool) gpphase.ReadinessLookup {
	return func(gpphase.PhaseKey, gpphase.Phase) (bool, bool) {
		return ready, ok
	}
}
