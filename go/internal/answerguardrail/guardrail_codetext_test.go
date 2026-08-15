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
// Two guards came out of it, and they are independent — neither one alone
// clears the table:
//
//   - At least one non-empty hextet group. An address has hex digits somewhere;
//     a bare "::" between punctuation does not. Kills "a[::-1]", "(::)", "::$x".
//   - An identifier character before "[" means a subscript, not an address.
//     "[::2]" is an address, "x[::2]" is a slice, and the "x" is the only thing
//     that separates them. Kills "x[::2]", "x[::]", "arr[::3]".
//
// What still gets withheld, and why that is the same accepted gap "abc::def"
// already documents: when the token on either side of "::" is 1-4 hex
// characters, it IS a valid hextet and no rule can tell it from an address.
// "a::b::c", "::f", and PHP's "DB::$connection" all land there. Every language
// idiom below whose segments are not all-hex publishes clean.
var codeTextIPv6Shapes = map[string]bool{
	// Python slices. The reported bug, and the reason for both guards.
	"x[::2]":                          false,
	"x[::]":                           false,
	"a[::-1]":                         false,
	"s[::-1]":                         false,
	"arr[::3]":                        false,
	"path[1::2]":                      false,
	"rows[::2] takes every other row": false,
	"reverse a list with a[::-1]":     false,
	"data[::step]":                    false,
	// Go slice and range syntax. "buf[0:4]" and "buf[0:4:8]" carry no "::" and
	// never reached the rule; "s[::]" does.
	"s[::]":       false,
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
	// A doubled bracket still carries the address, so the identifier guard must
	// read the character before the brackets, not the bracket itself.
	"[[fd00::1]": true,
	// Every spelling the product actually emits puts a non-identifier
	// character before the bracket, and all of them stay caught. The row below
	// them is the price of the identifier guard, stated with the rows it
	// belongs to rather than left for someone to discover.
	"host [fd00::1]":        true,
	"endpoint=[fd00::1]":    true,
	"bolt://[fd00::1]:7687": true,
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
	// The gap the identifier guard buys, and it cuts both ways: "x[fd00::1]"
	// is as valid a Python slice as "x[::2]" is, so no rule can call one a
	// slice and the other an address. This one publishes, and the three rows
	// above -- the spellings a real answer uses -- do not.
	"buf[fd00::1]": false,
	"peer[::1]":    false,
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
	// A table this long is worth counting: a map literal that lost rows to a bad
	// merge would otherwise report a clean sweep over whatever survived.
	if want := len(codeTextIPv6Shapes); checked != want {
		t.Fatalf("checked %d values, want %d", checked, want)
	}
}
