// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"testing"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// TestEnabledParsesTheSwitchOnce pins the one parse of
// ESHU_CHANGED_SINCE_LINK_ENABLED that the reducer, the ingester and
// bootstrap-index share (#7127 PR-3b), so the link runner and the Ack
// journal can never disagree on a value.
func TestEnabledParsesTheSwitchOnce(t *testing.T) {
	t.Parallel()
	if linksfreshnessstore.EnabledEnv != "ESHU_CHANGED_SINCE_LINK_ENABLED" {
		t.Fatalf("EnabledEnv = %q", linksfreshnessstore.EnabledEnv)
	}
	for value, want := range map[string]bool{
		"": false, "true": true, "TRUE": true, " true ": true, "1": true, "t": true, "yes": true, "y": true, "on": true,
		"false": false, "0": false, "f": false, "no": false, "n": false, "off": false, "maybe": false, "2": false,
	} {
		got := linksfreshnessstore.Enabled(func(key string) string {
			if key != linksfreshnessstore.EnabledEnv {
				t.Fatalf("Enabled read %q", key)
			}
			return value
		})
		if got != want {
			t.Errorf("Enabled(%q) = %v, want %v", value, got, want)
		}
	}
}
