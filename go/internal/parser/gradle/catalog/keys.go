// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"bytes"
	"encoding/binary"

	"github.com/pelletier/go-toml/v2/unstable"
)

// keyKind is what a TOML key names in keyTree.
type keyKind uint8

const (
	keyTable      keyKind = iota // a table, defined or only implied
	keyValue                     // a key-value pair (an inline table is a value)
	keyArrayTable                // a [[name]] array of tables
)

// keyNode is one key in keyTree.
type keyNode struct {
	kind keyKind
	// defined is true once a [header] defines the table.
	defined bool
	// dottedIn is the section in which a dotted key created the table, or -1
	// when a header created or implied it. Dotted keys may extend such a
	// table only inside that section.
	dottedIn int32
}

// keyTree applies TOML's rules on where a key may be defined to every
// top-level expression: a key is defined once, a table header names a table
// once, a header never redefines a table that dotted keys created, a dotted
// key never extends a table outside the section that created it, a value is
// never used as a table, and an inline table is closed. It follows the
// decisions of go-toml's key tracker, which a decode into a map applies and
// which a decode into an empty struct skips for every key under a header
// (so that decode accepted a library defined twice and returned rows). A
// decode into a map is not used instead: it stores every value and searches
// each table's keys in a list, which measured 74.8 ms against 1.0 ms on a
// 10,005-line catalog and 11.6 s on 80,000 entries in one table.
//
// Each check is one map operation per key segment: the tree is indexed by
// parent node and key, so a table with many keys stays linear. A small
// inline table is checked by a list scan instead (see smallInline). Memory
// grows with the distinct key segments in the file, about 70 bytes each,
// which MaxFileKeySegments bounds.
type keyTree struct {
	nodes   []keyNode
	index   map[string]int32 // parent id (4 bytes) + key -> node id
	current int32            // the table of the latest header; 0 is the root
	section int32            // count of headers seen
	buf     []byte           // scratch space for index keys
	inline  []inlineKey      // keys of the inline tables being checked
}

// inlineKey is one key of a small inline table, checked without the index.
type inlineKey struct {
	parent int32 // index in keyTree.inline, or -1 for the inline table itself
	name   []byte
	value  bool
}

// maxInlineKeys is the most key segments an inline table may have to be
// checked by a list scan. Catalog inline tables hold two to four; a larger
// one uses the index, so the scan never grows quadratic.
const maxInlineKeys = 16

// newKeyTree returns a tree holding only the root table.
func newKeyTree(hint int) *keyTree {
	t := &keyTree{
		nodes: make([]keyNode, 1, hint+1),
		index: make(map[string]int32, hint),
	}
	t.nodes[0] = keyNode{kind: keyTable, defined: true, dottedIn: -1}
	return t
}

// keyHint estimates how many keys enter the index so it is sized once: one
// per line, capped at one per 16 bytes. Presizing halves the index's
// allocation on a large catalog. Keys of small inline tables never enter the
// index (see smallInline), so a line rarely adds more than one. The cap
// keeps the waste small on a file of empty lines; a denser file only grows
// the map.
func keyHint(source []byte) int {
	return min(bytes.Count(source, []byte{'\n'})+1, len(source)/16)
}

// Reasons keyTree reports. They name no file content.
const (
	reasonKeyRepeated     = "a key is defined twice"
	reasonTableRepeated   = "a table is defined twice"
	reasonValueAsTable    = "a key names a value and a table"
	reasonClosedTable     = "a dotted key extends a table defined elsewhere"
	reasonArrayTableMixed = "a key names an array of tables and another kind"
)

// check applies the rules to one top-level expression. A non-empty return is
// the reason the file is malformed.
func (t *keyTree) check(node *unstable.Node) string {
	switch node.Kind {
	case unstable.Table:
		return t.header(node, false)
	case unstable.ArrayTable:
		return t.header(node, true)
	case unstable.KeyValue:
		return t.keyValue(t.current, node)
	default:
		// Comments are not kept; no other kind is a top-level expression.
	}
	return ""
}

// closed reports whether a dotted key may no longer extend the table id.
func (t *keyTree) closed(id int32) bool {
	n := t.nodes[id]
	return n.defined || (n.dottedIn >= 0 && n.dottedIn != t.section)
}

// header handles [a.b.c] and [[a.b.c]]. Every component before the last
// must be a table or an array of tables (its latest element); the last
// names a table not defined before, or an array of tables that gains an
// element.
func (t *keyTree) header(node *unstable.Node, array bool) string {
	t.section++
	parent := int32(0)
	it := node.Key()
	for it.Next() {
		name := it.Node().Data
		id, found := t.find(parent, name)
		if it.IsLast() {
			return t.headerLast(parent, name, id, found, array)
		}
		switch {
		case !found:
			id = t.create(parent, name, keyNode{kind: keyTable, dottedIn: -1})
		case t.nodes[id].kind == keyValue:
			return reasonValueAsTable
		}
		parent = id
	}
	return ""
}

