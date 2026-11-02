// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

import (
	"net/url"
	"strings"
)

// SwiftOSVSourceLocation returns the name the vulnerability planner sends OSV
// for a stored swift source_location, or "" when the location names no
// repository OSV could index. OSV's SwiftURL names are host/path
// ("github.com/apple/swift-nio") with no scheme, credentials, port, query,
// fragment, trailing slash or ".git", and OSV matches them byte for byte. So
// the host folds to ASCII lower case, as DNS does, and the path keeps its case.
// A location with neither a "://" scheme nor the git@host:path form names no
// query. The name always has a repository key (a dotless host such as localhost has
// none and is not queried). For every name the planner queries from a normal
// location, SwiftRepositoryKey of the name equals the key of the stored
// location. Two odd inputs break that: a path that ends in ".git.git" (the name
// keeps one ".git", which the key then strips again) and a percent-encoded "?"
// or "#" in the path (url.Parse decodes it, so the key cuts the name there).
// The resolver compares the key on both sides, so neither input can make it
// disagree with itself; it only over-matches in the first case.
//
// The planner and the owned-package resolver both call it, and both derive
// from [swiftRepositoryParts], so what names a query and what a changed feed
// name is compared against cannot drift. A "www." prefix is not removed:
// SwiftRepositoryKey does not remove it, so the two stay equal.
func SwiftOSVSourceLocation(raw string) string {
	// The planner queries only a location that carries a clone URL or the
	// git@host:path form. A scheme-less "host/path" is a feed spelling, not
	// something a manifest records, so it stays skipped as before.
	if trimmed := strings.TrimSpace(raw); !strings.HasPrefix(trimmed, "git@") && !strings.Contains(trimmed, "://") {
		return ""
	}
	host, repoPath, ok := swiftRepositoryParts(raw)
	if !ok {
		return ""
	}
	name := LowerASCII(host) + "/" + repoPath
	if SwiftRepositoryKey(name) == "" {
		return ""
	}
	return name
}

// SwiftRepositoryKey returns the one spelling of a swift repository that feed
// names and stored locations compare on: host/path, ASCII-folded, with no
// scheme, userinfo, port, query, fragment, trailing slash or ".git" in any case,
// and with git@host:path mapped to host/path. The OSV SwiftURL feed spells a package
// "github.com/apple/swift-nio" while a manifest spells it as a clone URL, so
// the two only meet on this key.
//
// It returns "" for anything that names no repository: a bare package name
// (the feed has three of those), a path with no host, or a URL with a scheme
// the planner does not query. A spelling with no scheme counts as host/path
// only when its first segment looks like a host name.
func SwiftRepositoryKey(raw string) string {
	host, repoPath, ok := swiftRepositoryParts(raw)
	if !ok {
		return ""
	}
	return LowerASCII(host + "/" + repoPath)
}

// swiftRepositoryParts splits a swift repository spelling into its host and
// repository path, the one parse behind [SwiftRepositoryKey] and
// [SwiftOSVSourceLocation]. The host is as written (the caller folds it) and
// the path keeps its case, with query, fragment, userinfo, port, surrounding
// slashes and a ".git" suffix in any case removed. It reports false when the
// spelling names no host/path repository.
func swiftRepositoryParts(raw string) (host, repoPath string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if index := strings.IndexAny(trimmed, "?#"); index >= 0 {
		trimmed = trimmed[:index]
	}
	switch {
	case strings.HasPrefix(trimmed, "git@"):
		if host, repoPath, ok = strings.Cut(strings.TrimPrefix(trimmed, "git@"), ":"); !ok {
			return "", "", false
		}
	case strings.Contains(trimmed, "://"):
		parsed, err := url.Parse(trimmed)
		if err != nil || !swiftRepositoryScheme(parsed.Scheme) {
			return "", "", false
		}
		host, repoPath = parsed.Hostname(), parsed.Path
	default:
		if host, repoPath, ok = strings.Cut(trimmed, "/"); !ok || !strings.Contains(host, ".") || strings.ContainsAny(host, ":@ \t") {
			return "", "", false
		}
	}
	// ".git" is matched in any case, as the key always did: a stored ".GIT"
	// must lose its suffix like ".git" does.
	repoPath = strings.Trim(repoPath, "/")
	if strings.HasSuffix(LowerASCII(repoPath), ".git") {
		repoPath = repoPath[:len(repoPath)-len(".git")]
	}
	repoPath = strings.Trim(repoPath, "/")
	if strings.TrimSpace(host) == "" || repoPath == "" || strings.ContainsAny(host, " \t") {
		return "", "", false
	}
	return host, repoPath, true
}

// swiftRepositoryScheme reports whether a URL scheme, which url.Parse has
// already lowercased, can name a swift repository the planner queries.
func swiftRepositoryScheme(scheme string) bool {
	switch scheme {
	case "https", "http", "ssh", "git":
		return true
	default:
		return false
	}
}
