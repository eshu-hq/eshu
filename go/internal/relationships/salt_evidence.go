// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// isSaltGitfsArtifact reports whether the content is a Salt master/minion config
// that declares gitfs formula remotes. Detection requires a TOP-LEVEL
// `gitfs_remotes:` mapping key (column 0), not a bare substring, so a Compose /
// GitHub Actions / other YAML file that merely mentions the string in a comment,
// env value, or nested field is NOT misrouted to the Salt emitter. The dispatch
// in evidence.go also runs this content probe AFTER every artifact-type-specific
// case (docker_compose, github_actions_workflow, …) so a known artifact type is
// always handled by its own extractor first; this probe only catches the generic
// Salt config that no other case claims.
func isSaltGitfsArtifact(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		// Top-level YAML keys sit at column 0; a leading space means the key is
		// nested under another mapping and is not the Salt gitfs declaration.
		if strings.HasPrefix(line, "gitfs_remotes:") {
			return true
		}
	}
	return false
}

// discoverSaltEvidence extracts DEPENDS_ON evidence from a Salt config that lists
// formula git repositories under gitfs_remotes. Each remote URL names the
// formula-owning repository; the URL is resolved against the catalog and emitted
// as SALT_FORMULA_REFERENCE evidence so the resolver can project a generic
// DEPENDS_ON edge that the B-7 gate isolates by evidence kind.
//
// A gitfs_remotes entry is written one of two ways in real Salt configs: a plain
// URL string, or a single-key map whose key is the URL and whose value carries
// per-remote options (root, base, ssl_verify, ...). Both shapes are handled; a
// non-gitfs YAML or a config with no gitfs_remotes list yields no evidence.
func discoverSaltEvidence(
	sourceRepoID, filePath, content string,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact
	for _, url := range saltGitfsRemoteURLs(content) {
		gitURL := strings.TrimSpace(url)
		if gitURL == "" {
			continue
		}
		repositoryName := normalizeRepositoryURLName(gitURL)
		details := withFirstPartyRefDetails(
			map[string]any{
				"git_source":      gitURL,
				"repository_name": repositoryName,
			},
			"salt_gitfs_remote",
			repositoryName,
			"",
			"",
			"",
			repositoryName,
		)
		evidence = append(evidence, matchCatalog(
			sourceRepoID,
			gitURL,
			filePath,
			EvidenceKindSaltFormulaReference,
			RelDependsOn,
			DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindSaltFormulaReference),
			"Salt gitfs formula source references the target repository",
			"salt",
			matcher,
			seen,
			details,
		)...)
	}
	return evidence
}

// saltGitfsRemoteURLs parses Salt config YAML and returns every gitfs_remotes
// entry's git URL in document order. A yaml.Node walk is used (rather than a
// plain map decode) so a single-key map entry's URL — the map key — is read
// deterministically as the first key, matching how per-remote-options gitfs
// entries are written.
func saltGitfsRemoteURLs(content string) []string {
	decoder := yaml.NewDecoder(strings.NewReader(content))
	var urls []string
	for {
		var doc yaml.Node
		if err := decoder.Decode(&doc); err != nil {
			break
		}
		root := documentRoot(&doc)
		if root == nil {
			continue
		}
		urls = append(urls, gitfsRemotesFromMapping(root)...)
	}
	return uniqueStrings(urls)
}

// documentRoot returns the mapping node at the root of a parsed YAML document,
// unwrapping the document node wrapper. A non-mapping root yields nil.
func documentRoot(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil
		}
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	return node
}

// gitfsRemotesFromMapping reads the gitfs_remotes sequence from a top-level Salt
// config mapping and returns each entry's URL. A plain scalar entry is the URL
// itself; a single-key mapping entry's first key is the URL.
func gitfsRemotesFromMapping(mapping *yaml.Node) []string {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i]
		value := mapping.Content[i+1]
		if key.Value != "gitfs_remotes" {
			continue
		}
		if value.Kind != yaml.SequenceNode {
			return nil
		}
		var urls []string
		for _, entry := range value.Content {
			if url := gitfsRemoteEntryURL(entry); url != "" {
				urls = append(urls, url)
			}
		}
		return urls
	}
	return nil
}

// gitfsRemoteEntryURL extracts the git URL from one gitfs_remotes sequence entry,
// handling both the plain-scalar form and the single-key map (URL-as-first-key)
// form with per-remote options.
func gitfsRemoteEntryURL(entry *yaml.Node) string {
	switch entry.Kind {
	case yaml.ScalarNode:
		return strings.TrimSpace(entry.Value)
	case yaml.MappingNode:
		if len(entry.Content) >= 1 {
			return strings.TrimSpace(entry.Content[0].Value)
		}
		return ""
	default:
		return ""
	}
}
