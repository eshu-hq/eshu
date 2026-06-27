// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"regexp"
	"strings"
)

// chefCookbookStartPattern marks the first line of a Berksfile `cookbook`
// declaration. A declaration runs from this line up to (but not including) the
// next `cookbook` line, so its git source can be matched within its own block
// without bleeding across a neighbouring declaration.
var chefCookbookStartPattern = regexp.MustCompile(`(?i)^\s*cookbook\s+['"]`)

// chefCookbookNamePattern captures the cookbook name from a `cookbook`
// declaration's first line.
var chefCookbookNamePattern = regexp.MustCompile(`(?i)^\s*cookbook\s+['"]([^'"]+)['"]`)

// chefCookbookGitPattern captures the git source URL from a single `cookbook`
// block. Both the new-hash form (git: 'URL') and the symbol-rocket form
// (:git => 'URL') are accepted. Only cookbooks with an explicit git source are
// matched: a Chef Supermarket / forge cookbook with only a version constraint
// (cookbook 'nginx', '~> 12.0') has no repository identity to resolve and must
// never fabricate an edge.
var chefCookbookGitPattern = regexp.MustCompile(`(?i)\bgit\b\s*(?:=>|:)\s*['"]([^'"]+)['"]`)

// chefCookbookBlocks splits a Berksfile into one string per `cookbook`
// declaration, each spanning the `cookbook` line and its continuation lines (the
// git/ref/branch hash arguments). Content before the first `cookbook` line is
// discarded. Splitting per declaration keeps a forge-only `cookbook` that
// precedes a git-backed `cookbook` from mis-attributing the later git source to
// the earlier cookbook.
func chefCookbookBlocks(content string) []string {
	var blocks []string
	var current []string
	flush := func() {
		if len(current) > 0 {
			blocks = append(blocks, strings.Join(current, "\n"))
			current = nil
		}
	}
	for _, raw := range strings.Split(content, "\n") {
		line := stripChefComment(raw)
		if chefCookbookStartPattern.MatchString(line) {
			flush()
			current = []string{line}
			continue
		}
		if len(current) > 0 {
			current = append(current, line)
		}
	}
	flush()
	return blocks
}

// stripChefComment removes a trailing Ruby `#` comment from a Berksfile line,
// ignoring `#` inside single- or double-quoted strings so a real git URL (which
// may legitimately contain a `#` fragment) is never truncated. A commented or
// disabled `cookbook`/`git:` line therefore yields no evidence. This duplicates
// the Puppet emitter's quote-aware logic on purpose, keeping the Chef path
// isolated from puppet_evidence.go.
func stripChefComment(line string) string {
	inSingle, inDouble := false, false
	for i, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return line[:i]
			}
		}
	}
	return line
}

// isChefArtifact reports whether the file is a Berkshelf Berksfile. The Berksfile
// has no extension, so detection is by exact basename.
func isChefArtifact(filePath string) bool {
	return fileBaseName(filePath) == "Berksfile"
}

// discoverChefEvidence extracts DEPENDS_ON evidence from a Berksfile. Each
// `cookbook` declaration with an explicit git source names the cookbook-owning
// repository; the git URL is resolved against the catalog and emitted as
// CHEF_COOKBOOK_DEPENDENCY evidence so the resolver can project a generic
// DEPENDS_ON edge that the B-7 gate isolates by evidence kind.
func discoverChefEvidence(
	sourceRepoID, filePath, content string,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact
	for _, block := range chefCookbookBlocks(content) {
		nameMatch := chefCookbookNamePattern.FindStringSubmatch(block)
		if nameMatch == nil {
			continue
		}
		gitMatch := chefCookbookGitPattern.FindStringSubmatch(block)
		if gitMatch == nil {
			// Forge-only cookbook (no git source) — no repository identity to resolve.
			continue
		}
		cookbookName := strings.TrimSpace(nameMatch[1])
		gitURL := strings.TrimSpace(gitMatch[1])
		if gitURL == "" {
			continue
		}
		repositoryName := normalizeRepositoryURLName(gitURL)
		details := withFirstPartyRefDetails(
			map[string]any{
				"cookbook_name":   cookbookName,
				"git_source":      gitURL,
				"repository_name": repositoryName,
			},
			"chef_cookbook_git_source",
			cookbookName,
			"",
			"",
			"",
			repositoryName,
		)
		evidence = append(evidence, matchCatalog(
			sourceRepoID,
			gitURL,
			filePath,
			EvidenceKindChefCookbookDependency,
			RelDependsOn,
			DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindChefCookbookDependency),
			"Chef cookbook git source references the target repository",
			"chef",
			matcher,
			seen,
			details,
		)...)
	}
	return evidence
}
