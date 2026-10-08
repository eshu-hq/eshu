// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"slices"
	"strings"
)

// Selector kinds the repository_selection_observations table accepts.
const (
	// KindGitHubOrg is a GitHub organization listing. Its rows cover every
	// known scope in the org: selected, excluded, or not listed.
	KindGitHubOrg = "github_org"
	// KindExplicit is an explicit repository list. Its rows are only ever
	// selected, one per configured repository that already has a scope.
	KindExplicit = "explicit"
)

// gitHubHost is the remote host every githubOrg listing covers: the listing
// reads api.github.com and its clones use github.com remotes.
const gitHubHost = "github.com"

// KnownScopeHost returns the remote host a selector kind's known scopes must
// carry. A github_org selector only judges github.com scopes, so a gitlab.com
// or GitHub Enterprise scope whose slug shares the org's owner never reads
// not_listed. Other kinds return "" (no host filter): an explicit selector
// matches scopes by exact id and writes only selected rows.
func KnownScopeHost(kind string) string {
	if kind == KindGitHubOrg {
		return gitHubHost
	}
	return ""
}

// selectorIDBytes is how many leading SHA-256 bytes the selector id keeps:
// 16 bytes (32 hex characters, 128 bits) is collision-free for any realistic
// number of selector configurations and keeps the primary key short.
const selectorIDBytes = 16

// tokenPrincipalSalt domain-separates the token principal hash from any other
// SHA-256 of the same token.
const tokenPrincipalSalt = "eshu-repo-selector:" // #nosec G101 -- public hash domain prefix, not a credential

// anonymousPrincipal names a selector whose collector has no GitHub
// credential, such as an explicit list cloned over SSH.
const anonymousPrincipal = "anonymous"

// Rule is one configured repository selection rule. Kind is "exact" or
// "regex"; Value is the rule text as the collector matches it.
type Rule struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Selector identifies one repository selection configuration. ID is
// "<config hash>@<principal>": the hash covers everything that decides which
// repositories a listing selects, and the principal names the credential that
// listed them, so two collectors with different rules or credentials on the
// same owner write separate observation rows. Owner is the lowercased org or
// user.
type Selector struct {
	ID    string
	Kind  string
	Owner string
}

// selectorIdentity is the canonical encoding hashed into a selector id.
// Changing a field name or the version changes every selector id.
type selectorIdentity struct {
	Version         int    `json:"version"`
	Kind            string `json:"kind"`
	SourceMode      string `json:"source_mode"`
	Owner           string `json:"owner"`
	Rules           []Rule `json:"rules"`
	IncludeArchived bool   `json:"include_archived"`
}

// GitHubAppPrincipal returns the selector principal of a GitHub App
// installation: "app:<app_id>:<installation_id>".
func GitHubAppPrincipal(appID, installationID string) string {
	return "app:" + strings.TrimSpace(appID) + ":" + strings.TrimSpace(installationID)
}

// TokenPrincipal returns the selector principal of a GitHub token: "token:"
// and the first 16 hex characters of SHA-256 over a fixed salt and the token.
// The token itself never appears in the principal, a selector id, a log, or
// an error; rotating the token yields a new principal.
func TokenPrincipal(token string) string {
	sum := sha256.Sum256([]byte(tokenPrincipalSalt + token))
	return "token:" + hex.EncodeToString(sum[:])[:16]
}

// NewGitHubOrgSelector returns the selector for a GitHub org listing listed
// with principal (see GitHubAppPrincipal and TokenPrincipal; blank means
// "anonymous").
//
// The config hash is the lowercase hex of the first 16 bytes of SHA-256 over
// a canonical JSON encoding of the kind, source mode, the lowercased trimmed
// org, the rules, and the include-archived flag. Rules are canonicalized
// before hashing: kind lowercased and trimmed, exact values normalized the way
// the collector matches repository ids (separators unified, empty, "." and
// ".." segments dropped), regex values trimmed, then sorted and deduplicated.
// Values keep their case because rules match case-sensitively.
func NewGitHubOrgSelector(sourceMode, org string, rules []Rule, includeArchived bool, principal string) Selector {
	return newSelector(KindGitHubOrg, sourceMode, org, rules, includeArchived, principal)
}

// NewExplicitSelector returns the selector for the configured explicit
// repositories under owner, listed with principal. Its id follows the
// NewGitHubOrgSelector encoding with the explicit kind, so an explicit list
// and an org listing never share a selector.
func NewExplicitSelector(sourceMode, owner string, rules []Rule, principal string) Selector {
	return newSelector(KindExplicit, sourceMode, owner, rules, false, principal)
}

func newSelector(kind, sourceMode, owner string, rules []Rule, includeArchived bool, principal string) Selector {
	owner = strings.ToLower(strings.TrimSpace(owner))
	normalized := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		ruleKind := strings.ToLower(strings.TrimSpace(rule.Kind))
		value := strings.TrimSpace(rule.Value)
		if ruleKind == "exact" {
			value = normalizeRepositoryID(value)
		}
		normalized = append(normalized, Rule{Kind: ruleKind, Value: value})
	}
	slices.SortFunc(normalized, func(a, b Rule) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Value, b.Value))
	})
	normalized = slices.Compact(normalized)
	// Marshal cannot fail: every field is a string, bool, int, or slice of
	// string pairs.
	encoded, _ := json.Marshal(selectorIdentity{
		Version:         2,
		Kind:            kind,
		SourceMode:      strings.TrimSpace(sourceMode),
		Owner:           owner,
		Rules:           normalized,
		IncludeArchived: includeArchived,
	})
	sum := sha256.Sum256(encoded)
	principal = strings.TrimSpace(principal)
	if principal == "" {
		principal = anonymousPrincipal
	}
	return Selector{
		ID:    hex.EncodeToString(sum[:selectorIDBytes]) + "@" + principal,
		Kind:  kind,
		Owner: owner,
	}
}

// normalizeRepositoryID is the repository id normalization the git collector
// applies before matching an exact rule. It must stay identical to the git
// package's normalizeRepositoryID; TestSelectorNormalizesExactRulesLikeMatching
// in that package pins the equivalence.
func normalizeRepositoryID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == '/' || r == '\\' })
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			continue
		}
		clean = append(clean, part)
	}
	return path.Clean(strings.Join(clean, "/"))
}
