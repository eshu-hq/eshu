// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capability

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestCapabilityCatalogLoadFailureAnswersFixedText drives the catalog route
// through a failed catalog load, a client cancel, and an inner cancel
// (#7674). The catalog is embedded, not read through the guarded reader, so
// the reader-fence case does not apply.
func TestCapabilityCatalogLoadFailureAnswersFixedText(t *testing.T) {
	suite := failure.Suite{Routes: []failure.Route{{
		Name: "capability catalog", Path: "/api/v0/capabilities", Message: catalogUnavailableMessage,
		Handler: func(err error) failure.Mounter {
			h := &Handler{}
			// Spend once so load() keeps this injected error instead of
			// overwriting it with a real catalog load.
			h.once.Do(func() { h.loadErr = err })
			return h
		},
	}}}
	for _, tc := range failure.Cases() {
		if tc.Kind == failure.ReaderFence {
			continue
		}
		t.Run(tc.Name, func(t *testing.T) {
			if err := failure.Check(suite, suite.Routes[0], tc); err != nil {
				t.Error(err)
			}
		})
	}
}
