// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"bytes"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
)

// versionKind says how an entry names its version.
type versionKind int

const (
	versionNone    versionKind = iota // no version declared
	versionLiteral                    // version = "1.2.3" or the third part of "g:a:1.2.3"
	versionRef                        // version.ref = "alias"
	versionRich                       // a version table other than { ref = ... }
)

// versionSpec is the version part of a library or plugin entry as written.
type versionSpec struct {
	kind versionKind
	text string // the literal text, or the referenced [versions] alias
}

// declaration is one library or plugin entry that matched the grammar.
type declaration struct {
	name string // "group:artifact" for a library, the plugin id for a plugin
	ver  versionSpec
}

// Field bits record which keys an inline table already set, so a repeated key
// makes the entry unsupported instead of silently taking the last value.
const (
	fieldModule = 1 << iota
	fieldGroup
	fieldName
	fieldID
	fieldVersionLiteral
	fieldVersionTable
	fieldVersionRef
	fieldVersionRich
)

// tableFields is the decoded content of one library or plugin inline table.
type tableFields struct {
	seen                    int
	module, group, name, id string
	ver                     versionSpec
}

// keyParts returns the components of the key of a KeyValue node. ok is false
// when the key has more than max components.
func keyParts(node *unstable.Node, max int) (parts [][]byte, ok bool) {
	for it := node.Key(); it.Next(); {
		if len(parts) == max {
			return nil, false
		}
		parts = append(parts, it.Node().Data)
	}
	return parts, len(parts) > 0
}

// boundedString copies data when it fits MaxFieldBytes.
func boundedString(data []byte) (string, bool) {
	if len(data) == 0 || len(data) > MaxFieldBytes {
		return "", false
	}
	return string(data), true
}

// setText stores a string value once.
func (f *tableFields) setText(slot *string, bit int, value *unstable.Node) bool {
	if f.seen&bit != 0 || value.Kind != unstable.String {
		return false
	}
	text, ok := boundedString(value.Data)
	if !ok {
		return false
	}
	f.seen |= bit
	*slot = text
	return true
}

// readTable decodes an inline table whose keys are limited to the Gradle
// catalog vocabulary: module, group, name, id and version in its three forms.
func readTable(table *unstable.Node) (tableFields, bool) {
	var f tableFields
	for it := table.Children(); it.Next(); {
		pair := it.Node()
		if pair.Kind != unstable.KeyValue {
			return f, false
		}
		parts, ok := keyParts(pair, 2)
		if !ok {
			return f, false
		}
		value := pair.Value()
		var set bool
		switch {
		case len(parts) == 1 && string(parts[0]) == "module":
			set = f.setText(&f.module, fieldModule, value)
		case len(parts) == 1 && string(parts[0]) == "group":
			set = f.setText(&f.group, fieldGroup, value)
		case len(parts) == 1 && string(parts[0]) == "name":
			set = f.setText(&f.name, fieldName, value)
		case len(parts) == 1 && string(parts[0]) == "id":
			set = f.setText(&f.id, fieldID, value)
		case string(parts[0]) == "version":
			set = f.setVersion(parts[1:], value)
		}
		if !set {
			return f, false
		}
	}
	consistent := f.versionConsistent()
	return f, consistent
}

// setVersion records the version key in its three legal shapes: a string, an
// inline table, or a dotted version.<key> pair.
func (f *tableFields) setVersion(rest [][]byte, value *unstable.Node) bool {
	switch {
	case len(rest) == 0 && value.Kind == unstable.String:
		text, ok := boundedString(value.Data)
		if !ok || f.seen&fieldVersionLiteral != 0 {
			return false
		}
		f.seen |= fieldVersionLiteral
		f.ver = versionSpec{kind: versionLiteral, text: text}
		return true
	case len(rest) == 0 && value.Kind == unstable.InlineTable:
		if f.seen&fieldVersionTable != 0 {
			return false
		}
		f.seen |= fieldVersionTable
		f.ver = readVersionTable(value)
		return true
	case len(rest) == 1 && string(rest[0]) == "ref" && value.Kind == unstable.String:
		text, ok := boundedString(value.Data)
		if !ok || f.seen&fieldVersionRef != 0 {
			return false
		}
		f.seen |= fieldVersionRef
		f.ver = versionSpec{kind: versionRef, text: text}
		return true
	case len(rest) == 1:
		f.seen |= fieldVersionRich
		f.ver = versionSpec{kind: versionRich}
		return true
	}
	return false
}

// readVersionTable classifies { ref = "x" } as a reference and every other
// version table as rich. A rich version (strictly, require, prefer, reject)
// is a constraint, not one version, so the parser does not pick a value.
func readVersionTable(table *unstable.Node) versionSpec {
	only := table.Children()
	if only.Next() {
		pair := only.Node()
		if !only.Next() && pair.Kind == unstable.KeyValue {
			if parts, ok := keyParts(pair, 2); ok && len(parts) == 1 && string(parts[0]) == "ref" &&
				pair.Value().Kind == unstable.String {
				if text, ok := boundedString(pair.Value().Data); ok {
					return versionSpec{kind: versionRef, text: text}
				}
			}
		}
	}
	return versionSpec{kind: versionRich}
}

