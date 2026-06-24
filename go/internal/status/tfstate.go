// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/tfstatewarning"
)

// TerraformStateLocatorSerial reports the most recent observed state serial for
// one Terraform-state scope, keyed by the scope-level safe locator hash so the
// report never carries raw bucket names, S3 keys, or local file paths.
type TerraformStateLocatorSerial struct {
	SafeLocatorHash string
	BackendKind     string
	Lineage         string
	Serial          int64
	GenerationID    string
	ObservedAt      time.Time
}

// TerraformStateLocatorWarning reports recent warning_fact observations for one
// Terraform-state scope, grouped by warning_kind so operators can spot patterns
// without scanning the full fact stream.
type TerraformStateLocatorWarning struct {
	SafeLocatorHash string
	BackendKind     string
	WarningKind     string
	Reason          string
	Severity        string
	Actionability   string
	Source          string
	SourceHandle    string
	GenerationID    string
	ObservedAt      time.Time
}

// TerraformStateWarningSummary reports bounded Terraform-state warning totals
// by warning kind, reason, and scope class for release-gate readback.
type TerraformStateWarningSummary struct {
	WarningKind   string
	Reason        string
	ScopeClass    string
	Severity      string
	Actionability string
	Count         int
}

// MaxTerraformStateRecentWarnings caps the number of recent warning rows the
// admin status surface will return per safe_locator_hash. Postgres still owns
// the canonical history; this bound prevents the JSON projection from growing
// without limits across restarts.
const MaxTerraformStateRecentWarnings = 50

// CloneTerraformStateSerials returns a defensive copy of a serial slice so the
// report cannot be mutated by callers after rendering.
func CloneTerraformStateSerials(rows []TerraformStateLocatorSerial) []TerraformStateLocatorSerial {
	if len(rows) == 0 {
		return nil
	}
	cloned := make([]TerraformStateLocatorSerial, len(rows))
	copy(cloned, rows)
	return cloned
}

// CloneTerraformStateWarnings returns a defensive copy of a warning slice.
func CloneTerraformStateWarnings(rows []TerraformStateLocatorWarning) []TerraformStateLocatorWarning {
	if len(rows) == 0 {
		return nil
	}
	cloned := make([]TerraformStateLocatorWarning, len(rows))
	copy(cloned, rows)
	for i := range cloned {
		if strings.TrimSpace(cloned[i].Severity) != "" && strings.TrimSpace(cloned[i].Actionability) != "" {
			continue
		}
		classification, ok := tfstatewarning.Classify(cloned[i].WarningKind, cloned[i].Reason)
		if !ok {
			continue
		}
		if strings.TrimSpace(cloned[i].Severity) == "" {
			cloned[i].Severity = classification.Severity
		}
		if strings.TrimSpace(cloned[i].Actionability) == "" {
			cloned[i].Actionability = classification.Actionability
		}
	}
	return cloned
}

// SortTerraformStateSerials orders serial rows deterministically by safe
// locator hash so JSON output is stable across reads.
func SortTerraformStateSerials(rows []TerraformStateLocatorSerial) []TerraformStateLocatorSerial {
	cloned := CloneTerraformStateSerials(rows)
	sort.SliceStable(cloned, func(i, j int) bool {
		left := strings.TrimSpace(cloned[i].SafeLocatorHash)
		right := strings.TrimSpace(cloned[j].SafeLocatorHash)
		return left < right
	})
	return cloned
}

// SortTerraformStateWarnings orders warnings deterministically by safe locator
// hash, then warning_kind, then ObservedAt descending. The Postgres query is
// expected to bound the input; this enforces ordering for stable JSON output.
func SortTerraformStateWarnings(rows []TerraformStateLocatorWarning) []TerraformStateLocatorWarning {
	cloned := CloneTerraformStateWarnings(rows)
	sort.SliceStable(cloned, func(i, j int) bool {
		left := cloned[i]
		right := cloned[j]
		if left.SafeLocatorHash != right.SafeLocatorHash {
			return left.SafeLocatorHash < right.SafeLocatorHash
		}
		if left.WarningKind != right.WarningKind {
			return left.WarningKind < right.WarningKind
		}
		return left.ObservedAt.After(right.ObservedAt)
	})
	return cloned
}

