// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// loadManifestDependencyEvidence runs the manifest-dependency load stage:
// the manifest rows one pass matches against, whether the read refused a
// reached delta-active scope's base, and the stage duration on timing. A
// refusal is incomplete evidence, not a failed load — refused reports true
// with no rows and no error, and the caller marks the pass partial (#10109).
func (h SupplyChainImpactHandler) loadManifestDependencyEvidence(
	ctx context.Context,
	envelopes []facts.Envelope,
	timing *supplyChainImpactTiming,
) ([]facts.Envelope, bool, error) {
	started := time.Now()
	dependencies, refused, err := h.loadActivePackageManifestDependencyFacts(ctx, envelopes)
	timing.loadManifestDependenciesDuration = time.Since(started)
	if err != nil {
		return nil, false, fmt.Errorf("load active package manifest dependency facts: %w", err)
	}
	return dependencies, refused, nil
}

// applySupplyChainImpactSecurityAlertScope runs the security-alert scoping
// tail of loadSupplyChainImpactEvidence as one timed step: when the pass
// carries provider alerts, evidence outside their scope is dropped and the
// scoped-out count is reported. It returns the scoped evidence, whether
// scoping applied, how many envelopes it removed, and the post-scope count.
func applySupplyChainImpactSecurityAlertScope(
	intent reducercontract.Intent,
	envelopes []facts.Envelope,
	timing *supplyChainImpactTiming,
) ([]facts.Envelope, bool, int, int) {
	preCount := len(envelopes)
	started := time.Now()
	applied := supplyChainImpactUsesSecurityAlertScope(intent, envelopes)
	if applied {
		envelopes = scopeSupplyChainImpactEvidenceToSecurityAlerts(envelopes)
	}
	timing.securityAlertScopingDuration = time.Since(started)
	postCount := len(envelopes)
	scopedOut := 0
	if applied && preCount > postCount {
		scopedOut = preCount - postCount
	}
	return envelopes, applied, scopedOut, postCount
}
