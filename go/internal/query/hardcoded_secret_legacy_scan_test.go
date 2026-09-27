// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"testing"
)

// TestHardcodedSecretLegacyScanQueryMatchesFrozenGolden binds the production
// legacy scan (the read that serves while the side table is not ready) to the
// frozen pre-#7125 builder in hardcoded_secret_legacy_golden_test.go for every
// proof request shape: same SQL text, same arguments. An edit to either that
// changes the fallback's answers fails here before the live differential runs.
func TestHardcodedSecretLegacyScanQueryMatchesFrozenGolden(t *testing.T) {
	t.Parallel()

	for _, item := range secretProofRequests() {
		wantQuery, wantArgs := legacyHardcodedSecretInvestigationQuery(item.req)
		gotQuery, gotArgs := hardcodedSecretLegacyScanQuery(item.req)
		if gotQuery != wantQuery {
			t.Errorf("%s: legacy scan SQL diverges from the frozen golden:\n got: %s\nwant: %s", item.name, gotQuery, wantQuery)
		}
		if len(gotArgs) != len(wantArgs) {
			t.Errorf("%s: legacy scan args = %v, want %v", item.name, gotArgs, wantArgs)
			continue
		}
		for i := range gotArgs {
			if got, want := fmt.Sprint(gotArgs[i]), fmt.Sprint(wantArgs[i]); got != want {
				t.Errorf("%s: legacy scan arg %d = %s, want %s", item.name, i+1, got, want)
			}
		}
	}
}
