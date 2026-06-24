// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
	"golang.org/x/mod/semver"
)

const derivedTargetBudgetExhaustionLookahead = 1

const (
	derivedTargetPlanningModeRotating   = "rotating"
	derivedTargetPlanningModeSinglePass = "single_pass"
)

func derivationEcosystems(values []string, defaults []string) map[string]struct{} {
	return stringSet(values, defaults)
}

func stringSet(values []string, defaults []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values)+len(defaults))
	source := values
	if len(source) == 0 {
		source = defaults
	}
	for _, value := range source {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		out[normalized] = struct{}{}
	}
	return out
}

func stringSetContains(values map[string]struct{}, value string) bool {
	_, ok := values[strings.ToLower(strings.TrimSpace(value))]
	return ok
}

func sortedStringSetValues(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func derivationLimit(raw int, fallback int) int {
	if raw > 0 {
		return raw
	}
	return fallback
}

func derivedTargetRotationOffset(observedAt time.Time, interval time.Duration, limit int) int64 {
	if observedAt.IsZero() || limit <= 0 {
		return 0
	}
	if interval <= 0 {
		interval = defaultReconcileInterval
	}
	bucket := observedAt.UTC().UnixNano() / int64(interval)
	return bucket * int64(limit)
}

func derivedTargetRotationOffsetForMode(
	planningMode string,
	observedAt time.Time,
	interval time.Duration,
	limit int,
) int64 {
	if normalizeDerivedTargetPlanningMode(planningMode) == derivedTargetPlanningModeSinglePass {
		return 0
	}
	return derivedTargetRotationOffset(observedAt, interval, limit)
}

func derivedTargetPlanKey(prefix string, observedAt time.Time, interval time.Duration, planningMode string) string {
	if prefix == "" {
		prefix = "schedule"
	}
	if normalizeDerivedTargetPlanningMode(planningMode) == derivedTargetPlanningModeSinglePass {
		return prefix + "-single-pass"
	}
	if interval <= 0 {
		interval = defaultReconcileInterval
	}
	return fmt.Sprintf("%s-%s", prefix, observedAt.UTC().Truncate(interval).Format("20060102T150405Z"))
}

func normalizeDerivedTargetPlanningMode(raw string) string {
	switch strings.TrimSpace(raw) {
	case derivedTargetPlanningModeSinglePass:
		return derivedTargetPlanningModeSinglePass
	default:
		return derivedTargetPlanningModeRotating
	}
}

func derivedTargetReadLimit(targetLimit int) int {
	if targetLimit <= 0 {
		return targetLimit
	}
	return targetLimit + derivedTargetBudgetExhaustionLookahead
}

func packageRegistryDerivationFromConfig(raw string) (packageRegistryDerivationConfiguration, error) {
	if err := workflow.ValidatePackageRegistryCollectorConfiguration(raw); err != nil {
		return packageRegistryDerivationConfiguration{}, err
	}
	var decoded packageRegistryRuntimeConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return packageRegistryDerivationConfiguration{}, fmt.Errorf("decode package registry derivation config: %w", err)
	}
	return decoded.DeriveFromOwnedPackages, nil
}

func vulnerabilityDerivationFromConfig(raw string) (vulnerabilityDerivationConfiguration, error) {
	if err := workflow.ValidateVulnerabilityIntelligenceCollectorConfiguration(raw); err != nil {
		return vulnerabilityDerivationConfiguration{}, err
	}
	var decoded vulnerabilityRuntimeConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return vulnerabilityDerivationConfiguration{}, fmt.Errorf("decode vulnerability derivation config: %w", err)
	}
	return decoded.DeriveFromOwnedPackages, nil
}

func vulnerabilityPlanKeyDerivationFromConfig(raw string) (vulnerabilityDerivationConfiguration, error) {
	if err := workflow.ValidateVulnerabilityIntelligenceCollectorConfiguration(raw); err != nil {
		return vulnerabilityDerivationConfiguration{}, err
	}
	var decoded vulnerabilityRuntimeConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return vulnerabilityDerivationConfiguration{}, fmt.Errorf("decode vulnerability derivation config: %w", err)
	}
	if decoded.DeriveFromOwnedPackages.Enabled {
		return decoded.DeriveFromOwnedPackages, nil
	}
	return decoded.DeriveFromInstalledEvidence, nil
}

func exactOwnedDependencyVersion(raw string) (string, bool) {
	version := strings.TrimSpace(raw)
	if version == "" {
		return "", false
	}
	lower := strings.ToLower(version)
	if lower == "latest" || nonVersionOwnedDependencyPrefix(lower) {
		return "", false
	}
	if strings.ContainsAny(version, "<>^~*=|, ") ||
		strings.Contains(lower, " - ") ||
		strings.Contains(lower, ".x") ||
		strings.Contains(lower, "x.") {
		return "", false
	}
	semverVersion := version
	if !strings.HasPrefix(semverVersion, "v") {
		semverVersion = "v" + semverVersion
	}
	if !semver.IsValid(semverVersion) {
		return "", false
	}
	return version, true
}

func nonVersionOwnedDependencyPrefix(lower string) bool {
	for _, prefix := range []string{
		"file:",
		"git+",
		"git://",
		"github:",
		"gitlab:",
		"http:",
		"https:",
		"link:",
		"npm:",
		"portal:",
		"workspace:",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}
