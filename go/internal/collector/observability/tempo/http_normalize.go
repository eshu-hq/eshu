// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

type tagSearchResponse struct {
	Scopes   []tagScope `json:"scopes"`
	TagNames []string   `json:"tagNames"`
}

type tagScope struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

type tagValuesResponse struct {
	TagValues []tagValue `json:"tagValues"`
}

type tagValue struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func normalizeTagSet(response tagSearchResponse, target TargetConfig) TraceSignal {
	tagSet := map[string]struct{}{}
	for _, tag := range response.TagNames {
		if trimmed := strings.TrimSpace(tag); trimmed != "" {
			tagSet[trimmed] = struct{}{}
		}
	}
	for _, scope := range response.Scopes {
		for _, tag := range scope.Tags {
			if trimmed := strings.TrimSpace(tag); trimmed != "" {
				tagSet[unscopedTagName(trimmed)] = struct{}{}
			}
		}
	}
	tagKeys := sortedKeys(tagSet)
	if len(tagKeys) == 0 {
		return TraceSignal{}
	}
	identity := fingerprintJoined("tags", strings.Join(tagKeys, ","))
	out := TraceSignal{
		ProviderObjectID:   identity,
		SignalKind:         SignalKindTagSet,
		TagKeys:            tagKeys,
		DeclaredMatchState: declaredMatchState(target, identity),
	}
	if target.ObservedOnlyHint && out.DeclaredMatchState != MatchStateMatchedDeclared {
		out.ManuallyCreated = true
	}
	return out
}

func normalizeTagValues(tagName string, response tagValuesResponse, target TargetConfig) TraceSignal {
	values := map[string]struct{}{}
	for _, raw := range response.TagValues {
		if value := strings.TrimSpace(raw.Value); value != "" {
			values[value] = struct{}{}
		}
	}
	count := len(values)
	identity := fingerprintJoined("tag_values", tagName)
	if identity == "" {
		return TraceSignal{}
	}
	signal := TraceSignal{
		ProviderObjectID:   identity,
		SignalKind:         SignalKindTagValues,
		TagScope:           tagScopeFromName(tagName),
		TagName:            strings.TrimSpace(tagName),
		TagValueCount:      count,
		DeclaredMatchState: declaredMatchState(target, identity),
	}
	if count <= normalizedTagValueLimit(target.MaxTagValuesPerTag) {
		for value := range values {
			if fp := fingerprint(value); fp != "" {
				signal.TagValueHashes = append(signal.TagValueHashes, fp)
			}
		}
		signal.TagValueHashes = cleanStringSlice(signal.TagValueHashes)
	}
	if target.ObservedOnlyHint && signal.DeclaredMatchState != MatchStateMatchedDeclared {
		signal.ManuallyCreated = true
	}
	return signal
}

func tagAvailable(tagName string, available []string) bool {
	want := unscopedTagName(tagName)
	for _, availableTag := range available {
		if unscopedTagName(availableTag) == want {
			return true
		}
	}
	return false
}

func unscopedTagName(tagName string) string {
	tagName = strings.TrimPrefix(strings.TrimSpace(tagName), ".")
	for _, prefix := range []string{"resource.", "span.", "intrinsic.", "event.", "link.", "instrumentation."} {
		if strings.HasPrefix(tagName, prefix) {
			return strings.TrimPrefix(tagName, prefix)
		}
	}
	return tagName
}

func tagScopeFromName(tagName string) string {
	tagName = strings.TrimPrefix(strings.TrimSpace(tagName), ".")
	if index := strings.Index(tagName, "."); index > 0 {
		scope := tagName[:index]
		switch scope {
		case "resource", "span", "intrinsic", "event", "link", "instrumentation":
			return scope
		}
	}
	return ""
}

func queryRange(observedAt time.Time, lookback time.Duration) url.Values {
	query := url.Values{}
	if observedAt.IsZero() {
		return query
	}
	if lookback <= 0 {
		lookback = defaultLookback
	}
	end := observedAt.UTC()
	query.Set("start", strconv.FormatInt(end.Add(-lookback).Unix(), 10))
	query.Set("end", strconv.FormatInt(end.Unix(), 10))
	return query
}

func withQueryLimit(query url.Values, limit int) url.Values {
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	return query
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

func normalizedTagValueLimit(value int) int {
	switch {
	case value <= 0:
		return defaultTagValueLimit
	case value > maxTagValueLimit:
		return maxTagValueLimit
	default:
		return value
	}
}
