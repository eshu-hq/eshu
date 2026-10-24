// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"bytes"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// Bounds for the uv.lock reader. Every limit is a count or a byte length, so
// the work stays linear in the input and a hostile file cannot grow memory
// past the row cap.
const (
	// uvLockMaxRows caps the dependency rows one uv.lock may produce. Entries
	// past the cap are counted in uv_lock_state.dropped_over_cap, never
	// silently dropped.
	uvLockMaxRows = 10_000
	// uvLockMaxName bounds a package name.
	uvLockMaxName = 214
	// uvLockMaxVersion bounds a locked version string.
	uvLockMaxVersion = 128
	// uvLockMaxURL bounds a source URL or path.
	uvLockMaxURL = 2048
)

// uvLockState is the parser's account of what it could and could not read.
// Resolved + Unresolved + Overflow + RootPackages + WorkspaceMembers always
// equals Entries.
type uvLockState struct {
	Entries          int  // [[package]] headers seen (plus unclassifiable headers)
	Resolved         int  // entries emitted as a well-formed row
	Unresolved       int  // entries emitted as a malformed row or not emitted
	Overflow         int  // entries past the row cap, counted and not read
	RootPackages     int  // the project itself (editable or virtual "."), counted and not emitted
	WorkspaceMembers int  // [manifest] members with an editable or virtual source, counted and not emitted
	RedactedURLs     int  // source URLs that had a userinfo component removed
	Unsupported      bool // lock format version is not 1, a multi-line string, or a root package key was seen
}

// State names the overall outcome: "complete" when every entry resolved,
// otherwise "partial".
func (s uvLockState) State() string {
	if s.Unsupported || s.Unresolved > 0 || s.Overflow > 0 {
		return "partial"
	}
	return "complete"
}

// uvLockResult is what parseUVLockBytes returns.
type uvLockResult struct {
	Rows  []map[string]any
	State uvLockState
	// Scanned counts the bytes the parser's own loops examined, excluding the
	// memchr/memmem primitives. Tests assert it stays within a small multiple
	// of the input size, which catches a quadratic rescan without a stopwatch.
	Scanned int
}

// ParseUVLock reads one uv.lock and emits one dependency row per [[package]]
// entry that matches the version-1 grammar documented in README.md. Anything
// outside that grammar yields a malformed_dependency row (or no row when the
// package name itself is unreadable) and is counted in uv_lock_state; the
// reader never guesses a name, version, or source.
func ParseUVLock(path string) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	result := parseUVLockBytes(source)
	payload := basePayload(path, LangTOML)
	payload["variables"] = result.Rows
	payload["uv_lock_state"] = map[string]any{
		"state":             result.State.State(),
		"entries":           result.State.Entries,
		"resolved":          result.State.Resolved,
		"unresolved":        result.State.Unresolved,
		"dropped_over_cap":  result.State.Overflow,
		"root_packages":     result.State.RootPackages,
		"workspace_members": result.State.WorkspaceMembers,
		"redacted_urls":     result.State.RedactedURLs,
		"unsupported":       result.State.Unsupported,
	}
	return payload, nil
}

type uvScope int

const (
	uvScopeRoot     uvScope = iota // before the first table header
	uvScopeEntry                   // top-level keys of one [[package]]
	uvScopeManifest                // top-level keys of [manifest]
	uvScopeOther                   // a sub-table or [options]
)

// uvEntry accumulates the keys of one [[package]] until the next header.
type uvEntry struct {
	line       int
	overflow   bool // past the row cap: counted, keys not read
	bad        bool // an unrecognised construct appeared in the entry's scope
	name       string
	haveName   bool
	nameBad    bool
	version    string
	haveVer    bool
	versionBad bool
	source     uvSource
	haveSource bool
	sourceBad  bool
}

type uvParser struct {
	source      []byte
	result      uvLockResult
	scope       uvScope
	cur         *uvEntry
	scratch     uvEntry // backing store for cur, so entries past the cap allocate nothing
	depth       int     // open brackets of a multi-line value being skipped
	lockVersion int     // 1 when the root version key is exactly 1; -1 when invalid
	seenVersion bool
	workspace   uvWorkspace
}

// parseUVLockBytes is the pure, testable core of ParseUVLock.
func parseUVLockBytes(source []byte) uvLockResult {
	p := &uvParser{source: source}
	p.result.State.Unsupported = bytes.Contains(source, []byte(`"""`)) || bytes.Contains(source, []byte(`'''`))
	line := 0
	for pos := 0; pos < len(source); {
		var text []byte
		text, pos = splitLine(source, pos)
		line++
		p.handleLine(text, line)
	}
	p.finishEntry()
	return p.result
}

