// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
)

// TestLoadRuntimeConfigDefaultsClaimLeaseToNVDEnvelope pins the lane-B code
// knob for eshu-hq/eshu#10446 (option B: 600s NVD claim envelope with
// coherent lease TTL). The extracted worker's lease fallback must cover the
// measured NVD walk floor (>=546s for the 4566-record / 92-page head window),
// so an unset ESHU_COMPONENT_COLLECTOR_CLAIM_LEASE_TTL can never shrink the
// satellite envelope back to the 60s workflow default that wedged the
// rolling NVD stream on context-deadline launch failures.
func TestLoadRuntimeConfigDefaultsClaimLeaseToNVDEnvelope(t *testing.T) {
	t.Parallel()

	componentHome := t.TempDir()
	configPath := writeComponentExtensionConfig(t)
	installEnabledComponent(t, componentHome, "process", configPath)

	config, err := loadRuntimeConfig(mapEnv(map[string]string{
		envComponentHome:            componentHome,
		envComponentTrustMode:       component.TrustModeAllowlist,
		envComponentAllowIDs:        "dev.eshu.examples.scorecard",
		envComponentAllowPublishers: "eshu-hq",
		envComponentCoreVersion:     "dev",
		envCollectorInstanceID:      "scorecard-local",
		envCollectorOwnerID:         "component-owner-a",
	}))
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v, want nil", err)
	}
	// Changed knob: the extracted-worker lease fallback. The deployed
	// overlay carries the matching per-activation value on the
	// vulnerability-intelligence worker; the fallback must agree so an
	// unset env keeps the ruled envelope instead of restoring the
	// shorter wedge.
	if got, want := config.ClaimLeaseTTL, 600*time.Second; got != want {
		t.Fatalf("ClaimLeaseTTL = %v, want %v (ruled 600s NVD envelope, eshu-hq/eshu#10446)", got, want)
	}
	// Coherent lease: the heartbeat fallback must stay well under the lease
	// so the worker keeps renewing through a full ~546s+ paced walk.
	if config.HeartbeatInterval >= config.ClaimLeaseTTL {
		t.Fatalf("HeartbeatInterval = %v must be less than ClaimLeaseTTL = %v", config.HeartbeatInterval, config.ClaimLeaseTTL)
	}
}
