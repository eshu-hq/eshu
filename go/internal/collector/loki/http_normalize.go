// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package loki

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

func normalizeSeries(raw map[string]string, target TargetConfig, observedAt time.Time) LogSignal {
	labelKeys := sortedKeys(raw)
	if len(labelKeys) == 0 {
		return LogSignal{}
	}
	identityParts := make([]string, 0, len(labelKeys)*2)
	for _, key := range labelKeys {
		identityParts = append(identityParts, key, raw[key])
	}
	fp := fingerprintJoined(identityParts...)
	out := LogSignal{
		ProviderObjectID:   fp,
		SignalKind:         SignalKindSeries,
		LabelKeys:          labelKeys,
		SeriesFingerprint:  fp,
		DeclaredMatchState: declaredMatchState(target, fp),
	}
	if isStale(time.Time{}, observedAt, target.StaleAfter) {
		out.FreshnessState = FreshnessStale
		out.Outcome = OutcomeStale
	}
	if target.ObservedOnlyHint && out.DeclaredMatchState != MatchStateMatchedDeclared {
		out.ManuallyCreated = true
	}
	return out
}

func normalizeRule(namespace string, group ruleGroupResource, raw ruleResource, target TargetConfig, observedAt time.Time) Rule {
	namespace = strings.TrimSpace(namespace)
	groupName := strings.TrimSpace(group.Name)
	ruleName, ruleType := ruleNameAndType(raw)
	identity := ruleIdentity(namespace, groupName, ruleName, raw.Expr)
	out := Rule{
		ProviderObjectID:   identity,
		Namespace:          namespace,
		GroupName:          groupName,
		RuleName:           ruleName,
		RuleType:           ruleType,
		QueryRedacted:      strings.TrimSpace(raw.Expr) != "",
		LabelKeys:          sortedAnyKeys(raw.Labels),
		AnnotationKeys:     sortedAnyKeys(raw.Annotations),
		DeclaredMatchState: declaredMatchState(target, identity),
	}
	if isStale(out.LastEvaluationAt, observedAt, target.StaleAfter) {
		out.FreshnessState = FreshnessStale
		out.Outcome = OutcomeStale
	}
	if target.ObservedOnlyHint && out.DeclaredMatchState != MatchStateMatchedDeclared {
		out.ManuallyCreated = true
	}
	return out
}

func ruleNameAndType(raw ruleResource) (string, string) {
	if alert := strings.TrimSpace(raw.Alert); alert != "" {
		return alert, RuleTypeAlerting
	}
	if record := strings.TrimSpace(raw.Record); record != "" {
		return record, RuleTypeRecording
	}
	return "", ""
}

func ruleIdentity(namespace string, groupName string, ruleName string, query string) string {
	switch {
	case namespace != "" && groupName != "" && ruleName != "":
		return namespace + "/" + groupName + ":" + ruleName
	case groupName != "" && ruleName != "":
		return groupName + ":" + ruleName
	case ruleName != "":
		return "rule:" + ruleName
	case strings.TrimSpace(query) != "":
		return fingerprint(query)
	default:
		return ""
	}
}

func allowedLabelValueSet(requested []string, available []string) []string {
	availableSet := map[string]struct{}{}
	for _, label := range available {
		if trimmed := strings.TrimSpace(label); trimmed != "" {
			availableSet[trimmed] = struct{}{}
		}
	}
	out := make([]string, 0, len(requested))
	seen := map[string]struct{}{}
	for _, label := range requested {
		trimmed := strings.TrimSpace(label)
		if trimmed == "" {
			continue
		}
		if _, ok := availableSet[trimmed]; !ok {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func timeQuery(observedAt time.Time) url.Values {
	query := url.Values{}
	if observedAt.IsZero() {
		return query
	}
	query.Set("end", fmt.Sprintf("%d", observedAt.UnixNano()))
	return query
}

func seriesQuery(target TargetConfig, observedAt time.Time) url.Values {
	query := timeQuery(observedAt)
	if !observedAt.IsZero() {
		start := observedAt.Add(-normalizedSeriesLookback(target))
		if start.Before(observedAt) {
			query.Set("start", fmt.Sprintf("%d", start.UnixNano()))
		}
	}
	matchers := cleanStringSlice(target.SeriesMatchers)
	if len(matchers) == 0 {
		matchers = []string{defaultSeriesMatcher}
	}
	for _, matcher := range matchers {
		query.Add("match[]", matcher)
	}
	return query
}

// normalizedSeriesLookback resolves the bounded /loki/api/v1/series `start`
// window. SeriesLookback is its own independent knob: an explicit value wins,
// otherwise it falls back to the generous defaultSeriesLookback. It
// deliberately does NOT inherit StaleAfter -- StaleAfter is a rule-staleness
// concern that was inert for the series window, and repurposing it here would
// silently change the series-fetch window for any deployment that set
// stale_after. Series last active before this window are not observed in the
// current generation and Loki reports no coverage warning for a time-window
// exclusion; widen SeriesLookback when full historical series visibility is
// required.
func normalizedSeriesLookback(target TargetConfig) time.Duration {
	if target.SeriesLookback > 0 {
		return target.SeriesLookback
	}
	return defaultSeriesLookback
}

func declaredMatchState(target TargetConfig, id string) string {
	if _, ok := target.DeclaredIDs[strings.TrimSpace(id)]; ok {
		return MatchStateMatchedDeclared
	}
	return MatchStateNotCompared
}

func normalizedResourceLimit(value int) int {
	switch {
	case value <= 0:
		return defaultResourceLimit
	case value > maxResourceLimit:
		return maxResourceLimit
	default:
		return value
	}
}

func normalizedLabelValueLimit(value int) int {
	switch {
	case value <= 0:
		return defaultLabelValueLimit
	case value > maxLabelValueLimit:
		return maxLabelValueLimit
	default:
		return value
	}
}

func isStale(updatedAt time.Time, observedAt time.Time, staleAfter time.Duration) bool {
	if staleAfter <= 0 || updatedAt.IsZero() || observedAt.IsZero() {
		return false
	}
	return updatedAt.UTC().Before(observedAt.UTC().Add(-staleAfter))
}
