// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerguardrail

import "testing"

// The compressed-IPv6 rule reads "::" in ordinary code text, and Ask Eshu
// answers are mostly ordinary code text. The rule shipped on this branch
// withheld Python's step slice:
//
//	x[::2]    withheld    step slice, matched inside the brackets
//	x[::]     withheld    full slice, matched inside the brackets
//	a[::-1]   withheld    reverse slice, matched around the brackets
//
// "a[::-1]" is the one with no defence: a "-" cannot appear anywhere in an IPv6
// address, so nothing about that string is address-shaped. It matched because
// both hextet groups around the "::" were optional, which left the rule reading
// a bare "::" between any two non-alphanumeric characters as an address. That is
// every one of "(::)", "-::-", "$::$", and "]::[" too, not only the slice.
//
// The three reported strings were three samples of a class, so this table is the
// class: every language whose scope-resolution or slice syntax puts "::" next to
// a character an address cannot contain. The true positives sit in the SAME
// table, because split apart, a rule that rejects everything and a rule that
// rejects nothing each look correct in one half.
//
// One guard came out of it: at least one non-empty hextet group. An address has
// hex digits somewhere; a bare "::" between punctuation does not. That clears
// "a[::-1]", "(::)", "-::-", "]::[", and "::$x".
//
// A second guard was tried and reverted, and the rows below are what reverted
// it. It read an identifier character before "[" as a subscript marker, on the
// reasoning that "[::2]" is an address and "x[::2]" is a slice. It does clear
// the step slice. It also publishes every address that follows a word, which is
// most of how an address appears in real text -- "client[fd00::1] disconnected",
// "sshd[::1]", "conn[fd00::1]:7687 closed", and Go's own map rendering
// "map[fd00::1:true]". Seventeen such spellings went from withheld to published.
// Narrowing it to fire only on a decimal-and-colon subscript was built and
// measured too: it recovers the twelve carrying a hex letter and still publishes
// "sshd[::1]" and "map[::1]", because "::1" is a valid address AND a valid
// slice. There is no boundary that separates them, so the guard is gone.
//
// What that leaves withheld, and it is the same accepted gap "abc::def" already
// documents: when the token beside "::" is 1-4 hex characters it IS a valid
// hextet, and when a whole bracketed subscript is a valid compressed address it
// IS an address. "a::b::c", PHP's "DB::$connection", and the step slices
// "x[::2]" and "arr[::3]" all land there, pinned below rather than left for
// someone to rediscover. Every language idiom whose segments are not all-hex
// publishes clean.
var codeTextIPv6Shapes = map[string]bool{
	// Python slices, the reported bug. The reverse slice is fixed: a "-" cannot
	// appear in an address, so the hextet guard clears it wherever it sits.
	"a[::-1]":                     false,
	"s[::-1]":                     false,
	"reverse a list with a[::-1]": false,
	// A step whose value is not hex is not an address either.
	"data[::step]": false,
	// The step slices whose brackets hold a valid compressed address. Still
	// withheld, pinned here so a future narrowing has to change this list and
	// argue for it. "[::2]" is 0:0:0:0:0:0:0:2 as surely as it is a stride.
	"x[::2]":                          true,
	"x[::]":                           true,
	"arr[::3]":                        true,
	"path[1::2]":                      true,
	"rows[::2] takes every other row": true,
	// Go slice and range syntax. "buf[0:4]" and "buf[0:4:8]" carry no "::" and
	// never reached the rule; "s[::]" does, and lands in the gap above.
	"s[::]":       true,
	"buf[0:4]":    false,
	"buf[0:4:8]":  false,
	"x := y":      false,
	"m[k] lookup": false,
	// A bare "::" between two punctuation characters. None of these is
	// address-shaped, and all of them were withheld.
	"<::>":   false,
	"-::-":   false,
	"$::$":   false,
	"]::[":   false,
	"(::)":   false,
	"{::}":   false,
	"*::*":   false,
	"'::'":   false,
	`"::"`:   false,
	"&::&":   false,
	"?::?":   false,
	",::,":   false,
	".::.":   false,
	";::;":   false,
	"a-::-b": false,
	"::$x":   false,
	"::":     false,
	// C++.
	"std::vector":              false,
	"std::vector<int>":         false,
	"Foo::Bar":                 false,
	"::global":                 false,
	"::std::move":              false,
	"MyClass::~MyClass":        false,
	"std::chrono::duration":    false,
	"std::map<K,V>::iterator":  false,
	"ns::fn(-1)":               false,
	"call Foo::Bar() directly": false,
	"the operator:: overload":  false,
	// Rust.
	"crate::mod":           false,
	"<T as Trait>::method": false,
	"self::inner":          false,
	"super::parent":        false,
	"Vec::<u8>::new":       false,
	"std::vec::Vec":        false,
	"::core::mem":          false,
	"Option::<i32>::None":  false,
	"T::default()":         false,
	// PHP.
	"self::CONST":         false,
	"Class::$var":         false,
	"static::method":      false,
	"parent::__construct": false,
	"Foo::class":          false,
	// Ruby.
	"Module::CONST": false,
	"Net::HTTP":     false,
	"::TopLevel":    false,
	"Foo::Bar::Baz": false,
	// Perl.
	"Data::Dumper": false,
	"List::Util":   false,
	"$Foo::bar":    false,
	"main::":       false,
	"::baz":        false,

	// The other half. Without these rows a rule that withheld nothing would
	// pass this table.
	"fd00::1":                    true,
	"fe80::1":                    true,
	"fc00::42":                   true,
	"::1":                        true,
	"2001:db8::1":                true,
	"[::1]":                      true,
	"[::]":                       true,
	"[fd00::1]:7687":             true,
	"[2001:db8::1]":              true,
	"::ffff:10.0.5.3":            true,
	"peer:fd00::1":               true,
	"listening on [::]:8080 now": true,
	"[[fd00::1]":                 true,
	"host [fd00::1]":             true,
	"endpoint=[fd00::1]":         true,
	"bolt://[fd00::1]:7687":      true,
	// Accepted gap, stated with the rows it belongs to: 1-4 hex characters on
	// both sides of "::" IS a compressed address by shape, so these stay
	// withheld for the same reason "abc::def" does. Pinned rather than left
	// implicit, so a future narrowing that changes them has to change this list.
	"abc::def":        true,
	"a::b::c":         true,
	"x = ::f(-1)":     true,
	"a::b(-1)":        true,
	"DB::$connection": true,
	"A::$b":           true,
	"buf[fd00::1]":    true,
	"peer[::1]":       true,

	// An address preceded by an ordinary word. AnswerSummary is model-written
	// narration over the user's graph, not a format string this product
	// controls, so "the spellings we emit all put a space before the bracket"
	// is not a claim anyone can check. These are the shapes that assumption
	// misses: Go's own %v rendering of a map keyed by an address, and the
	// bracketed-tag idiom every syslog line uses.
	"map[fd00::1:true]":            true,
	"map[[fd00::1]:7687:true]":     true,
	"client[fd00::1] disconnected": true,
	"conn[fd00::1]:7687 closed":    true,
	"server[fd00::1]":              true,
	"addr[fd00::1]":                true,
	"peers[fd00::1]":               true,
	"hosts[fd00::1]":               true,
	"HOST[fd00::1]":                true,
	"SERVICE[fd00::1]":             true,
	"see[fd00::1](url)":            true,
	"sshd[::1]":                    true,
	"node1[::1]":                   true,
	"map[::1:true]":                true,
	// No closing bracket, so nothing bracket-shaped can catch this one.
	"x[fd00::1": true,
}

