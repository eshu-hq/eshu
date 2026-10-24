// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"bytes"
	"strings"
)

// uvSource is one validated `source = { ... }` value of a [[package]] entry.
type uvSource struct {
	ConfigKind string
	SourceKind string
	URL        string
	Ref        string
	Redacted   bool // a userinfo component was removed from URL
}

// uvPrimarySourceKeys are the keys that name the origin of a locked package.
// Exactly one must appear in a source table.
var uvPrimarySourceKeys = map[string]struct{}{
	"registry": {}, "git": {}, "path": {}, "directory": {},
	"editable": {}, "virtual": {}, "url": {},
}

// parseUVSource reads a single-line inline table of basic-string pairs. It
// returns ok=false for anything else: multi-line tables, unknown keys, two
// primary keys, escapes, or oversized values. The caller never guesses a
// source from a table that fails here.
func parseUVSource(value []byte) (uvSource, bool) {
	value = bytes.TrimRight(value, " \t")
	if len(value) < 2 || value[0] != '{' {
		return uvSource{}, false
	}
	rest := bytes.TrimLeft(value[1:], " \t")
	var primaryKey, primaryValue string
	sawSubdirectory := false
	for {
		key, afterKey, ok := splitBareKeyValue(rest)
		if !ok {
			return uvSource{}, false
		}
		content, afterValue, ok := parseBasicString(afterKey, uvLockMaxURL)
		if !ok || len(content) > uvLockMaxURL {
			return uvSource{}, false
		}
		name := string(key)
		if name == "subdirectory" {
			if sawSubdirectory {
				return uvSource{}, false
			}
			sawSubdirectory = true
		} else {
			if _, isPrimary := uvPrimarySourceKeys[name]; !isPrimary || primaryKey != "" {
				return uvSource{}, false
			}
			primaryKey, primaryValue = name, string(content)
		}
		rest = bytes.TrimLeft(afterValue, " \t")
		if len(rest) > 0 && rest[0] == ',' {
			rest = bytes.TrimLeft(rest[1:], " \t")
			continue
		}
		if len(rest) == 0 || rest[0] != '}' {
			return uvSource{}, false
		}
		if !restIsEmptyOrComment(rest[1:]) {
			return uvSource{}, false
		}
		break
	}
	if primaryKey == "" || primaryValue == "" {
		return uvSource{}, false
	}
	return classifyUVSource(primaryKey, primaryValue)
}

// classifyUVSource maps one primary source key to the row's config_kind,
// source_kind, source_url, and source_ref. Only the PyPI index yields
// config_kind=dependency: any other index may serve a package that merely
// shares a PyPI name, so it must not match PyPI advisories.
func classifyUVSource(key, value string) (uvSource, bool) {
	switch key {
	case "registry":
		if strings.ContainsAny(value, " \t") {
			return uvSource{}, false
		}
		url, redacted := stripURLUserinfo(value)
		kind := "url_dependency"
		if isPyPIIndex(url) {
			kind = configKindDependency
		}
		return uvSource{ConfigKind: kind, SourceKind: "registry", URL: url, Redacted: redacted}, true
	case "git":
		if strings.ContainsAny(value, " \t") {
			return uvSource{}, false
		}
		base, ref := splitGitSource(value)
		url, redacted := stripURLUserinfo(base)
		return uvSource{ConfigKind: configKindVCS, SourceKind: "vcs", URL: url, Ref: ref, Redacted: redacted}, true
	case "url":
		if strings.ContainsAny(value, " \t") {
			return uvSource{}, false
		}
		url, redacted := stripURLUserinfo(value)
		return uvSource{ConfigKind: configKindURL, SourceKind: "url", URL: url, Redacted: redacted}, true
	case "editable":
		return uvSource{ConfigKind: configKindEditable, SourceKind: "path", URL: value}, true
	default: // path, directory, virtual
		return uvSource{ConfigKind: configKindPath, SourceKind: "path", URL: value}, true
	}
}

// isPyPIIndex reports whether url is exactly the public PyPI simple index.
// Ports, other hosts, and sub-paths are not PyPI.
func isPyPIIndex(url string) bool {
	return url == "https://pypi.org/simple" || url == "https://pypi.org/simple/"
}

// stripURLUserinfo removes a `user[:password]@` component from the authority
// of a URL so a credential never reaches a persisted row. Values without a
// scheme separator (relative index paths) are returned unchanged.
func stripURLUserinfo(value string) (string, bool) {
	schemeEnd := strings.Index(value, "://")
	if schemeEnd < 0 {
		return value, false
	}
	authorityStart := schemeEnd + 3
	authorityEnd := len(value)
	if index := strings.IndexAny(value[authorityStart:], "/?#"); index >= 0 {
		authorityEnd = authorityStart + index
	}
	at := strings.LastIndexByte(value[authorityStart:authorityEnd], '@')
	if at < 0 {
		return value, false
	}
	return value[:authorityStart] + value[authorityStart+at+1:], true
}

// splitGitSource splits uv's `url?rev=...#commit` form into the clone URL
// and the most specific reference the lockfile proves: the resolved commit
// fragment when it is hex, otherwise a rev/tag/branch query value.
func splitGitSource(value string) (base, ref string) {
	base = value
	fragment := ""
	if index := strings.IndexByte(base, '#'); index >= 0 {
		base, fragment = base[:index], base[index+1:]
	}
	query := ""
	if index := strings.IndexByte(base, '?'); index >= 0 {
		base, query = base[:index], base[index+1:]
	}
	if isHexCommit(fragment) {
		return base, fragment
	}
	for _, pair := range strings.Split(query, "&") {
		name, val, found := strings.Cut(pair, "=")
		if found && (name == "rev" || name == "tag" || name == "branch") && validGitRefName(val) {
			return base, val
		}
	}
	return base, ""
}

func isHexCommit(s string) bool {
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if !isDigit(ch) && (ch < 'a' || ch > 'f') && (ch < 'A' || ch > 'F') {
			return false
		}
	}
	return true
}

func validGitRefName(s string) bool {
	if s == "" || len(s) > 256 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isNameChar(s[i]) && s[i] != '/' {
			return false
		}
	}
	return true
}
