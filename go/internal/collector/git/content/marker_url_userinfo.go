// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/secret/detect"
)

// urlToken is a value that is exactly one URL with a scheme and no white space.
var urlToken = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://\S+$`)

// redactURLKey applies the userinfo-only rule to the value of a URL-valued
// listed key (#8833, the #8812 binding comment): when the value is exactly one
// URL, only its userinfo becomes REDACTED_<finding_kind>, so the scheme, host,
// port, and path stay readable. Everything else fails closed to the
// credential-key rule, which replaces the whole value: an empty or block-scalar
// value, an unterminated quote, text after the URL, a value that is not a URL,
// an authority the rule cannot read (a raw "/" in a password makes "user:pa"
// look like a host with port "pa"), and a line that still holds a detector
// match after the userinfo is gone. More-indented lines after the key are its
// YAML continuation and are redacted, as for a credential key.
func (s *markerState) redactURLKey(line, keyText string, keyEnd, valueStart int, rawKind string) string {
	whole := func() string { return s.redactListedKey(line, keyText, keyEnd, valueStart, rawKind, false) }
	value := strings.TrimSpace(line[valueStart:])
	if value == "" || blockScalar.MatchString(value) {
		return whole()
	}
	s.continuationOpen = true
	s.continuationIndent = len(leadingWhitespace(line))
	if keepsVerbatim(value) {
		return line
	}
	quote, inner, rest, closed := splitQuotedValue(value)
	if !closed || strings.TrimSpace(rest) != "" || !urlToken.MatchString(inner) {
		return whole()
	}
	// YAML decodes escapes in a double-quoted scalar ("\x40" is "@"), so the
	// raw text is not the URL Yarn reads. Any backslash sends the whole value.
	if strings.IndexByte(inner, '\\') >= 0 {
		return whole()
	}
	kind := rawKind
	if kind == "" {
		kind = defaultMarkerKind
	}
	redacted, changed, ok := redactUserinfo(inner, markerPrefix+kind)
	if !ok {
		return whole()
	}
	if !changed {
		return line
	}
	quoteText := ""
	if quote != 0 {
		quoteText = string(quote)
	}
	out := line[:valueStart] + quoteText + redacted + quoteText
	if len(detect.Spans(out)) > 0 {
		return whole()
	}
	s.replaced++
	return out
}

// redactUserinfo replaces the userinfo of one URL with marker. The authority
// runs from "://" to the first "/", "?", or "#", and the userinfo ends at its
// last "@", so a raw "@" in a password is covered. It reports whether the URL
// changed, and ok=false, so the caller redacts the whole value, when the host
// and port are not a plain name and number or when an "@" follows the
// authority: "http://tok/en@host" may be a token holding a raw "/", and the
// rule cannot tell. A userinfo that is already a marker is kept as it is, which
// keeps the redactor a fixed point.
func redactUserinfo(url, marker string) (string, bool, bool) {
	schemeEnd := strings.Index(url, "://") + len("://")
	authorityEnd := len(url)
	if cut := strings.IndexAny(url[schemeEnd:], "/?#"); cut >= 0 {
		authorityEnd = schemeEnd + cut
	}
	authority := url[schemeEnd:authorityEnd]
	at := strings.LastIndexByte(authority, '@')
	if !validHostPort(authority[at+1:]) || strings.IndexByte(url[authorityEnd:], '@') >= 0 {
		return "", false, false
	}
	if at < 0 || existingMarker.MatchString(authority[:at]) {
		return url, false, true
	}
	return url[:schemeEnd] + marker + authority[at:] + url[authorityEnd:], true, true
}

// validHostPort reports whether text reads as a host with an optional numeric
// port: a bracketed IPv6 literal (hex digits, ":" and ".") or a name of
// letters, digits, ".", "_", and "-", then at most one ":" and digits. A
// percent escape or any other character fails, so an authority the rule cannot
// read sends the whole value to the credential-key rule.
func validHostPort(text string) bool {
	port := ""
	if strings.HasPrefix(text, "[") {
		end := strings.IndexByte(text, ']')
		if end < 2 || strings.Trim(text[1:end], "0123456789abcdefABCDEF:.") != "" {
			return false
		}
		if rest := text[end+1:]; rest != "" {
			if !strings.HasPrefix(rest, ":") {
				return false
			}
			port = rest[1:]
		}
	} else {
		host := text
		if colon := strings.IndexByte(text, ':'); colon >= 0 {
			host, port = text[:colon], text[colon+1:]
		}
		if host == "" || strings.IndexFunc(host, isNotHostRune) >= 0 {
			return false
		}
	}
	return strings.Trim(port, "0123456789") == ""
}

func isNotHostRune(r rune) bool {
	return r != '.' && r != '_' && r != '-' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
}
