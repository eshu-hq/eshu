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
	branchesByName := make(map[string]GitRef)
	tagsByName := make(map[string]GitRef)
	// peeledTagSHAs tracks the committed SHA for annotated tags whose peeled
	// (^{}) line arrived before the tag-object line, which can happen because
	// git ls-remote output order is not guaranteed.
	peeledTagSHAs := make(map[string]string)
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
		if strings.HasPrefix(fields[1], "refs/heads/") {
			branch, err := normalizeGitBranchName(strings.TrimPrefix(fields[1], "refs/heads/"))
			if err != nil {
				return nil, err
			}
			headSHA := strings.TrimSpace(fields[0])
			if branch == "" || headSHA == "" {
				continue
			}
			branchesByName[branch] = GitRef{
				Name:    branch,
				Kind:    "branch",
				HeadSHA: headSHA,
			}
			continue
		}
		if strings.HasPrefix(fields[1], "refs/tags/") {
			refspec := fields[1]
			if strings.HasSuffix(refspec, "^{}") {
				// Peeled line of an annotated tag — carry the object SHA.
				// Note: git ls-remote does not report the object type, so
				// the peeled SHA is always the dereferenced OBJECT (a commit
				// for normal release tags, a blob for annotated tags of blobs,
				// a tree for annotated tags of trees). Eshu stores the peeled
				// object SHA as-is; it does not guarantee the SHA is a commit.
				tagName, err := normalizeGitTagName(strings.TrimSuffix(strings.TrimPrefix(refspec, "refs/tags/"), "^{}"))
				if err != nil {
					return nil, err
				}
				if tagName != "" {
					peeledTagSHAs[tagName] = strings.TrimSpace(fields[0])
				}
				continue
			}
			tagName, err := normalizeGitTagName(strings.TrimPrefix(refspec, "refs/tags/"))
			if err != nil {
				return nil, err
			}
			if tagName == "" {
				continue
			}
			headSHA := strings.TrimSpace(fields[0])
			// When a peeled SHA is already known (^{} line arrived first),
			// prefer the peeled commit SHA over the tag object SHA.
			if peeled, ok := peeledTagSHAs[tagName]; ok {
				headSHA = peeled
			}
			if headSHA == "" {
				continue
			}
			tagsByName[tagName] = GitRef{
				Name:    tagName,
				Kind:    "tag",
				HeadSHA: headSHA,
			}
			continue
		}
	}
	// When the tag-object line arrived before the peeled line, replace the
	// stored SHA with the peeled commit SHA.
	for tagName, peeledSHA := range peeledTagSHAs {
		if existing, ok := tagsByName[tagName]; ok {
			existing.HeadSHA = peeledSHA
			tagsByName[tagName] = existing
		}
	}

	// Collect sorted branch names, tagged as default where applicable.
	branchNames := make([]string, 0, len(branchesByName))
	for name := range branchesByName {
		branchNames = append(branchNames, name)
	}
	sort.Strings(branchNames)

	tagNames := make([]string, 0, len(tagsByName))
	for name := range tagsByName {
		tagNames = append(tagNames, name)
	}
	sort.Strings(tagNames)

	refs := make([]GitRef, 0, len(branchNames)+len(tagNames))
	for _, name := range branchNames {
		ref := branchesByName[name]
		ref.Default = name == defaultBranch
		refs = append(refs, ref)
	}
	for _, name := range tagNames {
		ref := tagsByName[name]
		ref.Default = false // Tags are never the default branch.
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
		"refs/tags/*",
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

// normalizeGitTagName validates a Git tag name against the same safety rules as
// normalizeGitBranchName. Git forbids `:`, `..`, `\\`, whitespace, and control
// characters in all ref names, including tags.
func normalizeGitTagName(tag string) (string, error) {
	tag = strings.TrimSpace(tag)
	tag = strings.TrimPrefix(tag, "refs/tags/")
	if tag == "" {
		return "", nil
	}
	if strings.HasPrefix(tag, "-") ||
		strings.Contains(tag, ":") ||
		strings.Contains(tag, "..") ||
		strings.Contains(tag, "\\") ||
		strings.Contains(tag, "^{}") ||
		strings.ContainsAny(tag, " \t\r\n") {
		return "", fmt.Errorf("invalid git tag name %q", tag)
	}
	return tag, nil
}
