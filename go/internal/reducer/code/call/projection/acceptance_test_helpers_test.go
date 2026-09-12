// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// acceptedGenerationFixed and readinessLookupFixed are local copies of the
// reducer root's own test helpers (acceptance_test_helpers_test.go). Go test
// files cannot share unexported symbols across a package boundary (issue
// #6061).

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
