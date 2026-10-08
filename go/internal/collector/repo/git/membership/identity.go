// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
)

// KindGitHubOrg is the selector kind of a GitHub organization listing, the
// only kind the repository_selection_observations table accepts.
const KindGitHubOrg = "github_org"

// selectorIDBytes is how many leading SHA-256 bytes the selector id keeps:
// 16 bytes (32 hex characters, 128 bits) is collision-free for any realistic
// number of selector configurations and keeps the primary key short.
const selectorIDBytes = 16

// Rule is one configured repository selection rule. Kind is "exact" or
// "regex"; Value is the rule text as the collector matches it.
type Rule struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Selector identifies one repository selection configuration. ID is a stable
// hash of everything that decides which repositories a listing selects, so two
// collector instances with different rules on the same org write separate
// observation rows. Owner is the lowercased org.
type Selector struct {
	ID    string
	Kind  string
	Owner string
}

// selectorIdentity is the canonical encoding hashed into a selector id.
// Changing a field name or the version changes every selector id.
type selectorIdentity struct {
	Version         int    `json:"version"`
	SourceMode      string `json:"source_mode"`
	Owner           string `json:"owner"`
	Rules           []Rule `json:"rules"`
	IncludeArchived bool   `json:"include_archived"`
}

// NewGitHubOrgSelector returns the selector for a GitHub org listing. The id
// is the lowercase hex of the first 16 bytes of SHA-256 over a canonical JSON
// encoding of the source mode, the lowercased trimmed org, the rules (kind
// lowercased and trimmed, value trimmed, sorted, duplicates removed) and the
// include-archived flag. Rule values keep their case because exact rules
// match case-sensitively.
func NewGitHubOrgSelector(sourceMode, org string, rules []Rule, includeArchived bool) Selector {
	owner := strings.ToLower(strings.TrimSpace(org))
	normalized := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		normalized = append(normalized, Rule{
			Kind:  strings.ToLower(strings.TrimSpace(rule.Kind)),
			Value: strings.TrimSpace(rule.Value),
		})
	}
	slices.SortFunc(normalized, func(a, b Rule) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Value, b.Value))
	})
	normalized = slices.Compact(normalized)
	// Marshal cannot fail: every field is a string, bool, int, or slice of
	// string pairs.
	encoded, _ := json.Marshal(selectorIdentity{
		Version:         1,
		SourceMode:      strings.TrimSpace(sourceMode),
		Owner:           owner,
		Rules:           normalized,
		IncludeArchived: includeArchived,
	})
	sum := sha256.Sum256(encoded)
	return Selector{
		ID:    hex.EncodeToString(sum[:selectorIDBytes]),
		Kind:  KindGitHubOrg,
		Owner: owner,
	}
}
