// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"path/filepath"
	"strings"
)

// requirements_userinfo.go holds the requirements row builders that handle
// non-registry provenance (direct references, VCS, URL, path) and the
// userinfo scrub that keeps credentials out of requirements facts (#9074).
// The parse driver (ParseRequirements, filename scope, name/specifier
// splitting) stays in requirements.go.

func splitDirectReference(content string) (string, []string, string, string, bool) {
	left, right, ok := strings.Cut(content, "@")
	if !ok {
		return "", nil, "", "", false
	}
	name, extras, value, valid := splitNameSpecifier(strings.TrimSpace(left))
	if !valid || name == "" || value != "" {
		return "", nil, "", "", false
	}
	source := strings.TrimSpace(right)
	if source == "" {
		return "", nil, "", "", false
	}
	return name, extras, source, strings.TrimSpace(left), true
}

func populateDirectReferenceRow(
	builder *rowBuilder,
	name string,
	extras []string,
	source string,
	left string,
	editable bool,
) {
	builder.Name = name
	builder.Extras = extras
	switch {
	case isVCSRequirement(source):
		populateVCSRow(builder, source, editable)
	case isURLRequirement(source):
		populateURLRow(builder, source, editable)
	case looksLikePathRequirement(source, editable):
		populatePathRow(builder, source, editable)
	default:
		if strings.Contains(source, "@") || strings.Contains(source, "://") {
			// A direct-reference target with "@" or "://" that is not a
			// URL, VCS, or path line cannot be cleaned reliably (a
			// password may hold "/" or "@"), so the row keeps its kind
			// but loses its text, mirroring the constraints policy.
			builder.Value = ""
			builder.Raw = ""
			builder.SourceCacheWithheld = true
			builder.ConfigKind = configKindMalformed
			builder.Malformed = true
			return
		}
		builder.Value = source
		builder.ConfigKind = configKindMalformed
		builder.Malformed = true
		return
	}
	if builder.Malformed {
		// A demoted row (unsafe marker, unplaceable target) drops its
		// left-hand fields with everything else; only a row that kept
		// its kind restores the reliably parsed left-hand name and
		// extras. The direct-reference cut is at the first "@", so
		// left-hand extras can never carry URL text — but a demoted row
		// keeps nothing parsable at all (#9074).
		builder.Name = ""
		builder.Extras = nil
	} else {
		builder.Name = name
		builder.Extras = extras
	}
	if (builder.SourceKind == "url" || builder.SourceKind == "vcs") && builder.Value != "" {
		// The row keeps the trimmed parsed line as raw, with the source
		// URL scrubbed: the scanner's raw line can carry a trailing
		// comment or original spacing the row scrubbed.
		raw := left + " @ " + builder.Value
		if builder.Marker != "" {
			raw += " ; " + builder.Marker
		}
		builder.Raw = raw
	}
}

