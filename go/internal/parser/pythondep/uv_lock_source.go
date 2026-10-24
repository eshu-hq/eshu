// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"bytes"
	"strings"
)

// uvSource is one validated `source = { ... }` value of a [[package]] entry.
type uvSource struct {
	Origin     string // the primary key: registry, git, path, directory, editable, virtual, or url
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
	src, ok := classifyUVSource(primaryKey, primaryValue)
	src.Origin = primaryKey
	return src, ok
}

// classifyUVSource maps one primary source key to the row's config_kind,
// source_kind, source_url, and source_ref. Only the PyPI index yields
// config_kind=dependency: any other index may serve a package that merely
// shares a PyPI name, so it must not match PyPI advisories. Persisted URLs
// never carry userinfo, a query, or a fragment, because any of them can hold
// a credential or a signed token; each removal is counted in Redacted.
func classifyUVSource(key, value string) (uvSource, bool) {
	switch key {
	case "registry", "url":
		if strings.ContainsAny(value, " \t") || !schemeOrPlainPath(key, value) {
			return uvSource{}, false
		}
		url, redacted, ok := stripURLUserinfo(value)
		if !ok {
			return uvSource{}, false
		}
		kind, sourceKind := configKindURL, "url"
		if key == "registry" {
			sourceKind = "registry"
			if isPyPIIndex(url) {
				kind = configKindDependency
			}
		}
		url, dropped := dropURLQueryFragment(url)
		return uvSource{ConfigKind: kind, SourceKind: sourceKind, URL: url, Redacted: redacted || dropped}, true
	case "git":
		if strings.ContainsAny(value, " \t") || urlSchemeLen(value) == 0 {
			// scp-style "user:secret@host:path" has no scheme, so userinfo
			// cannot be told from a host; fail closed.
			return uvSource{}, false
		}
		// Strip userinfo before splitting off the query and fragment so a raw
		// "#" or "?" inside a password cannot leave a credential prefix behind.
		clean, redacted, ok := stripURLUserinfo(value)
		if !ok {
			return uvSource{}, false
		}
		url, ref, dropped := splitGitSource(clean)
		return uvSource{ConfigKind: configKindVCS, SourceKind: "vcs", URL: url, Ref: ref, Redacted: redacted || dropped}, true
	default: // path, directory, editable, virtual
		if strings.ContainsAny(value, "@:") {
			// A local path never carries a scheme or userinfo; a URL or an
			// scp-style "user:secret@host:path" here could hold a credential
			// this reader does not strip. A Windows drive path is refused too.
			return uvSource{}, false
		}
		kind := configKindPath
		if key == "editable" {
			kind = configKindEditable
		}
		return uvSource{ConfigKind: kind, SourceKind: "path", URL: value}, true
	}
}

// schemeOrPlainPath accepts a URL with a scheme, or for a registry a plain
// relative or absolute path. A scheme-less value that holds "@" or ":" could
// carry userinfo this reader cannot separate from a host, so it is refused.
func schemeOrPlainPath(key, value string) bool {
	if urlSchemeLen(value) > 0 {
		return true
	}
	return key == "registry" && !strings.ContainsAny(value, "@:")
}

// dropURLQueryFragment removes a `?query` and `#fragment` from value and
// reports whether anything was removed.
func dropURLQueryFragment(value string) (string, bool) {
	if index := strings.IndexAny(value, "?#"); index >= 0 {
		return value[:index], true
	}
	return value, false
}

// isPyPIIndex reports whether url is exactly the public PyPI simple index.
// Ports, other hosts, and sub-paths are not PyPI.
func isPyPIIndex(url string) bool {
	return url == "https://pypi.org/simple" || url == "https://pypi.org/simple/"
}

// urlSchemeLen returns the length of a leading `scheme://` (letters, digits,
// plus, dot, hyphen; letter first), or 0 when value does not start with one.
// Anchoring at the start matters: a "://" later in the text could leave
// credentials in an unparsed prefix.
func urlSchemeLen(value string) int {
	for i := 0; i < len(value); i++ {
		ch := value[i]
		switch {
		case ch == ':':
			if i > 0 && strings.HasPrefix(value[i:], "://") {
				return i
			}
			return 0
		case (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z'):
		case i > 0 && (isDigit(ch) || ch == '+' || ch == '.' || ch == '-'):
		default:
			return 0
		}
	}
	return 0
}

// stripURLUserinfo removes a `user[:password]@` component from the authority
// of a URL so a credential never reaches a persisted row. The authority is
// cut at the first slash, wider than the URL standard, so a client that reads
// a raw "#" or "?" as part of a password cannot leave a prefix behind. When a
// "?" or "#" comes before the first slash and an "@" follows it, standards
// parsers and this reader would disagree about the host, so the value is
// refused (ok=false) instead of cleaned. Any "@" left after the strip is
// refused too: a raw "/" inside a password ("https://u:a/b@host") moves the
// real userinfo past the cut, so a cleaned URL never contains an "@". Values
// with no scheme are returned unchanged only when they cannot hold userinfo
// (see schemeOrPlainPath).
func stripURLUserinfo(value string) (clean string, redacted, ok bool) {
	schemeLen := urlSchemeLen(value)
	if schemeLen == 0 {
		return value, false, true
	}
	authorityStart := schemeLen + 3
	authorityEnd := len(value)
	if index := strings.IndexByte(value[authorityStart:], '/'); index >= 0 {
		authorityEnd = authorityStart + index
	}
	if q := strings.IndexAny(value[authorityStart:authorityEnd], "?#"); q >= 0 &&
		strings.IndexByte(value[authorityStart+q:], '@') >= 0 {
		return "", false, false
	}
	at := strings.LastIndexByte(value[authorityStart:authorityEnd], '@')
	if at < 0 {
		if strings.IndexByte(value[authorityStart:], '@') >= 0 {
			return "", false, false
		}
		return value, false, true
	}
	if strings.IndexByte(value[authorityStart+at+1:], '@') >= 0 {
		return "", false, false
	}
	return value[:authorityStart] + value[authorityStart+at+1:], true, true
}

// splitGitSource splits uv's `url?rev=...#commit` form into the clone URL
// and the most specific reference the lockfile proves: the resolved commit
// fragment when it is hex, otherwise a rev/tag/branch query value. dropped is
// true when a query key other than rev, tag, branch, or subdirectory, or a
// non-commit fragment, was discarded.
func splitGitSource(value string) (base, ref string, dropped bool) {
	base = value
	fragment := ""
	if index := strings.IndexByte(base, '#'); index >= 0 {
		base, fragment = base[:index], base[index+1:]
	}
	query := ""
	if index := strings.IndexByte(base, '?'); index >= 0 {
		base, query = base[:index], base[index+1:]
	}
	if fragment != "" && !isHexCommit(fragment) {
		dropped = true
	}
	var queryRef string
	for _, pair := range strings.Split(query, "&") {
		name, val, found := strings.Cut(pair, "=")
		switch {
		case !found && pair == "":
		case found && (name == "rev" || name == "tag" || name == "branch") && validGitRefName(val):
			if queryRef == "" {
				queryRef = val
			}
		case found && name == "subdirectory":
		default:
			dropped = true
		}
	}
	if isHexCommit(fragment) {
		return base, fragment, dropped
	}
	return base, queryRef, dropped
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
