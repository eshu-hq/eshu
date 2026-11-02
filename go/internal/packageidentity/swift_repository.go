// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

import (
	"net/url"
	"strings"
)

// SwiftOSVSourceLocation returns the name the vulnerability planner queries for
// a stored swift source_location: the repository URL with userinfo, query and
// fragment removed, the git@host:path form with query and fragment removed, or
// "" when the location is neither an http(s), ssh or git URL nor a git@host:path
// form. The planner and the owned-package resolver both call it, so what names
// a query and what a changed feed name is compared against cannot drift.
func SwiftOSVSourceLocation(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "git@") {
		cleaned := trimmed
		if index := strings.IndexAny(cleaned, "?#"); index >= 0 {
			cleaned = cleaned[:index]
		}
		host, repoPath, ok := strings.Cut(strings.TrimPrefix(cleaned, "git@"), ":")
		if ok && strings.TrimSpace(host) != "" && strings.TrimSpace(repoPath) != "" {
			return cleaned
		}
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" || strings.Trim(parsed.Path, "/") == "" {
		return ""
	}
	if !swiftRepositoryScheme(parsed.Scheme) {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
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
	trimmed := strings.TrimSpace(raw)
	if index := strings.IndexAny(trimmed, "?#"); index >= 0 {
		trimmed = trimmed[:index]
	}
	var host, repoPath string
	switch {
	case strings.HasPrefix(trimmed, "git@"):
		var ok bool
		if host, repoPath, ok = strings.Cut(strings.TrimPrefix(trimmed, "git@"), ":"); !ok {
			return ""
		}
	case strings.Contains(trimmed, "://"):
		parsed, err := url.Parse(trimmed)
		if err != nil || !swiftRepositoryScheme(parsed.Scheme) {
			return ""
		}
		host, repoPath = parsed.Hostname(), parsed.Path
	default:
		var ok bool
		host, repoPath, ok = strings.Cut(trimmed, "/")
		if !ok || !strings.Contains(host, ".") || strings.ContainsAny(host, ":@ \t") {
			return ""
		}
	}
	// Fold before trimming: a stored ".GIT" must lose its suffix like ".git" does,
	// or the key stops matching a lookup that was folded first.
	repoPath = strings.Trim(strings.TrimSuffix(strings.Trim(LowerASCII(repoPath), "/"), ".git"), "/")
	if host == "" || repoPath == "" {
		return ""
	}
	return LowerASCII(host + "/" + repoPath)
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
