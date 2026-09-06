// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenance

import "github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"

// acceptedGenerationFixed mirrors the reducer root's test helper of the same
// name (internal/reducer/acceptance_test_helpers_test.go). Go test files
// cannot share unexported symbols across a package boundary, so it is
// duplicated here rather than exported from root (issue #6061).
func acceptedGenerationFixed(generationID string, ok bool) AcceptedGenerationLookup {
	return func(sharedintent.AcceptanceKey) (string, bool) {
		return generationID, ok
	}
}
