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
	slot   int // accessor-key slot in the section's aliasTable
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
	keyBuf   []byte // scratch space for accessorKey
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
		node := parser.Expression()
		if keysExceed(node, MaxKeySegments) {
			return Result{State: StateTooDeep, ParseError: "a dotted key has more segments than the catalog bound"}
		}
		if reason := s.expression(node); reason != "" {
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
// empty struct keeps no values, but the decoder still builds its own parse
// tree and key paths, so its allocation grows with the input and with the
// dotted segments of each key (about 290 bytes per input byte on one long
// dotted key). It runs only after the size, nesting and key-segment guards,
// which bound its work and its recursion, and after the streaming parser
// accepted the syntax, so a syntax error never reaches the decoder (which
// copies an unterminated string before it reports one).
//
// The target must stay an empty struct. go-toml's decoder recurses once per
// dotted-key segment while it fills a matching field or map, with no bound,
// and nestingExceeds does not count dotted keys: decoding into a map
// overflows the stack on a 900,000-segment key. An empty struct matches no
// key, so the decoder never descends.
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
		return s.header(node, false)
	case unstable.ArrayTable:
		return s.header(node, true)
	case unstable.KeyValue:
		return s.keyValue(node)
	default:
		// Comments never reach here (KeepComments is off); other kinds are
		// value kinds that cannot be a top-level expression.
	}
	return ""
}

// sectionNamed returns the catalog table a header component names, or
// sectionUnsupported.
func sectionNamed(name []byte) section {
	switch string(name) {
	case "versions":
		return sectionVersions
	case "libraries":
		return sectionLibraries
	case "bundles":
		return sectionBundles
	case "plugins":
		return sectionPlugins
	default:
		return sectionUnsupported
	}
}

// header switches section on a [name] header made of exactly one component.
// Any other header is unsupported. A [libraries.alias] sub-table, deeper
// [libraries.alias.version] tables and an [[libraries.alias]] array still
// register alias with Gradle, so alias is shadowed in its table.
func (s *scanner) header(node *unstable.Node, array bool) string {
	first, second, segments := keyHead(node)
	which := sectionNamed(first)
	if segments != 1 || array || which == sectionUnsupported {
		s.current = sectionUnsupported
		s.counts.Unsupported++
		if segments > 1 && which != sectionUnsupported {
			s.shadow(which, second)
		}
		return ""
	}
	s.current = which
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
	first, _, segments := keyHead(node)
	alias, ok := boundedString(first)
	if segments != 1 || !ok || !validAlias(alias) {
		// A dotted entry (alias.module = ...) or an alias over the field
		// bound emits no row, but Gradle registers its alias.
		s.counts.Unsupported++
		s.shadow(s.current, first)
		return ""
	}
	table := s.table(s.current)
	if table.full() {
		if table.addDropped(alias, &s.keyBuf) {
			return "alias repeated in one table"
		}
		s.counts.Dropped++
		return ""
	}
	slot, repeated := table.add(alias, &s.keyBuf)
	if repeated {
		return "alias repeated in one table"
	}

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

// table returns the alias table of which, creating it on first use.
func (s *scanner) table(which section) *aliasTable {
	table := s.aliases[which]
	if table == nil {
		table = newAliasTable()
		s.aliases[which] = table
	}
	return table
}

// shadow registers an alias that emits no row, so it still collides with
// the other spellings of it in its table (see aliasTable.shadow).
func (s *scanner) shadow(which section, alias []byte) {
	if len(alias) == 0 {
		return
	}
	s.table(which).shadow(alias, &s.keyBuf)
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
