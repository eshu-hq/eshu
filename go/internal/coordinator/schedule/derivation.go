// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedule

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// DefaultReconcileInterval is the coordinator's fallback reconcile cadence. It
// also bounds plan-key and rotation bucketing whenever a collector instance
// supplies no interval of its own.
const DefaultReconcileInterval = 30 * time.Second

// budgetExhaustionLookahead is the one extra row a derived-target read asks
// for beyond the target limit, so the caller can tell "exactly at budget"
// from "budget exhausted with more waiting".
const budgetExhaustionLookahead = 1

// Derived-target planning modes. Rotating pages through the derived target
// set across reconcile intervals; single-pass plans the same first page every
// time under a stable plan key.
const (
	PlanningModeRotating   = "rotating"
	PlanningModeSinglePass = "single_pass"
)

// DerivationEcosystems normalizes the configured ecosystem filter for derived
// target selection, falling back to defaults when the instance configures
// none.
func DerivationEcosystems(values []string, defaults []string) map[string]struct{} {
	return StringSet(values, defaults)
}

// StringSet lowercases and trims values into a set, dropping blanks. An empty
// values slice falls back to defaults.
func StringSet(values []string, defaults []string) map[string]struct{} {
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

// StringSetContains reports whether value, normalized the same way
// [StringSet] normalizes its input, is a member of values.
func StringSetContains(values map[string]struct{}, value string) bool {
	_, ok := values[strings.ToLower(strings.TrimSpace(value))]
	return ok
}

// SortedStringSetValues returns the set's members in lexical order, so
// evidence and plan payloads stay byte-stable across runs.
func SortedStringSetValues(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// DerivationLimit resolves a configured target limit, using fallback when the
// configured value is unset or non-positive.
func DerivationLimit(raw int, fallback int) int {
	if raw > 0 {
		return raw
	}
	return fallback
}

// DerivedTargetRotationOffset returns the read offset that pages a rotating
// instance through its derived target set, one limit-sized page per interval.
func DerivedTargetRotationOffset(observedAt time.Time, interval time.Duration, limit int) int64 {
	if observedAt.IsZero() || limit <= 0 {
		return 0
	}
	if interval <= 0 {
		interval = DefaultReconcileInterval
	}
	// Index the bucket from the same truncated clock the plan key uses.
	// time.Truncate rounds from Go's zero time, so for an interval that does
	// not divide the zero-time-to-epoch offset (5h, say) an epoch-based
	// UnixNano/interval bucket would flip at a different moment than the plan
	// key and let a rotating instance page to new targets under the old run ID.
	// For an epoch-aligned interval the two indices are identical.
	bucket := observedAt.UTC().Truncate(interval).UnixNano() / int64(interval)
	return bucket * int64(limit)
}

// DerivedTargetRotationOffsetForMode is [DerivedTargetRotationOffset] for a
// rotating instance and a fixed zero offset for a single-pass one.
func DerivedTargetRotationOffsetForMode(
	planningMode string,
	observedAt time.Time,
	interval time.Duration,
	limit int,
) int64 {
	if NormalizePlanningMode(planningMode) == PlanningModeSinglePass {
		return 0
	}
	return DerivedTargetRotationOffset(observedAt, interval, limit)
}

// DerivedTargetPlanKey renders the plan key that makes one interval's derived
// target page idempotent: a stable key for single-pass, an interval-truncated
// timestamp for rotating.
func DerivedTargetPlanKey(prefix string, observedAt time.Time, interval time.Duration, planningMode string) string {
	if prefix == "" {
		prefix = "schedule"
	}
	if NormalizePlanningMode(planningMode) == PlanningModeSinglePass {
		return prefix + "-single-pass"
	}
	if interval <= 0 {
		interval = DefaultReconcileInterval
	}
	return fmt.Sprintf("%s-%s", prefix, observedAt.UTC().Truncate(interval).Format("20060102T150405Z"))
}

// NormalizePlanningMode maps any unrecognized or blank planning mode onto
// [PlanningModeRotating], so an unknown value never silently disables paging.
func NormalizePlanningMode(raw string) string {
	switch strings.TrimSpace(raw) {
	case PlanningModeSinglePass:
		return PlanningModeSinglePass
	default:
		return PlanningModeRotating
	}
}

// DerivedTargetReadLimit widens a target limit by the lookahead row that lets
// a caller detect budget exhaustion.
func DerivedTargetReadLimit(targetLimit int) int {
	if targetLimit <= 0 {
		return targetLimit
	}
	return targetLimit + budgetExhaustionLookahead
}

// ExactOwnedDependencyVersion reports the exact version a dependency pins, and
// false when the declared version is a range, a tag, or a non-registry
// reference that cannot identify one release.
func ExactOwnedDependencyVersion(raw string) (string, bool) {
	version := strings.TrimSpace(raw)
	if version == "" {
		return "", false
	}
	lower := strings.ToLower(version)
	if lower == "latest" || NonVersionOwnedDependencyPrefix(lower) {
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

// NonVersionOwnedDependencyPrefix reports whether lower, an already
// lowercased dependency version, names a source location (a git URL, a local
// file, a workspace link) rather than a published version.
func NonVersionOwnedDependencyPrefix(lower string) bool {
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

// FirstNonBlank returns the first value that is not blank after trimming, or
// "" when every value is blank.
func FirstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
