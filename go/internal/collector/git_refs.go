// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// GitRef captures one source-observed Git reference head for a repository.
type GitRef struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	HeadSHA string `json:"head_sha"`
	Default bool   `json:"is_default,omitempty"`
}

func parseRemoteGitRefs(output string) ([]GitRef, error) {
	defaultBranch := ""
	refsByName := make(map[string]GitRef)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] == "ref:" && strings.HasPrefix(fields[1], "refs/heads/") {
			branch, err := normalizeGitBranchName(strings.TrimPrefix(fields[1], "refs/heads/"))
			if err != nil {
				return nil, err
			}
			defaultBranch = branch
			continue
		}
		if !strings.HasPrefix(fields[1], "refs/heads/") {
			continue
		}
		branch, err := normalizeGitBranchName(strings.TrimPrefix(fields[1], "refs/heads/"))
		if err != nil {
			return nil, err
		}
		headSHA := strings.TrimSpace(fields[0])
		if branch == "" || headSHA == "" {
			continue
		}
		refsByName[branch] = GitRef{
			Name:    branch,
			Kind:    "branch",
			HeadSHA: headSHA,
		}
	}

	names := make([]string, 0, len(refsByName))
	for name := range refsByName {
		names = append(names, name)
	}
	sort.Strings(names)

	refs := make([]GitRef, 0, len(names))
	for _, name := range names {
		ref := refsByName[name]
		ref.Default = name == defaultBranch
		refs = append(refs, ref)
	}
	return refs, nil
}

func remoteGitRefs(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	token string,
) ([]GitRef, error) {
	output, err := gitRun(
		ctx,
		repoPath,
		config,
		token,
		"ls-remote",
		"--symref",
		"origin",
		"HEAD",
		"refs/heads/*",
	)
	if err != nil {
		return nil, fmt.Errorf("list remote git refs for %q: %w", repoPath, err)
	}
	return parseRemoteGitRefs(output)
}

func cloneGitRefs(refs []GitRef) []GitRef {
	if len(refs) == 0 {
		return nil
	}
	cloned := make([]GitRef, len(refs))
	copy(cloned, refs)
	return cloned
}

func repositoryDefaultBranch(refs []GitRef) string {
	for _, ref := range refs {
		if ref.Default {
			return strings.TrimSpace(ref.Name)
		}
	}
	return ""
}

func repositoryFactGitRefsPayload(refs []GitRef) []map[string]any {
	if len(refs) == 0 {
		return nil
	}
	payload := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		name := strings.TrimSpace(ref.Name)
		headSHA := strings.TrimSpace(ref.HeadSHA)
		if name == "" || headSHA == "" {
			continue
		}
		kind := strings.TrimSpace(ref.Kind)
		if kind == "" {
			kind = "branch"
		}
		payload = append(payload, map[string]any{
			"name":       name,
			"kind":       kind,
			"head_sha":   headSHA,
			"is_default": ref.Default,
		})
	}
	return payload
}
