// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// minimalPublicBundle returns a bundle produced by Capture from a small,
// clean CaptureInput, for tests that only need a valid share-safe starting
// point to mutate.
func minimalPublicBundle(t *testing.T) Bundle {
	t.Helper()
	bundle, err := Capture(CaptureInput{
		Surface: "api",
		Target:  "/api/v0/services/checkout/story",
		Method:  "GET",
		Params:  map[string]any{"repo": "demo/service"},
		Envelope: query.ResponseEnvelope{
			Data: map[string]any{"owner": "platform-team"},
			Truth: &query.TruthEnvelope{
				Level: query.TruthLevelExact,
				Basis: query.TruthBasisAuthoritativeGraph,
			},
		},
		ReporterNote: "expected the owning team, got an empty list",
	})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}
	return bundle
}

// mustMarshal is a test helper that fails the test instead of returning an
// error, keeping canary assertions focused on the redaction behavior rather
// than marshal error plumbing.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}
