// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// pinnedRefPerRepoCap returns the maximum number of pinned refs per repository.
// Reads ESHU_PINNED_REF_PER_REPO_CAP from env; defaults to 3.
func pinnedRefPerRepoCap(getenv func(string) string) int {
	if raw := strings.TrimSpace(getenv("ESHU_PINNED_REF_PER_REPO_CAP")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return 3
}

// pinnedRefFleetCap returns the absolute maximum total pinned-ref worktree
// entries allowed across the entire fleet per sync cycle. Zero means unlimited.
// Reads ESHU_PINNED_REF_FLEET_CAP from env; defaults to 0.
func pinnedRefFleetCap(getenv func(string) string) int {
	if raw := strings.TrimSpace(getenv("ESHU_PINNED_REF_FLEET_CAP")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return n
		}
	}
	return 0
}

// parsePinnedRefsJSON parses ESHU_PINNED_REFS_JSON into a per-repository
// pinned-ref map. The JSON is a flat object mapping normalized repository IDs
// to a string array of ref names. Both branches (e.g. "feature-x") and tags
// (e.g. "v1.0") are supported; tags are fetched via refs/tags refspec.
// Empty means feature off. Each ref is validated against common git ref name
// constraints. Pin counts exceeding the per-repo cap are truncated with a
// warning log at sync time (not rejected at parse time — the parse-time cap
// lives at the sync boundary in createRefWorktrees). Enabler for #5417.
func parsePinnedRefsJSON(raw string) (map[string][]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("parse ESHU_PINNED_REFS_JSON: %w", err)
	}
	result := make(map[string][]string, len(parsed))
	for rawID, rawRefs := range parsed {
		repoID := normalizeRepositoryID(rawID)
		if repoID == "" {
			return nil, fmt.Errorf("ESHU_PINNED_REFS_JSON: empty repository ID after normalization: %q", rawID)
		}
		refs, err := parseRefList(rawRefs)
		if err != nil {
			return nil, fmt.Errorf("ESHU_PINNED_REFS_JSON: repository %q: %w", repoID, err)
		}
		if len(refs) == 0 {
			continue
		}
		existing, ok := result[repoID]
		if !ok {
			result[repoID] = refs
			continue
		}
		// Merge with existing, deduplicated.
		seen := make(map[string]struct{}, len(existing)+len(refs))
		for _, r := range existing {
			seen[r] = struct{}{}
		}
		for _, r := range refs {
			if _, dup := seen[r]; !dup {
				existing = append(existing, r)
				seen[r] = struct{}{}
			}
		}
		result[repoID] = existing
	}
	return result, nil
}

func parseRefList(raw any) ([]string, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case []any:
		if len(v) == 0 {
			return nil, nil
		}
		refs := make([]string, 0, len(v))
		for _, item := range v {
			ref := strings.TrimSpace(fmt.Sprint(item))
			if ref == "" {
				continue
			}
			if err := validateGitRefName(ref); err != nil {
				return nil, err
			}
			refs = append(refs, ref)
		}
		return refs, nil
	case string:
		// Single ref string for one repo.
		ref := strings.TrimSpace(v)
		if ref == "" {
			return nil, nil
		}
		if err := validateGitRefName(ref); err != nil {
			return nil, err
		}
		return []string{ref}, nil
	default:
		return nil, fmt.Errorf("unsupported ref list type %T", raw)
	}
}

// validateGitRefName checks a single ref name (branch or tag) for common git
// constraint violations. It shares the safety rules with normalizeGitBranchName
// (no :, .., \\, whitespace, or leading dash) and adds refs/ prefix and HEAD
// rejection. This is a safety filter, not a full git-check-ref-format equivalent.
func validateGitRefName(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("empty ref name")
	}
	if ref == "HEAD" {
		return fmt.Errorf("invalid ref name %q (HEAD is a pseudo-ref, not a branch or tag)", ref)
	}
	// Reject raw refs/ prefixes — pinned refs are bare names like "main" or "v1.0".
	if strings.HasPrefix(ref, "refs/") {
		return fmt.Errorf("invalid ref name %q: bare ref name required, not refs/ path", ref)
	}
	// The remaining checks mirror normalizeGitBranchName's rules exactly.
	if strings.HasPrefix(ref, "-") ||
		strings.Contains(ref, ":") ||
		strings.Contains(ref, "..") ||
		strings.Contains(ref, "\\") ||
		strings.ContainsAny(ref, " \t\r\n") {
		return fmt.Errorf("invalid ref name %q", ref)
	}
	return nil
}