func (p *uvParser) handleLine(text []byte, lineNumber int) {
	trimmed := bytes.TrimSpace(text)
	if p.depth > 0 {
		if string(trimmed) == "[[package]]" {
			// A valid array value cannot contain this line, so the open value
			// was never closed. Mark its entry and resume at the header.
			if p.cur != nil {
				p.cur.bad = true
			}
			p.workspace.abandonMembers()
			p.depth = 0
		} else {
			if p.workspace.collecting {
				p.result.Scanned += p.workspace.collect(trimmed)
			}
			p.skip(trimmed)
			p.workspace.endIfClosed(p.depth)
			return
		}
	}
	if len(trimmed) == 0 || trimmed[0] == '#' {
		return
	}
	if trimmed[0] == '[' {
		p.handleHeader(trimmed, lineNumber)
		return
	}
	key, value, ok := splitBareKeyValue(trimmed)
	if !ok {
		switch {
		case p.scope == uvScopeEntry && p.cur != nil:
			p.cur.bad = true
		case p.scope == uvScopeRoot:
			// A quoted or dotted root key can define packages ("package" =
			// [...], package.name = ...) in a form this reader does not read.
			p.result.State.Unsupported = true
		}
		return
	}
	switch {
	case p.scope == uvScopeRoot:
		p.handleRootKey(string(key), value)
	case p.scope == uvScopeEntry && p.cur != nil && !p.cur.overflow:
		p.handleEntryKey(string(key), value)
	case p.scope == uvScopeManifest && string(key) == "members":
		p.startMembers(value)
	default:
		p.skip(value)
	}
}

// skip walks a value that the reader does not interpret, keeping depth.
func (p *uvParser) skip(value []byte) {
	scan := scanValueBytes(value, p.depth)
	p.result.Scanned += scan.scanned
	p.depth = scan.depth
	if scan.badString && p.cur != nil && p.scope == uvScopeEntry {
		p.cur.bad = true
	}
}

func (p *uvParser) handleRootKey(key string, value []byte) {
	if key == "version" {
		if p.seenVersion {
			p.lockVersion = -1
		} else {
			p.seenVersion = true
			p.lockVersion = -1
			if len(value) > 0 && value[0] == '1' && restIsEmptyOrComment(value[1:]) {
				p.lockVersion = 1
			}
		}
		p.result.Scanned += len(value)
		return
	}
	if key == "package" {
		// A root `package = [...]` array holds entries the reader does not
		// read; the file is unsupported rather than silently empty.
		p.result.State.Unsupported = true
	}
	p.skip(value)
}

func (p *uvParser) handleHeader(trimmed []byte, lineNumber int) {
	p.finishEntry()
	content, rest, array, ok := splitHeader(trimmed)
	p.result.Scanned += len(trimmed)
	switch {
	case !ok || !bareDottedKey(content) || (!array && string(content) == "package"):
		// A header the reader cannot classify (mismatched brackets, a quoted
		// key such as [["package"]], or a [package] table) may hide an entry.
		// Count it as an unresolved unit so the loss is visible.
		p.result.State.Entries++
		p.result.State.Unresolved++
		p.scope = uvScopeOther
	case array && string(content) == "package" && restIsEmptyOrComment(rest):
		p.startEntry(lineNumber)
	case !array && string(content) == "manifest":
		p.scope = uvScopeManifest
		p.workspace.openManifest(p.result.State.Entries > 0)
	default:
		p.scope = uvScopeOther
	}
}

// bareDottedKey reports whether a header's content is one or more bare keys
// joined by dots, the only header form uv writes.
func bareDottedKey(content []byte) bool {
	if len(content) == 0 {
		return false
	}
	start := 0
	for i := 0; i <= len(content); i++ {
		if i == len(content) || content[i] == '.' {
			if i == start {
				return false
			}
			start = i + 1
			continue
		}
		if !isBareKeyByte(content[i]) {
			return false
		}
	}
	return true
}

