// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"regexp"
	"strings"
)

// puppetModStartPattern marks the first line of an r10k Puppetfile `mod`
// declaration. A declaration runs from this line up to (but not including) the
// next `mod` line, so its git source can be matched within its own block without
// bleeding across a neighbouring declaration.
var puppetModStartPattern = regexp.MustCompile(`(?i)^\s*mod\s+['"]`)

// puppetModNamePattern captures the module name from a `mod` declaration's first
// line.
var puppetModNamePattern = regexp.MustCompile(`(?i)^\s*mod\s+['"]([^'"]+)['"]`)

// puppetModGitPattern captures the git source URL from a single `mod` block.
// Both the symbol-rocket form (:git => 'URL') and the new-hash form (git: 'URL')
// are accepted. Only modules with an explicit git source are matched: a Puppet
// Forge slug (mod 'puppetlabs-stdlib', '9.4.0') has no repository identity to
// resolve and must never fabricate an edge.
var puppetModGitPattern = regexp.MustCompile(`(?i)\bgit\b\s*(?:=>|:)\s*['"]([^'"]+)['"]`)

// puppetModBlocks splits a Puppetfile into one string per `mod` declaration,
// each spanning the `mod` line and its continuation lines (the git/ref hash
// arguments). Content before the first `mod` line is discarded. Splitting per
// declaration keeps a forge-only `mod` that precedes a git-backed `mod` from
// mis-attributing the later git source to the earlier module.
func puppetModBlocks(content string) []string {
	var blocks []string
	var current []string
	flush := func() {
		if len(current) > 0 {
			blocks = append(blocks, strings.Join(current, "\n"))
			current = nil
		}
	}
	for _, raw := range strings.Split(content, "\n") {
		line := stripPuppetComment(raw)
		if puppetModStartPattern.MatchString(line) {
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

// stripPuppetComment removes a trailing Ruby/Puppet `#` comment from a line,
// ignoring `#` inside single- or double-quoted strings so a real git URL (which
// may legitimately contain a `#` fragment) is never truncated. A commented or
// disabled `mod`/`:git =>` line therefore yields no evidence.
func stripPuppetComment(line string) string {
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

// isPuppetArtifact reports whether the file is an r10k Puppetfile. The Puppetfile
// has no extension, so detection is by exact basename.
func isPuppetArtifact(filePath string) bool {
	return fileBaseName(filePath) == "Puppetfile"
}

// discoverPuppetEvidence extracts DEPENDS_ON evidence from a Puppetfile. Each
// `mod` declaration with an explicit git source names the module-owning
// repository; the git URL is resolved against the catalog and emitted as
// PUPPET_MODULE_REFERENCE evidence so the resolver can project a generic
// DEPENDS_ON edge that the B-7 gate isolates by evidence kind.
func discoverPuppetEvidence(
	sourceRepoID, filePath, content string,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact
	for _, block := range puppetModBlocks(content) {
		nameMatch := puppetModNamePattern.FindStringSubmatch(block)
		if nameMatch == nil {
			continue
		}
		gitMatch := puppetModGitPattern.FindStringSubmatch(block)
		if gitMatch == nil {
			// Forge-only module (no git source) — no repository identity to resolve.
			continue
		}
		moduleName := strings.TrimSpace(nameMatch[1])
		gitURL := strings.TrimSpace(gitMatch[1])
		if gitURL == "" {
			continue
		}
		repositoryName := normalizeRepositoryURLName(gitURL)
		details := withFirstPartyRefDetails(
			map[string]any{
				"module_name":     moduleName,
				"git_source":      gitURL,
				"repository_name": repositoryName,
			},
			"puppet_module_git_source",
			moduleName,
			"",
			"",
			"",
			repositoryName,
		)
		evidence = append(evidence, matchCatalog(
			sourceRepoID,
			gitURL,
			filePath,
			EvidenceKindPuppetModuleReference,
			RelDependsOn,
			DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindPuppetModuleReference),
			"Puppet module git source references the target repository",
			"puppet",
			matcher,
			seen,
			details,
		)...)
	}
	return evidence
}