// versionConsistent rejects mixed version forms such as version and version.ref
// together, which Gradle also rejects.
func (f *tableFields) versionConsistent() bool {
	forms := 0
	for _, bit := range []int{fieldVersionLiteral, fieldVersionTable, fieldVersionRef, fieldVersionRich} {
		if f.seen&bit != 0 {
			forms++
		}
	}
	return forms <= 1
}

// readLibrary decodes one [libraries] value: a "group:artifact[:version]"
// string, or an inline table with module or group and name.
func readLibrary(value *unstable.Node) (declaration, bool) {
	switch value.Kind {
	case unstable.String:
		return readNotation(value.Data, 3)
	case unstable.InlineTable:
		f, ok := readTable(value)
		if !ok || f.seen&fieldID != 0 {
			return declaration{}, false
		}
		hasModule := f.seen&fieldModule != 0
		hasPair := f.seen&(fieldGroup|fieldName) == fieldGroup|fieldName
		onlyOne := f.seen&(fieldGroup|fieldName) != 0 && !hasPair
		switch {
		case hasModule == hasPair, onlyOne:
			return declaration{}, false
		case hasModule:
			parts := bytes.Split([]byte(f.module), []byte{':'})
			if len(parts) != 2 || !validCoordinatePart(parts[0]) || !validCoordinatePart(parts[1]) {
				return declaration{}, false
			}
			return declaration{name: f.module, ver: f.ver}, true
		default:
			if !validCoordinatePart([]byte(f.group)) || !validCoordinatePart([]byte(f.name)) {
				return declaration{}, false
			}
			return declaration{name: f.group + ":" + f.name, ver: f.ver}, true
		}
	default:
		return declaration{}, false
	}
}

// readPlugin decodes one [plugins] value: an "id:version" string, or an inline
// table with id and an optional version.
func readPlugin(value *unstable.Node) (declaration, bool) {
	switch value.Kind {
	case unstable.String:
		return readNotation(value.Data, 2)
	case unstable.InlineTable:
		f, ok := readTable(value)
		if !ok || f.seen&(fieldModule|fieldGroup|fieldName) != 0 || f.seen&fieldID == 0 ||
			!validCoordinatePart([]byte(f.id)) {
			return declaration{}, false
		}
		return declaration{name: f.id, ver: f.ver}, true
	default:
		return declaration{}, false
	}
}

// readNotation splits the string notation. Gradle's TOML reader requires
// exactly "group:artifact:version" for a library (parts 3) and exactly
// "plugin.id:version" for a plugin (parts 2), and fails the build on any other
// part count or an empty part, so neither form is read with a part missing.
// The name is every part but the last.
func readNotation(data []byte, parts int) (declaration, bool) {
	if len(data) == 0 || len(data) > 3*MaxFieldBytes {
		return declaration{}, false
	}
	split := bytes.Split(data, []byte{':'})
	if len(split) != parts {
		return declaration{}, false
	}
	names, version := split[:parts-1], split[parts-1]
	for _, part := range names {
		if !validCoordinatePart(part) {
			return declaration{}, false
		}
	}
	if len(version) == 0 {
		return declaration{}, false
	}
	return declaration{
		name: string(bytes.Join(names, []byte{':'})),
		ver:  versionSpec{kind: versionLiteral, text: string(version)},
	}, true
}

// validCoordinatePart accepts the characters Maven allows in a group id,
// artifact id and plugin id: letters, digits, dot, underscore and hyphen.
func validCoordinatePart(part []byte) bool {
	if len(part) == 0 || len(part) > MaxFieldBytes {
		return false
	}
	for _, b := range part {
		switch {
		case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9', b == '.', b == '_', b == '-':
		default:
			return false
		}
	}
	return true
}

// exactVersionText reports whether text names one exact version, the only
// text a resolved row may carry. Beyond validVersionText it rejects what
// Gradle reads as a selector or a modifier rather than a version: a dynamic
// version ("1.+", "+"), a "latest." status selector, a range ("[1.0,2.0)",
// "]1.0,2.0[", "1.0,2.0"), a strict marker ("1.0!!", "[1.0,2.0)!!1.5", any
// '!'), and an artifact-type suffix ("1.0@jar"). Gradle's StrictVersionParser
// reads "1.0!!" as strictly 1.0, so even that text is not the version.
func exactVersionText(text string) bool {
	if !validVersionText(text) || strings.ContainsAny(text, "+!@[](),") {
		return false
	}
	return len(text) < len("latest.") || !strings.EqualFold(text[:len("latest.")], "latest.")
}

// validVersionText accepts printable ASCII except whitespace, quotes,
// backslash, and the characters that start an interpolation or a shell
// construct. It is the character floor; exactVersionText adds the Gradle
// selector rules.
func validVersionText(text string) bool {
	if text == "" || len(text) > MaxFieldBytes {
		return false
	}
	for i := 0; i < len(text); i++ {
		b := text[i]
		if b <= ' ' || b >= 0x7f {
			return false
		}
		switch b {
		case '"', '\'', '\\', '$', '`', '{', '}', '<', '>', '|', ';', '&':
			return false
		}
	}
	return true
}

// validAlias accepts a non-empty alias without control characters.
func validAlias(alias string) bool {
	if alias == "" || len(alias) > MaxFieldBytes {
		return false
	}
	for i := 0; i < len(alias); i++ {
		if alias[i] < ' ' || alias[i] == 0x7f {
			return false
		}
	}
	return true
}