// splitHeader parses `[a.b]` or `[[a.b]]`, returning the bracketed text, what
// follows, and whether it was an array header. ok=false when the closing
// brackets do not match the opening ones.
func splitHeader(trimmed []byte) (content, rest []byte, array, ok bool) {
	if bytes.HasPrefix(trimmed, []byte("[[")) {
		end := bytes.Index(trimmed, []byte("]]"))
		if end < 0 {
			return nil, nil, true, false
		}
		content = bytes.TrimSpace(trimmed[2:end])
		rest = trimmed[end+2:]
		if bytes.IndexByte(content, '[') >= 0 || bytes.IndexByte(content, ']') >= 0 || !restIsEmptyOrComment(rest) {
			return nil, nil, true, false
		}
		return content, rest, true, true
	}
	end := bytes.IndexByte(trimmed, ']')
	if end < 0 {
		return nil, nil, false, false
	}
	content = bytes.TrimSpace(trimmed[1:end])
	rest = trimmed[end+1:]
	if bytes.IndexByte(content, '[') >= 0 || !restIsEmptyOrComment(rest) {
		return nil, nil, false, false
	}
	return content, rest, false, true
}

func (p *uvParser) startEntry(lineNumber int) {
	p.result.State.Entries++
	p.scope = uvScopeEntry
	p.cur = nil
	if p.result.State.Unsupported {
		p.result.State.Unresolved++
		return
	}
	if p.lockVersion != 1 {
		p.result.State.Unsupported = true
		p.result.State.Unresolved++
		return
	}
	p.scratch = uvEntry{line: lineNumber}
	p.cur = &p.scratch
	if len(p.result.Rows) >= uvLockMaxRows {
		p.cur.overflow = true
	}
}

func (p *uvParser) handleEntryKey(key string, value []byte) {
	switch key {
	case "name", "version":
		scan := scanValueBytes(value, 0)
		p.result.Scanned += scan.scanned
		p.depth = scan.depth
		single := scan.depth == 0 && !scan.badString
		max := uvLockMaxName
		if key == "version" {
			max = uvLockMaxVersion
		}
		content, rest, ok := parseBasicString(value, max)
		valid := single && ok && restIsEmptyOrComment(rest)
		p.result.Scanned += min(len(value), max+2)
		if key == "name" {
			valid = valid && validUVName(content)
			if p.cur.haveName || !valid {
				p.cur.nameBad = true
			} else {
				p.cur.haveName, p.cur.name = true, string(content)
			}
			return
		}
		valid = valid && validUVVersion(content)
		if p.cur.haveVer || !valid {
			p.cur.versionBad = true
		} else {
			p.cur.haveVer, p.cur.version = true, string(content)
		}
	case "source":
		scan := scanValueBytes(value, 0)
		p.result.Scanned += scan.scanned
		p.depth = scan.depth
		if p.cur.haveSource || p.cur.sourceBad {
			p.cur.sourceBad = true
			return
		}
		src, ok := parseUVSource(value)
		p.result.Scanned += min(len(value), 2*uvLockMaxURL)
		if !ok || scan.depth != 0 || scan.badString {
			p.cur.sourceBad = true
			return
		}
		p.cur.haveSource, p.cur.source = true, src
	default:
		p.skip(value)
	}
}

// finishEntry turns the open entry into a row, a malformed row, or a counted
// absence, and clears the entry. An open value at that point (end of file or
// a new header) means the entry's last array was never closed.
func (p *uvParser) finishEntry() {
	entry := p.cur
	p.cur = nil
	if entry == nil {
		p.depth = 0
		return
	}
	if p.depth > 0 {
		entry.bad = true
		p.depth = 0
	}
	state := &p.result.State
	if entry.overflow {
		state.Overflow++
		return
	}
	if !entry.haveName || entry.nameBad {
		state.Unresolved++
		return
	}
	builder := &rowBuilder{
		Name:           entry.name,
		LineNumber:     entry.line,
		Section:        "package",
		PackageManager: PackageManager,
		Lang:           LangTOML,
		Lockfile:       true,
	}
	registry := entry.haveSource && entry.source.SourceKind == "registry"
	wellFormed := !entry.bad && !entry.versionBad && !entry.sourceBad && entry.haveSource &&
		(entry.haveVer || !registry)
	if !wellFormed {
		builder.ConfigKind = configKindMalformed
		builder.Malformed = true
		state.Unresolved++
		p.result.Rows = append(p.result.Rows, builder.finish())
		return
	}
	if p.workspace.omit(entry.name, entry.source, state) {
		return
	}
	builder.Value = entry.version
	builder.ConfigKind = entry.source.ConfigKind
	builder.SourceKind = entry.source.SourceKind
	builder.SourceURL = entry.source.URL
	builder.SourceRef = entry.source.Ref
	if entry.source.Redacted {
		state.RedactedURLs++
	}
	state.Resolved++
	p.result.Rows = append(p.result.Rows, builder.finish())
}
