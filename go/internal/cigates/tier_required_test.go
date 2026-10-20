// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The smoke tier (#8617 step 5) decides which gates still run on a clean
// rebase from each gate's tier, so a gate without a valid tier would be
// unclassifiable. The loader must reject a missing or unknown tier
// outright instead of defaulting one in.
func TestLoadRejectsMissingTier(t *testing.T) {
	t.Parallel()
	path := writeSelfTestRegistry(t, nil, `
      command: "bash scripts/verify-example.sh"`)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	raw = []byte(strings.Replace(string(raw), "    tier: pre-pr\n", "", 1))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	_, err = Load(path)
	if err == nil || !strings.Contains(err.Error(), "has invalid tier") ||
		!strings.Contains(err.Error(), "(tier is required)") {
		t.Fatalf("Load() error = %v, want invalid-tier error", err)
	}
}

// An unknown tier name must fail the same way: a typo like "prepush"
// would otherwise let a gate escape every stage comparison.
func TestLoadRejectsUnknownTier(t *testing.T) {
	t.Parallel()
	path := writeSelfTestRegistry(t, nil, `
      command: "bash scripts/verify-example.sh"`)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	raw = []byte(strings.Replace(string(raw), "    tier: pre-pr", "    tier: prepush", 1))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	_, err = Load(path)
	if err == nil || !strings.Contains(err.Error(), "has invalid tier") ||
		!strings.Contains(err.Error(), "(tier is required)") {
		t.Fatalf("Load() error = %v, want invalid-tier error", err)
	}
}

// Every gate in the committed registry must carry a valid tier. This is
// stated explicitly rather than relying on Load's behavior so a future
// loader relaxation cannot silently drop the contract.
func TestCommittedRegistryEveryGateHasValidTier(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "..", "specs", "ci-gates.v1.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("committed registry not present: %v", err)
	}
	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(committed) error = %v", err)
	}
	if len(reg.Gates) == 0 {
		t.Fatal("committed registry loaded zero gates")
	}
	for _, gate := range reg.Gates {
		if _, ok := tierOrder[gate.Tier]; !ok {
			t.Errorf("gate %q has tier %q, want one of the five tiers", gate.ID, gate.Tier)
		}
	}
}