// TestUnsafeStringKeepsCodeTextPublishable runs the class the three reported
// Python slices came from. It is one table rather than a publishable table and a
// withheld table, for the reason stated above the data.
func TestUnsafeStringKeepsCodeTextPublishable(t *testing.T) {
	t.Parallel()

	checked := 0
	for value, want := range codeTextIPv6Shapes {
		checked++
		t.Run(value, func(t *testing.T) {
			got := UnsafeString(value)
			switch {
			case got == want:
			case want:
				t.Fatalf("UnsafeString(%q) = false, want true; the screen stopped catching an address", value)
			default:
				t.Fatalf("UnsafeString(%q) = true; this screen runs on a publish path "+
					"and withholding an honest answer is its own outage", value)
			}
		})
	}
	// A table this long is worth counting, against a literal and not against
	// len() of the same map. Comparing the loop counter to the map it just
	// ranged over is true by construction: rows lost to a bad merge shrink both
	// sides together and the guard stays green over whatever survived. Deleting
	// ten rows was how that was proven, so the number is written out. Update it
	// when you add a row, which is the point.
	if checked != codeTextIPv6ShapeCount {
		t.Fatalf("checked %d values, want %d; codeTextIPv6Shapes lost or gained rows",
			checked, codeTextIPv6ShapeCount)
	}
}

// codeTextIPv6ShapeCount is the number of rows codeTextIPv6Shapes must carry.
const codeTextIPv6ShapeCount = 104
