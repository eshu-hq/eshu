// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rules

import "testing"

func TestMultiCloudRuntimeDriftRulePackValidates(t *testing.T) {
	t.Parallel()

	pack := MultiCloudRuntimeDriftRulePack()
	if err := pack.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
	if pack.Name != MultiCloudRuntimeDriftPackName {
		t.Fatalf("pack.Name = %q, want %q", pack.Name, MultiCloudRuntimeDriftPackName)
	}
}

func TestFirstPartyRulePacksIncludeMultiCloudRuntimeDrift(t *testing.T) {
	t.Parallel()

	for _, pack := range FirstPartyRulePacks() {
		if pack.Name == MultiCloudRuntimeDriftPackName {
			return
		}
	}
	t.Fatalf("FirstPartyRulePacks() missing %q", MultiCloudRuntimeDriftPackName)
}