func isVCSRequirement(content string) bool {
	lower := strings.ToLower(content)
	for _, prefix := range []string{"git+", "hg+", "svn+", "bzr+"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func isURLRequirement(content string) bool {
	lower := strings.ToLower(content)
	switch {
	case strings.HasPrefix(lower, "http://"),
		strings.HasPrefix(lower, "https://"),
		strings.HasPrefix(lower, "ftp://"),
		strings.HasPrefix(lower, "file://"):
		return true
	}
	return false
}

func looksLikePathRequirement(content string, editable bool) bool {
	if editable {
		return true
	}
	if strings.HasPrefix(content, "./") || strings.HasPrefix(content, "../") {
		return true
	}
	if strings.HasPrefix(content, "/") {
		return true
	}
	return false
}

func populateVCSRow(builder *rowBuilder, content string, editable bool) {
	cleaned, redacted, ok := scrubRequirementURL(content)
	if ok && vcsScrubResidue(cleaned) {
		// A "@" survives outside the ref position: a password with a raw
		// "/" split the authority at the first "/", so the authority cut
		// found no "@" while "user@host" residue sits in the path. The
		// residue cannot be told from a host, so the line is refused
		// instead of cleaned (#9074).
		cleaned, redacted, ok = "", false, false
	}
	if !ok {
		// The line cannot be cleaned reliably (a password may hold "/"
		// or "@", or the host parse is ambiguous), so the row keeps its
		// kind but loses its text, mirroring the constraints policy.
		// The name is dropped too: it is read from the URL fragment,
		// which the same line may poison. A direct reference restores
		// the left-hand name after this returns.
		builder.SourceKind = "vcs"
		builder.Name = ""
		builder.Value = ""
		builder.Raw = ""
		builder.SourceCacheWithheld = true
	} else {
		builder.SourceKind = "vcs"
		builder.SourceURL = stripFragment(cleaned)
		builder.Name = eggNameFromURL(cleaned)
		builder.Value = cleaned
		// The row keeps the trimmed parsed line, never the scanner's raw
		// line: a trailing inline comment can carry URL text the row
		// scrubbed, and the content shaper would otherwise slice the
		// same line into the entity source_cache.
		builder.Raw = cleaned
		if redacted {
			builder.SourceCacheWithheld = true
		}
	}
	if editable {
		builder.ConfigKind = configKindEditable
	} else {
		builder.ConfigKind = configKindVCS
	}
	if !safeMarker(builder.Marker) {
		demoteRequirementsRow(builder)
	}
}

func populateURLRow(builder *rowBuilder, content string, editable bool) {
	cleaned, redacted, ok := scrubRequirementURL(content)
	if ok && strings.Contains(cleaned, "@") {
		// A plain URL has no ref syntax: any "@" left after the
		// authority cut is a password with a raw "/" or an "@" in the
		// path, which cannot be told from userinfo, so the row is
		// refused instead of cleaned. (VCS lines keep a post-slash
		// "@" because that is ref syntax: @v1, @feature/foo.)
		cleaned, redacted, ok = "", false, false
	}
	if !ok {
		// The line cannot be cleaned reliably (a password may hold "/"
		// or "@", or the host parse is ambiguous), so the row keeps its
		// kind but loses its text, mirroring the constraints policy.
		// The name is dropped too: it is read from the URL fragment,
		// which the same line may poison. A direct reference restores
		// the left-hand name after this returns.
		builder.SourceKind = "url"
		builder.Name = ""
		builder.Value = ""
		builder.Raw = ""
		builder.SourceCacheWithheld = true
	} else {
		builder.SourceKind = "url"
		builder.SourceURL = stripFragment(cleaned)
		builder.Name = eggNameFromURL(cleaned)
		builder.Value = cleaned
		// The row keeps the trimmed parsed line, never the scanner's raw
		// line: a trailing inline comment can carry URL text the row
		// scrubbed, and the content shaper would otherwise slice the
		// same line into the entity source_cache.
		builder.Raw = cleaned
		if redacted {
			builder.SourceCacheWithheld = true
		}
	}
	if editable {
		builder.ConfigKind = configKindEditable
	} else {
		builder.ConfigKind = configKindURL
	}
	if !safeMarker(builder.Marker) {
		demoteRequirementsRow(builder)
	}
}

// demoteRequirementsRow turns a row with an unsafe marker into a malformed
// row with no name, value, URL, marker, extras, or raw line. Dropping only
// the marker would leave two lines that differ only in their markers with
// one content identity, and the content writer would keep one of them; see
// applyConstraintPolicy for the same rule on constraints files. Extras go
// too: a PEP 508 extra can never legitimately hold "@" or "://", so extras
// on a demoted row are untrusted text (#9074).
func demoteRequirementsRow(builder *rowBuilder) {
	builder.Name = ""
	builder.Value = ""
	builder.SourceURL = ""
	builder.SourceRef = ""
	builder.Marker = ""
	builder.Extras = nil
	builder.Raw = ""
	builder.SourceCacheWithheld = true
	builder.ConfigKind = configKindMalformed
	builder.Malformed = true
}

// vcsScrubResidue reports whether a scrubbed VCS line still holds an "@"
// that is not a lone ref separator. The authority cut already removed every
// "@" from the authority, and the fragment is stripped above, so at most one
// "@" may remain, and only as the ref separator ("@v1", "@feature/foo").
// Anything else is userinfo residue: a password with a raw "/" split the
// authority at the first "/", so the cut found no "@" while "user@host"
// sits in the path. Either residue refuses the line.
//
// Two vanishing corners stay accepted and are documented, not fixed: a
// password whose pre-slash segment is all digits ("user:22/xx@host", no ref)
// parses as a numeric port under every URL standard, and a colon-less user
// ("user/xx@host") parses as a bare hostname — no parser treats either line
// as credentialed, so the residue is cosmetic. Both need a raw "/" password,
// which is invalid URL syntax to begin with.
func vcsScrubResidue(cleaned string) bool {
	noFrag := stripFragment(cleaned)
	at := strings.LastIndexByte(noFrag, '@')
	if at < 0 {
		return false
	}
	if strings.Contains(noFrag[:at], "@") {
		return true
	}
	return vcsAuthorityHasPasswordShape(noFrag)
}

// vcsAuthorityHasPasswordShape reports whether the authority of a scrubbed
// VCS line (recomputed the way scrubRequirementURL computes it) holds a
// "user:password" shape: a colon whose port side is not all digits. A
// numeric port is the only legitimate colon in an authority; anything else
// with an "@" later in the line is a raw-"/" password that split the
// authority. Bracketed IPv6 literals are skipped so their colons never
// count.
func vcsAuthorityHasPasswordShape(cleaned string) bool {
	work := cleaned
	if isVCSRequirement(cleaned) {
		if index := strings.Index(work, "+"); index >= 0 {
			work = work[index+1:]
		}
	}
	if urlSchemeLen(work) == 0 {
		return false
	}
	// Skip past "://" the way scrubRequirementURL does: the first "/"
	// belongs to the scheme, not the authority.
	rest := work
	if index := strings.Index(rest, "://"); index >= 0 {
		rest = rest[index+3:]
	}
	authority := rest
	if index := strings.IndexByte(rest, '/'); index >= 0 {
		authority = rest[:index]
	}
	host := authority
	if strings.HasPrefix(host, "[") {
		index := strings.Index(host, "]")
		if index < 0 {
			return true
		}
		host = host[index+1:]
		if host == "" {
			return false
		}
		if !strings.HasPrefix(host, ":") {
			return true
		}
		host = host[1:]
	}
	_, port, hasColon := strings.Cut(host, ":")
	if !hasColon {
		return false
	}
	if port == "" {
		return true
	}
	for i := 0; i < len(port); i++ {
		if port[i] < '0' || port[i] > '9' {
			return true
		}
	}
	return false
}

// scrubRequirementURL removes a `user[:password]@` component from the
// authority of a requirements URL or VCS line, keeping the host, path, VCS
// ref (`@v1`, `@feature/foo`), and fragment. It reports redacted=true when it
// removed userinfo and ok=false when the line cannot be cleaned reliably —
// callers then drop the text fields fail-closed.
//
// The cut follows the URL standard: the authority ends at the first "/"
// after the scheme, so a "@" in a VCS ref or fragment is never userinfo and
// survives. Two shapes are refused instead of cleaned: a "?" or "#" before
// the first "/" with an "@" after it (a standards parser and this reader
// would disagree about the host), and a scheme-less line holding "@"
// (scp-style "user@host:path", where userinfo cannot be told from a host).
// A password with a raw "/" cannot be told from a path either — the
// authority cut finds no "@" while userinfo residue sits past the first
// "/" — so callers refuse a VCS line whose scrubbed text still holds "@"
// outside the ref position (see vcsScrubResidue). Such a password is invalid
// URL syntax (it must be percent-encoded, which scrubs cleanly), so the
// residual risk is malformed input only.
func scrubRequirementURL(content string) (clean string, redacted, ok bool) {
	work := content
	prefixLen := 0
	if isVCSRequirement(content) {
		if index := strings.Index(work, "+"); index >= 0 {
			prefixLen = index + 1
		}
	}
	rest := work[prefixLen:]
	if urlSchemeLen(rest) == 0 {
		if strings.Contains(rest, "@") {
			return "", false, false
		}
		return content, false, true
	}
	authorityStart := prefixLen + urlSchemeLen(rest) + 3
	authorityEnd := len(work)
	if index := strings.IndexByte(work[authorityStart:], '/'); index >= 0 {
		authorityEnd = authorityStart + index
	}
	if query := strings.IndexAny(work[authorityStart:authorityEnd], "?#"); query >= 0 &&
		strings.IndexByte(work[authorityStart+query:], '@') >= 0 {
		return "", false, false
	}
	at := strings.LastIndexByte(work[authorityStart:authorityEnd], '@')
	if at < 0 {
		return content, false, true
	}
	return work[:authorityStart] + work[authorityStart+at+1:], true, true
}

func populatePathRow(builder *rowBuilder, content string, editable bool) {
	builder.SourceKind = "path"
	builder.Value = content
	builder.Name = filepath.Base(strings.TrimRight(content, "/"))
	if editable {
		builder.ConfigKind = configKindEditable
	} else {
		builder.ConfigKind = configKindPath
	}
	if builder.Name == "" || builder.Name == "." || builder.Name == ".." {
		builder.Name = content
	}
}

func stripFragment(content string) string {
	if index := strings.Index(content, "#"); index >= 0 {
		return content[:index]
	}
	return content
}

// eggNameFromURL reads the `egg` key of a URL fragment. The fragment starts at
// the first "#" after the authority: a "#" inside userinfo is not a fragment,
// and text there must never become a package name.
func eggNameFromURL(content string) string {
	rest, start := content, 0
	if scheme := strings.Index(content, "://"); scheme >= 0 {
		start = scheme + 3
		if slash := strings.IndexByte(content[start:], '/'); slash >= 0 {
			start += slash
		} else {
			return ""
		}
		rest = content[start:]
	}
	hash := strings.Index(rest, "#")
	if hash < 0 {
		return ""
	}
	fragment := rest[hash+1:]
	for _, part := range strings.Split(fragment, "&") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "egg") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
