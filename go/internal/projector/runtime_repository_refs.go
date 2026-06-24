// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func buildRepositoryRefs(fact facts.Envelope) []content.RepositoryRef {
	if fact.FactKind != "repository" || fact.IsTombstone {
		return nil
	}
	rawEntries := repositoryRefEntries(fact.Payload["git_refs"])
	if len(rawEntries) == 0 {
		return nil
	}

	defaultBranch, _ := payloadString(fact.Payload, "default_branch")
	refsByKey := make(map[string]content.RepositoryRef, len(rawEntries))
	for _, entry := range rawEntries {
		name := refPayloadString(entry, "name")
		headSHA := refPayloadString(entry, "head_sha")
		if name == "" || headSHA == "" {
			continue
		}
		kind := refPayloadString(entry, "kind")
		if kind == "" {
			kind = "branch"
		}
		isDefault := refPayloadBool(entry, "is_default") || name == defaultBranch
		key := kind + "\x00" + name
		refsByKey[key] = content.RepositoryRef{
			Name:       name,
			Kind:       kind,
			HeadSHA:    headSHA,
			Default:    isDefault,
			ObservedAt: fact.ObservedAt,
		}
	}

	keys := make([]string, 0, len(refsByKey))
	for key := range refsByKey {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := refsByKey[keys[i]]
		right := refsByKey[keys[j]]
		if left.Default != right.Default {
			return left.Default
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.Name < right.Name
	})

	refs := make([]content.RepositoryRef, 0, len(keys))
	for _, key := range keys {
		refs = append(refs, refsByKey[key])
	}
	return refs
}

func repositoryRefEntries(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		entries := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			entry, ok := item.(map[string]any)
			if ok {
				entries = append(entries, entry)
			}
		}
		return entries
	default:
		return nil
	}
}

func refPayloadString(payload map[string]any, key string) string {
	if len(payload) == 0 {
		return ""
	}
	value, ok := payload[key]
	if !ok {
		return ""
	}
	text, ok := asString(value)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func refPayloadBool(payload map[string]any, key string) bool {
	if len(payload) == 0 {
		return false
	}
	value, ok := payload[key]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}
