// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"bytes"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// section is the catalog table the scanner is inside.
type section int

const (
	sectionNone        section = iota // before any table header
	sectionVersions                   // [versions]
	sectionLibraries                  // [libraries]
	sectionBundles                    // [bundles]
	sectionPlugins                    // [plugins]
	sectionUnsupported                // any other table
	sectionCount
)

// versionDef is one [versions] alias.
type versionDef struct {
	text string
	rich bool
}

// entry is a library or plugin declaration with its place in the file.
type entry struct {
	decl   declaration
	alias  string
	slot   int // normalized-alias slot in the section's aliasTable
	line   int
	plugin bool
}

// scanner holds the state of one pass over the document.
type scanner struct {
	source   []byte
	current  section
	headers  [sectionCount]bool
	aliases  [sectionCount]*aliasTable
	versions map[int]versionDef // [versions] slot -> first definition
	entries  []entry
	counts   Counts
	offset   int    // end of the span already counted for line numbers
	line     int    // line number at offset
	normBuf  []byte // scratch space for normalizeAlias
}

// Scan applies the catalog grammar to source and returns the rows and counts.
// It is linear in len(source), allocates in proportion to the input, and
// never panics on any byte sequence.
func Scan(source []byte) Result {
	if len(source) > MaxSourceBytes {
		return Result{State: StateOversize, ParseError: "file exceeds the catalog size bound"}
	}
	if nestingExceeds(source, MaxNesting) {
		return Result{State: StateTooDeep, ParseError: "arrays or inline tables nest deeper than the catalog bound"}
	}
	s := &scanner{source: source, versions: map[int]versionDef{}, line: 1}
	var parser unstable.Parser
	parser.Reset(source)
	for parser.NextExpression() {
		if reason := s.expression(parser.Expression()); reason != "" {
			return Result{State: StateMalformed, ParseError: reason}
		}
	}
	if err := parser.Error(); err != nil {
		return Result{State: StateMalformed, ParseError: boundedMessage(err.Error())}
	}
	if err := validTOML(source); err != nil {
		return Result{State: StateMalformed, ParseError: boundedMessage(err.Error())}
	}
	return s.finish()
}

// validTOML runs the TOML decoder's semantic checks, which the streaming
// parser below does not perform: a table defined twice, by a header and by
// dotted keys, or a key repeated outside the catalog tables. Decoding into an
// empty struct keeps no values, so on syntactically valid input the check
// allocates almost nothing. It runs only after the size and nesting guards,
// which bound its work and its recursion, and after the streaming parser
// accepted the syntax, so a syntax error never reaches the decoder (which
// copies an unterminated string before it reports one).
func validTOML(source []byte) error {
	var discard struct{}
	return toml.Unmarshal(source, &discard)
}

// boundedMessage keeps a parser message within MaxFieldBytes.
func boundedMessage(message string) string {
	if len(message) > MaxFieldBytes {
		return message[:MaxFieldBytes]
	}
	return message
}

// lineAt returns the 1-based line of a byte offset. Offsets arrive in
// increasing order, so each byte is counted once.
func (s *scanner) lineAt(offset int) int {
	if offset > s.offset {
		s.line += bytes.Count(s.source[s.offset:offset], []byte{'\n'})
		s.offset = offset
	}
	return s.line
}

// expression handles one top-level table header or key-value pair. A non-empty
// return is a reason the file is malformed.
func (s *scanner) expression(node *unstable.Node) string {
	switch node.Kind {
	case unstable.Table:
		return s.header(node)
	case unstable.ArrayTable:
		s.current = sectionUnsupported
		s.counts.Unsupported++
	case unstable.KeyValue:
		return s.keyValue(node)
	default:
		// Comments never reach here (KeepComments is off); other kinds are
		// value kinds that cannot be a top-level expression.
	}
	return ""
}

// header switches section on a [name] header made of exactly one component.
func (s *scanner) header(node *unstable.Node) string {
	parts, ok := keyParts(node, 2)
	which := sectionUnsupported
	if ok && len(parts) == 1 {
		switch string(parts[0]) {
		case "versions":
			which = sectionVersions
		case "libraries":
			which = sectionLibraries
		case "bundles":
			which = sectionBundles
		case "plugins":
			which = sectionPlugins
		}
	}
	s.current = which
	if which == sectionUnsupported {
		s.counts.Unsupported++
		return ""
	}
	if s.headers[which] {
		return "table header repeated"
	}
	s.headers[which] = true
	return ""
}

// keyValue handles one alias = value pair in the current section.
func (s *scanner) keyValue(node *unstable.Node) string {
	switch s.current {
	case sectionUnsupported:
		return ""
	case sectionNone:
		s.counts.Unsupported++
		return ""
	default:
		// A recognized table: fall through to the alias checks.
	}
	parts, ok := keyParts(node, 2)
	if !ok || len(parts) != 1 {
		s.counts.Unsupported++
		return ""
	}
	alias, ok := boundedString(parts[0])
	if !ok || !validAlias(alias) {
		s.counts.Unsupported++
		return ""
	}
	table := s.aliases[s.current]
	if table == nil {
		table = newAliasTable()
		s.aliases[s.current] = table
	}
	if table.repeated(alias) {
		return "alias repeated in one table"
	}
	if table.full() {
		s.counts.Dropped++
		table.addDropped(alias, &s.normBuf)
		return ""
	}
	slot := table.add(alias, &s.normBuf)

	line := s.lineAt(int(node.Raw.Offset))
	value := node.Value()
	switch s.current {
	case sectionVersions:
		s.version(slot, value)
	case sectionLibraries:
		s.declare(alias, slot, line, value, false)
	case sectionPlugins:
		s.declare(alias, slot, line, value, true)
	case sectionBundles:
		if value.Kind == unstable.Array {
			s.counts.Bundles++
		} else {
			s.counts.Unsupported++
		}
	default:
		// sectionNone and sectionUnsupported returned above.
	}
	return ""
}

// version records a [versions] alias: a string is a plain version, an inline
// table is a rich one, anything else is unsupported and stays undefined. A
// slot keeps its first definition; a shared slot never resolves (see
// aliasTable), so which definition it keeps does not reach a row.
func (s *scanner) version(slot int, value *unstable.Node) {
	var def versionDef
	switch value.Kind {
	case unstable.String:
		text, ok := boundedString(value.Data)
		if !ok {
			s.counts.Unsupported++
			return
		}
		def = versionDef{text: text}
	case unstable.InlineTable:
		def = versionDef{rich: true}
	default:
		s.counts.Unsupported++
		return
	}
	if _, defined := s.versions[slot]; !defined {
		s.versions[slot] = def
	}
	s.counts.Versions++
}

// declare records a library or plugin entry.
func (s *scanner) declare(alias string, slot, line int, value *unstable.Node, plugin bool) {
	read := readLibrary
	if plugin {
		read = readPlugin
	}
	decl, ok := read(value)
	if !ok {
		s.counts.Unsupported++
		return
	}
	if plugin {
		s.counts.Plugins++
	} else {
		s.counts.Libraries++
	}
	s.entries = append(s.entries, entry{decl: decl, alias: alias, slot: slot, line: line, plugin: plugin})
}