// headerLast defines the table or array element a header's last component
// names and makes it the current table.
func (t *keyTree) headerLast(parent int32, name []byte, id int32, found, array bool) string {
	switch {
	case !found && array:
		id = t.create(parent, name, keyNode{kind: keyArrayTable, defined: true, dottedIn: -1})
	case !found:
		id = t.create(parent, name, keyNode{kind: keyTable, defined: true, dottedIn: -1})
	case array:
		if t.nodes[id].kind != keyArrayTable {
			return reasonArrayTableMixed
		}
		// A new element: a fresh node, so the keys of the previous element
		// (indexed under the old id) are never found again.
		id = t.create(parent, name, keyNode{kind: keyArrayTable, defined: true, dottedIn: -1})
	case t.nodes[id].kind == keyArrayTable:
		return reasonArrayTableMixed
	case t.nodes[id].kind != keyTable:
		return reasonValueAsTable
	case t.closed(id):
		return reasonTableRepeated
	default:
		t.nodes[id].defined = true
	}
	t.current = id
	return ""
}

// keyValue handles a.b.c = value under the table parent. Each component
// before the last is a table that dotted keys of this section created or
// that a header only implied; the last is a new key.
func (t *keyTree) keyValue(parent int32, node *unstable.Node) string {
	it := node.Key()
	for it.Next() {
		name := it.Node().Data
		id, found := t.find(parent, name)
		switch {
		case !found:
			id = t.create(parent, name, keyNode{kind: keyTable, dottedIn: t.section})
		case it.IsLast():
			return reasonKeyRepeated
		case t.nodes[id].kind == keyArrayTable:
			return reasonArrayTableMixed
		case t.nodes[id].kind != keyTable:
			return reasonValueAsTable
		case t.closed(id):
			return reasonClosedTable
		}
		parent = id
	}
	t.nodes[parent].kind = keyValue
	return t.value(node.Value())
}

// value checks the inline tables inside a value. Each inline table is its
// own closed tree: its keys follow the same rules among themselves, and no
// later expression can reach them because its key is a value. A small one
// is checked by a list scan (smallInline); a larger one through the index,
// under a fresh root. The recursion follows array and inline-table nesting,
// which nestingExceeds bounded.
func (t *keyTree) value(value *unstable.Node) string {
	switch value.Kind {
	case unstable.InlineTable:
		if reason, done := t.smallInline(value); done {
			return reason
		}
		root := int32(len(t.nodes))
		t.nodes = append(t.nodes, keyNode{kind: keyTable, dottedIn: t.section})
		for it := value.Children(); it.Next(); {
			if reason := t.keyValue(root, it.Node()); reason != "" {
				return reason
			}
		}
	case unstable.Array:
		for it := value.Children(); it.Next(); {
			if reason := t.value(it.Node()); reason != "" {
				return reason
			}
		}
	default:
		// A scalar holds no key.
	}
	return ""
}

// smallInline checks an inline table of at most maxInlineKeys key segments
// against a list of its own keys, with no map operation and no allocation
// once the list has grown. done is false, before any key is checked, when
// the table is larger; the caller then checks it through the index. The
// size is counted first so no key or nested value is ever checked twice.
// Nested inline tables push their keys after this table's and pop them
// before it continues, so one list serves every level.
func (t *keyTree) smallInline(table *unstable.Node) (reason string, done bool) {
	segments := 0
	for it := table.Children(); it.Next(); {
		for key := it.Node().Key(); key.Next(); {
			segments++
		}
	}
	if segments > maxInlineKeys {
		return "", false
	}
	base := len(t.inline)
	defer func() { t.inline = t.inline[:base] }()
	for it := table.Children(); it.Next(); {
		node := it.Node()
		parent := int32(-1)
		for key := node.Key(); key.Next(); {
			name := key.Node().Data
			id := t.findInline(base, parent, name)
			switch {
			case id < 0:
				id = int32(len(t.inline))
				t.inline = append(t.inline, inlineKey{parent: parent, name: name})
			case key.IsLast():
				return reasonKeyRepeated, true
			case t.inline[id].value:
				return reasonValueAsTable, true
			}
			parent = id
		}
		t.inline[parent].value = true
		if reason := t.value(node.Value()); reason != "" {
			return reason, true
		}
	}
	return "", true
}

// findInline returns the index of name under parent among the keys pushed
// since base, or -1.
func (t *keyTree) findInline(base int, parent int32, name []byte) int32 {
	for i := base; i < len(t.inline); i++ {
		if t.inline[i].parent == parent && bytes.Equal(t.inline[i].name, name) {
			return int32(i)
		}
	}
	return -1
}

// indexKey writes parent and name into the scratch buffer and returns it.
func (t *keyTree) indexKey(parent int32, name []byte) []byte {
	t.buf = binary.LittleEndian.AppendUint32(t.buf[:0], uint32(parent))
	t.buf = append(t.buf, name...)
	return t.buf
}

// find returns the node named name under parent. The lookup does not
// allocate.
func (t *keyTree) find(parent int32, name []byte) (int32, bool) {
	id, found := t.index[string(t.indexKey(parent, name))]
	return id, found
}

// create adds node under parent as name, replacing any earlier node of that
// name, and returns its id.
func (t *keyTree) create(parent int32, name []byte, node keyNode) int32 {
	id := int32(len(t.nodes))
	t.nodes = append(t.nodes, node)
	t.index[string(t.indexKey(parent, name))] = id
	return id
}
