// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package npmrc

import (
	"strings"
)

// maxSlotBytes bounds one variable path segment (an org, repo, feed, or id).
const maxSlotBytes = 64

// maxSlotWordBytes bounds one word of a slot. A random token is one long word;
// a human-chosen repository name is built from short ones.
const maxSlotWordBytes = 16

// minMixedWordBytes is the length at which a word that mixes letters and digits
// is treated as a token (hex digests, UUID groups, base64 runs). Shorter mixes
// (k8s, v2, q3) are ordinary names.
const minMixedWordBytes = 8

// pathLayout is one documented registry URL path layout. A "*" segment is a
// variable slot (an org, repo, feed, or id); every other segment is literal.
type pathLayout struct {
	// hostOK restricts the layout to hosts that serve it. Nil means any host
	// except a token-in-path vendor.
	hostOK   func(host string) bool
	segments []string
}

// pathLayouts lists the registry layouts whose vendor documents the credential
// in a config key (_authToken, _auth) and not in the URL path. A path that
// matches none of them is withheld: a token placed as a bare path segment has
// no key and no known value shape, so no heuristic can see it reliably.
// Documentation checked for each layout:
//   - Artifactory: /artifactory/api/npm/<repo>/ (JFrog npm registry docs)
//   - Nexus Repository 3: /repository/<repo>/, with an optional /nexus context
//   - Azure Artifacts: /<org>[/<project>]/_packaging/<feed>/npm/registry/ and
//     the visualstudio.com form without the org segment
//   - GitLab: /api/v4/projects/<id>/packages/npm/, /api/v4/groups/<id>/-/packages/npm/,
//     and the instance form /api/v4/packages/npm/
//   - Gitea and Forgejo: /api/packages/<owner>/npm/
//   - Cloudsmith: npm.cloudsmith.io/<org>/<repo>/
//   - Google Artifact Registry: <region>-npm.pkg.dev/<project>/<repo>/
//   - AWS CodeArtifact: <domain>-<account>.d.codeartifact.<region>.amazonaws.com/npm/<repo>/
//
// The root path (npmjs, GitHub Packages, Verdaccio) always passes.
var pathLayouts = []pathLayout{
	{segments: []string{"artifactory", "api", "npm", "*"}},
	{segments: []string{"repository", "*"}},
	{segments: []string{"nexus", "repository", "*"}},
	{segments: []string{"*", "_packaging", "*", "npm", "registry"}},
	{segments: []string{"*", "*", "_packaging", "*", "npm", "registry"}},
	{hostOK: hasHostSuffix(".pkgs.visualstudio.com"), segments: []string{"_packaging", "*", "npm", "registry"}},
	{segments: []string{"api", "v4", "projects", "*", "packages", "npm"}},
	{segments: []string{"api", "v4", "groups", "*", "-", "packages", "npm"}},
	{segments: []string{"api", "v4", "packages", "npm"}},
	{segments: []string{"api", "packages", "*", "npm"}},
	{hostOK: func(host string) bool { return host == "npm.cloudsmith.io" }, segments: []string{"*", "*"}},
	{hostOK: hasHostSuffix("-npm.pkg.dev"), segments: []string{"*", "*"}},
	{hostOK: isCodeArtifactHost, segments: []string{"npm", "*"}},
}

func hasHostSuffix(suffix string) func(string) bool {
	return func(host string) bool { return strings.HasSuffix(host, suffix) }
}

func isCodeArtifactHost(host string) bool {
	return strings.Contains(host, ".d.codeartifact.") &&
		(strings.HasSuffix(host, ".amazonaws.com") || strings.HasSuffix(host, ".amazonaws.com.cn"))
}

// tokenInPathHost reports a vendor that documents the credential as a path
// segment (Gemfury: https://npm-proxy.fury.io/<token>/<user>/). Its path is
// always withheld.
func tokenInPathHost(host string) bool {
	return host == "fury.io" || strings.HasSuffix(host, ".fury.io")
}

// registryPathAllowed reports whether the escaped URL path of a registry on
// host matches a documented, token-free layout with every variable slot
// shaped like a name. The empty path and "/" always pass. Anything else,
// including every path on a token-in-path vendor, is withheld by the caller,
// which keeps the scheme and host.
func registryPathAllowed(host, escapedPath string) bool {
	if tokenInPathHost(host) {
		return false
	}
	trimmed := strings.TrimPrefix(escapedPath, "/")
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" {
		return escapedPath == "" || escapedPath == "/"
	}
	segments := strings.Split(trimmed, "/")
	for _, layout := range pathLayouts {
		if len(layout.segments) != len(segments) || layout.hostOK != nil && !layout.hostOK(host) {
			continue
		}
		if layoutMatches(layout.segments, segments) {
			return true
		}
	}
	return false
}

func layoutMatches(pattern, segments []string) bool {
	for i, want := range pattern {
		if want == "*" {
			if !slotLooksLikeAName(segments[i]) {
				return false
			}
			continue
		}
		if segments[i] != want {
			return false
		}
	}
	return true
}

// slotLooksLikeAName reports whether a variable path segment reads as a name
// chosen by a person: short words split by '-', '_', '.', or '~', each all
// lower case, all upper case, or Capitalized, with no long letter-and-digit
// mix. A random token (hex, base64url, UUID groups) fails at least one test.
// This is a heuristic that fails closed; it only runs inside a documented
// layout, where no vendor puts a token. A token drawn as a short lower-case
// word, or as digits only, still passes; that is the stated residual.
func slotLooksLikeAName(slot string) bool {
	if slot == "" || len(slot) > maxSlotBytes {
		return false
	}
	words := strings.FieldsFunc(slot, func(r rune) bool { return r == '-' || r == '_' || r == '.' || r == '~' })
	if len(words) == 0 {
		return false
	}
	for i := 0; i < len(slot); i++ {
		c := slot[i]
		if !isNameByte(c) {
			return false
		}
	}
	for _, word := range words {
		if !wordLooksLikeAName(word) {
			return false
		}
	}
	return true
}

func wordLooksLikeAName(word string) bool {
	if len(word) > maxSlotWordBytes {
		return false
	}
	var upper, lower, digit int
	for i := 0; i < len(word); i++ {
		switch c := word[i]; {
		case c >= 'A' && c <= 'Z':
			upper++
		case c >= 'a' && c <= 'z':
			lower++
		default:
			digit++
		}
	}
	if (upper+lower) > 0 && digit > 0 && len(word) >= minMixedWordBytes {
		return false
	}
	if upper > 0 && lower > 0 {
		// Only Capitalized words pass: one leading capital, then lower case.
		return upper == 1 && word[0] >= 'A' && word[0] <= 'Z' && digit == 0
	}
	return true
}

// isNameByte reports a byte a repository, org, or feed name may hold.
func isNameByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	default:
		return c == '-' || c == '_' || c == '.' || c == '~'
	}
}