// GroupTerraformStateWarningsByKind buckets warnings per safe locator hash and
// warning_kind, returning a map keyed first by SafeLocatorHash then WarningKind.
// The Postgres query already caps results per locator; the grouping here only
// projects the bounded input into operator-friendly shape.
func GroupTerraformStateWarningsByKind(
	rows []TerraformStateLocatorWarning,
) map[string]map[string][]TerraformStateLocatorWarning {
	if len(rows) == 0 {
		return map[string]map[string][]TerraformStateLocatorWarning{}
	}
	grouped := map[string]map[string][]TerraformStateLocatorWarning{}
	for _, row := range rows {
		hash := strings.TrimSpace(row.SafeLocatorHash)
		kind := strings.TrimSpace(row.WarningKind)
		if hash == "" || kind == "" {
			continue
		}
		if _, ok := grouped[hash]; !ok {
			grouped[hash] = map[string][]TerraformStateLocatorWarning{}
		}
		grouped[hash][kind] = append(grouped[hash][kind], row)
	}
	return grouped
}

// SummarizeTerraformStateWarnings returns deterministic aggregate warning
// totals. ScopeClass is currently the sanitized backend kind because that is
// the stable public class for a Terraform-state scope without exposing raw
// locators.
func SummarizeTerraformStateWarnings(rows []TerraformStateLocatorWarning) []TerraformStateWarningSummary {
	if len(rows) == 0 {
		return nil
	}
	type key struct {
		warningKind   string
		reason        string
		scopeClass    string
		severity      string
		actionability string
	}
	counts := map[key]int{}
	for _, row := range rows {
		warningKind := strings.TrimSpace(row.WarningKind)
		reason := strings.TrimSpace(row.Reason)
		if warningKind == "" || reason == "" {
			continue
		}
		scopeClass := strings.ToLower(strings.TrimSpace(row.BackendKind))
		if scopeClass == "" {
			scopeClass = "unknown"
		}
		severity := strings.TrimSpace(row.Severity)
		actionability := strings.TrimSpace(row.Actionability)
		if severity == "" || actionability == "" {
			if classification, ok := tfstatewarning.Classify(warningKind, reason); ok {
				severity = classification.Severity
				actionability = classification.Actionability
			}
		}
		counts[key{
			warningKind:   warningKind,
			reason:        reason,
			scopeClass:    scopeClass,
			severity:      severity,
			actionability: actionability,
		}]++
	}
	if len(counts) == 0 {
		return nil
	}
	keys := make([]key, 0, len(counts))
	for summaryKey := range counts {
		keys = append(keys, summaryKey)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].warningKind != keys[j].warningKind {
			return keys[i].warningKind < keys[j].warningKind
		}
		if keys[i].reason != keys[j].reason {
			return keys[i].reason < keys[j].reason
		}
		if keys[i].scopeClass != keys[j].scopeClass {
			return keys[i].scopeClass < keys[j].scopeClass
		}
		if keys[i].severity != keys[j].severity {
			return keys[i].severity < keys[j].severity
		}
		return keys[i].actionability < keys[j].actionability
	})
	summaries := make([]TerraformStateWarningSummary, 0, len(keys))
	for _, summaryKey := range keys {
		summaries = append(summaries, TerraformStateWarningSummary{
			WarningKind:   summaryKey.warningKind,
			Reason:        summaryKey.reason,
			ScopeClass:    summaryKey.scopeClass,
			Severity:      summaryKey.severity,
			Actionability: summaryKey.actionability,
			Count:         counts[summaryKey],
		})
	}
	return summaries
}
