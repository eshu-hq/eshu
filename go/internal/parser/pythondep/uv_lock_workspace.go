// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import "strings"

// uvWorkspace tracks the lock's own project and its [manifest] members. Those
// entries are the repository's code, not third-party dependencies, so they are
// counted in uv_lock_state instead of emitted as rows; emitted, they would
// read as unsupported local dependencies in supply-chain readiness. Every
// rule here fails closed: when the members list cannot be read exactly, no
// member is omitted and each stays an ordinary editable or path row.
type uvWorkspace struct {
	members     map[string]struct{} // PEP 503 normalized member names
	bad         bool                // members cannot be trusted; omit none
	seenMembers bool                // a members key was already read
	seenTable   bool                // a [manifest] table was already opened
	collecting  bool                // inside a multi-line members array
	closed      bool                // the members array's closing bracket was read
	afterName   bool                // the last members token was a name, so "," or "]" must follow
	listed      int                 // names listed so far, duplicates included
	rootSeen    bool                // a "." root was counted, or an unread entry may have been it
	scratch     []byte              // reused buffer for PEP 503 normalisation
}

// openManifest records a [manifest] header. uv writes [manifest] before every
// [[package]], so a manifest after an entry, or a second manifest table, is
// not trusted.
func (w *uvWorkspace) openManifest(afterEntries bool) {
	if afterEntries || w.seenTable {
		w.bad = true
	}
	w.seenTable = true
}

// abandonMembers is called when a members array is never closed.
func (w *uvWorkspace) abandonMembers() {
	if w.collecting {
		w.bad = true
		w.collecting = false
	}
}

// startMembers reads the first line of `members = [...]`.
func (p *uvParser) startMembers(value []byte) {
	w := &p.workspace
	duplicate := w.seenMembers
	w.seenMembers = true
	p.skip(value)
	if duplicate || w.bad || len(value) == 0 || value[0] != '[' {
		w.bad = true
		return
	}
	w.collecting = true
	p.result.Scanned += w.collect(value[1:])
	w.endIfClosed(p.depth)
}

// endIfClosed stops collecting once the members array's brackets balance. The
// token reader and the bracket counter must agree that the array closed.
func (w *uvWorkspace) endIfClosed(depth int) {
	if !w.collecting || depth > 0 {
		return
	}
	w.collecting = false
	if !w.closed {
		w.bad = true
	}
}

// collect reads one line of member names: basic strings separated by single
// commas (one trailing comma allowed), ending at "]" and optionally followed
// by a comment. A missing or doubled comma, any other value, or more listed
// names than uvLockMaxMembers marks the list untrusted. Every listed name
// counts against the cap, so a repeated name cannot grow work or memory, and the
// map is written only for a new distinct name. It returns the bytes examined.
func (w *uvWorkspace) collect(line []byte) int {
	scanned := 0
	for i := 0; i < len(line); {
		ch := line[i]
		scanned++
		switch {
		case w.bad || w.closed:
			w.bad = w.bad || !restIsEmptyOrComment(line[i:])
			return scanned
		case ch == ' ' || ch == '\t':
			i++
		case ch == '#':
			return scanned
		case ch == ',' && w.afterName:
			w.afterName = false
			i++
		case ch == ']':
			w.closed = true
			i++
		case ch == '"' && !w.afterName:
			content, rest, ok := parseBasicString(line[i:], uvLockMaxName)
			w.listed++
			if !ok || !validUVName(content) || w.listed > uvLockMaxMembers {
				w.bad = true
				return scanned
			}
			w.afterName = true
			key := w.normalize(content)
			if _, seen := w.members[string(key)]; !seen {
				if w.members == nil {
					w.members = map[string]struct{}{}
				}
				w.members[string(key)] = struct{}{}
			}
			scanned += len(content) + 1
			i = len(line) - len(rest)
		default:
			w.bad = true
			return scanned
		}
	}
	return scanned
}

// omit reports whether a well-formed entry is the project itself or a
// workspace member, counting it in state when it is. Only an editable or
// virtual source qualifies: that is how uv locks first-party code.
func (w *uvWorkspace) omit(name string, src uvSource, state *uvLockState) bool {
	if src.Origin != "editable" && src.Origin != "virtual" {
		return false
	}
	if src.URL == "." {
		// Only one entry can be the project itself; a second "." entry is
		// not a uv form and stays a row.
		if w.rootSeen {
			return false
		}
		w.rootSeen = true
		state.RootPackages++
		return true
	}
	if w.bad || w.collecting || len(w.members) == 0 || !insideWorkspace(src.URL) {
		return false
	}
	if _, ok := w.members[string(w.normalize([]byte(name)))]; ok {
		state.WorkspaceMembers++
		return true
	}
	return false
}

// insideWorkspace reports whether a member path stays under the lock's
// directory: relative, with no ".." segment. uv writes member paths that way;
// a path that leaves the workspace is a local dependency and stays a row.
func insideWorkspace(path string) bool {
	if path == "" || path[0] == '/' {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return false
		}
	}
	return true
}

// normalize applies PEP 503 (lowercase, and every run of "-", "_", or "."
// becomes one "-") into the reused scratch buffer. Names reaching it passed
// validUVName, so they are ASCII. The result is valid until the next call.
func (w *uvWorkspace) normalize(name []byte) []byte {
	out := w.scratch[:0]
	run := false
	for _, ch := range name {
		if ch == '-' || ch == '_' || ch == '.' {
			if !run {
				out = append(out, '-')
			}
			run = true
			continue
		}
		run = false
		if ch >= 'A' && ch <= 'Z' {
			ch += 'a' - 'A'
		}
		out = append(out, ch)
	}
	w.scratch = out
	return out
}
