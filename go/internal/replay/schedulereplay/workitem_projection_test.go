// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedulereplay

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestVulnerabilitySuppressionWorkItemRejectsBlankSuppressionID(t *testing.T) {
	t.Parallel()

	for _, payload := range []map[string]any{
		{},
		{"suppression_id": ""},
		{"suppression_id": "   "},
	} {
		_, err := vulnerabilitySuppressionWorkItem(facts.Envelope{
			FactKind:      factKindVulnerabilitySupp,
			StableFactKey: "suppression:stable-key",
			Payload:       payload,
		})
		if err == nil || !strings.Contains(err.Error(), "missing required suppression_id") {
			t.Fatalf("payload %#v: want missing required suppression_id error, got %v", payload, err)
		}
	}
}
