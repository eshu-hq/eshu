// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql

import "strings"

type recoveredDropTarget struct {
	name   string
	offset int
}

// recoverDropTableTargetsFromTail recovers valid targets that the SQL grammar
// leaves after drop_table in a sibling ERROR. It accepts only comma-prefixed,
// fully formed identifier lists followed by an optional CASCADE or RESTRICT
// clause and statement terminator; any other tail is ignored.
func recoverDropTableTargetsFromTail(
	start int,
	source []byte,
	add func(name, operation string, offset int),
) {
	if start >= len(source) {
		return
	}
	targets, ok := parseDropTargetTail(string(source[start:]))
	if !ok {
		return
	}
	for _, target := range targets {
		add(target.name, "drop", start+target.offset)
	}
}

// parseDropTargetTail recognizes only the recovery tail immediately after a
// parsed DROP TABLE target. Keeping this tiny recognizer separate from the AST
// walk makes its boundary explicit: it cannot absorb an arbitrary malformed
// expression or a later statement as another migration target.
func parseDropTargetTail(tail string) ([]recoveredDropTarget, bool) {
	index := skipDropTargetSpaceAndComments(tail, 0)
	if index >= len(tail) || tail[index] != ',' {
		return nil, false
	}
	return parseDropTargetList(tail, skipDropTargetSpaceAndComments(tail, index+1))
}

// isCompleteDropTargetListFrom validates the full source remainder beginning
// at a direct tree-sitter ERROR child. The grammar uses that shape for some
// valid comma-separated DROP lists, but it also uses it for malformed input;
// only a complete identifier list with an optional behavior clause and
// terminator may contribute recovered migration targets.
func isCompleteDropTargetListFrom(start int, source []byte) bool {
	if start < 0 || start >= len(source) {
		return false
	}
	_, ok := parseDropTargetList(string(source[start:]), 0)
	return ok
}

// parseDropTargetList reads a complete comma-separated identifier list from
// index. The caller decides whether the list begins at its first identifier or
// immediately after a recovery-leading comma.
func parseDropTargetList(source string, index int) ([]recoveredDropTarget, bool) {
	targets := make([]recoveredDropTarget, 0, 1)
	for {
		index = skipDropTargetSpaceAndComments(source, index)
		name, offset, next, ok := scanDropTargetName(source, index)
		if !ok {
			return nil, false
		}
		targets = append(targets, recoveredDropTarget{name: name, offset: offset})
		index = skipDropTargetSpaceAndComments(source, next)

		switch {
		case index == len(source):
			return targets, true
		case source[index] == ',':
			index++
			continue
		case source[index] == ';':
			return targets, skipDropTargetSpaceAndComments(source, index+1) == len(source)
		case hasDropTargetKeyword(source[index:], "cascade"), hasDropTargetKeyword(source[index:], "restrict"):
			if hasDropTargetKeyword(source[index:], "cascade") {
				index += len("cascade")
			} else {
				index += len("restrict")
			}
			index = skipDropTargetSpaceAndComments(source, index)
			if index == len(source) {
				return targets, true
			}
			return targets, source[index] == ';' && skipDropTargetSpaceAndComments(source, index+1) == len(source)
		default:
			return nil, false
		}
	}
}

// scanDropTargetName reads one schema-qualified identifier and returns its
// normalized name, byte offset, and first byte after the identifier.
func scanDropTargetName(source string, index int) (string, int, int, bool) {
	offset := index
	part, next, ok := scanDropTargetIdentifier(source, index)
	if !ok {
		return "", 0, 0, false
	}
	parts := []string{normalizeSQLName(part)}
	index = next
	for {
		index = skipDropTargetSpaceAndComments(source, index)
		if index >= len(source) || source[index] != '.' {
			break
		}
		index = skipDropTargetSpaceAndComments(source, index+1)
		part, next, ok = scanDropTargetIdentifier(source, index)
		if !ok {
			return "", 0, 0, false
		}
		parts = append(parts, normalizeSQLName(part))
		index = next
	}
	return strings.Join(parts, "."), offset, index, true
}

// scanDropTargetIdentifier reads one unquoted, double-quoted, backtick-quoted,
// or bracket-quoted SQL identifier.
func scanDropTargetIdentifier(source string, index int) (string, int, bool) {
	if index >= len(source) {
		return "", 0, false
	}
	if quote := source[index]; quote == '"' || quote == '`' || quote == '[' {
		closing := quote
		if quote == '[' {
			closing = ']'
		}
		for next := index + 1; next < len(source); next++ {
			if source[next] != closing {
				continue
			}
			if next+1 < len(source) && source[next+1] == closing {
				next++
				continue
			}
			return source[index : next+1], next + 1, true
		}
		return "", 0, false
	}
	if !isDropTargetIdentifierStart(source[index]) {
		return "", 0, false
	}
	next := index + 1
	for next < len(source) && isDropTargetIdentifierPart(source[next]) {
		next++
	}
	return source[index:next], next, true
}

func isDropTargetIdentifierStart(value byte) bool {
	return value == '_' || value >= 0x80 || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func isDropTargetIdentifierPart(value byte) bool {
	return isDropTargetIdentifierStart(value) || value >= '0' && value <= '9' || value == '$'
}

func hasDropTargetKeyword(source, keyword string) bool {
	if len(source) < len(keyword) || !strings.EqualFold(source[:len(keyword)], keyword) {
		return false
	}
	return len(source) == len(keyword) || !isDropTargetIdentifierPart(source[len(keyword)])
}

func skipDropTargetSpaceAndComments(source string, index int) int {
	for index < len(source) {
		switch {
		case source[index] == ' ' || source[index] == '\t' || source[index] == '\n' || source[index] == '\r':
			index++
		case strings.HasPrefix(source[index:], "--"):
			for index < len(source) && source[index] != '\n' {
				index++
			}
		case strings.HasPrefix(source[index:], "/*"):
			end := strings.Index(source[index+2:], "*/")
			if end < 0 {
				return index
			}
			index += end + 4
		default:
			return index
		}
	}
	return index
}
