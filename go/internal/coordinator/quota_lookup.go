// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Operator quota-override surface (#8561). Each variable is optional: blank
// means the operator sets no override for that field and the manifest
// declaration stands. A set value must be a positive integer; a
// set-but-invalid value fails coordinator startup so a misconfigured bound
// can never silently run unlimited (or fail per claim at runtime).
const (
	componentQuotaMaxGenerationsPerHourEnv = "ESHU_COMPONENT_QUOTA_MAX_GENERATIONS_PER_HOUR"
	componentQuotaMaxFactsPerGenerationEnv = "ESHU_COMPONENT_QUOTA_MAX_FACTS_PER_GENERATION"
)

// componentQuotaOverrideFromEnv reads the uniform operator quota override:
// every set field wins over every quota-contributing manifest's declaration
// per field (component.Quota.Resolve). The override is deployment policy,
// not a per-manifest patch: it applies to all planned components equally.
// A kind declared only by dormant manifests gets no budget from the
// override alone (#8582).
func componentQuotaOverrideFromEnv(getenv func(string) string) (component.Quota, error) {
	var override component.Quota
	var err error
	if override.MaxGenerationsPerHour, err = quotaOverrideField(getenv, componentQuotaMaxGenerationsPerHourEnv); err != nil {
		return component.Quota{}, err
	}
	if override.MaxFactsPerGeneration, err = quotaOverrideField(getenv, componentQuotaMaxFactsPerGenerationEnv); err != nil {
		return component.Quota{}, err
	}
	return override, nil
}

// quotaOverrideField reads one override field: blank stays nil (no
// override), a set value must parse as a positive integer.
func quotaOverrideField(getenv func(string) string, key string) (*int, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", key, err)
	}
	if parsed < 1 {
		return nil, fmt.Errorf("%s must be at least 1, got %d", key, parsed)
	}
	return &parsed, nil
}

// buildComponentQuotaLookup fans the admitted manifests of components
// planning at least one claims-enabled instance out to the claim-path
// lookup (#8471, #8561, #8582). One manifest may declare many collector
// kinds while the lookup is keyed by one kind, so every kind a manifest
// declares registers the manifest's resolved quota: the manifest declares,
// the uniform operator override wins per field. A kind whose resolved quota
// carries no generations bound stays unthrottled — the lookup reports no
// quota rather than a zero limit, which the store rejects as a
// configuration error. The result is always non-nil: with nothing to
// enforce every kind reports unthrottled, exactly the pre-quota behavior.
//
// One kind gets one budget: the quota state is keyed by collector kind, so
// two planned manifests with different resolved limits for the same kind
// are contradictory input and fail here with the kind named, instead of
// silently picking a winner the operator did not choose. Quota-less
// deployments (no declared quotas, no override) build an empty lookup and
// keep the plain claim statement.
func buildComponentQuotaLookup(manifests []component.Manifest, override component.Quota) (workflow.QuotaLookup, error) {
	budgets := make(map[scope.CollectorKind]int)
	for _, manifest := range manifests {
		effective := manifest.Spec.Quotas.Resolve(override)
		limit, ok := effective.GenerationsPerHourLimit()
		if !ok {
			continue
		}
		for _, kind := range manifest.Spec.CollectorKinds {
			collectorKind := scope.CollectorKind(strings.TrimSpace(kind))
			if collectorKind == "" {
				continue
			}
			if current, claimed := budgets[collectorKind]; claimed && current != limit {
				return nil, fmt.Errorf(
					"collector kind %q has conflicting quota limits %d and %d: one kind gets one budget",
					string(collectorKind), current, limit,
				)
			}
			budgets[collectorKind] = limit
		}
	}
	return func(collectorKind scope.CollectorKind) (workflow.ClaimQuota, bool) {
		limit, ok := budgets[collectorKind]
		if !ok {
			return workflow.ClaimQuota{}, false
		}
		return workflow.ClaimQuota{MaxGenerationsPerHour: limit}, true
	}, nil
}
