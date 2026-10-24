// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzEmitPinFileInvariants checks the contract invariants on arbitrary pin
// file bodies for every source-kind grammar: each fact decodes, its raw_spec
// is text from the body, the spec fields match the spec kind, an
// operator-only raw_spec is never a version, order indices on a line run
// 0..n-1, and a .swift-version fact has numbers only for a plain release.
func FuzzEmitPinFileInvariants(f *testing.F) {
	for _, seed := range []string{
		"18 - 20\n", "= 20\n", ">= 18 < 21\n", "^14 || >=16\n", "20 -\n", "nodejs 18 - 20\n",
		"nodejs 20.11.1 18.19.0\n", "java=18 - 20\n", "5.9\n", "swift-5.9-RELEASE\n",
		"5.10-DEVELOPMENT-SNAPSHOT-2024-01-01-a\n", "${LO} - ${HI}\n", "{{ v }} >=\n", "==  20\n",
	} {
		f.Add(seed)
	}
	paths := []string{".nvmrc", ".python-version", ".tool-versions", ".sdkmanrc", "rust-toolchain", ".swift-version"}
	f.Fuzz(func(t *testing.T, body string) {
		if len(body) > MaxBytesPerFile {
			return
		}
		// Invalid UTF-8 decodes to the replacement rune (see
		// TestEmitDecodesInvalidUTF8ToTheReplacementRune), so raw_spec is
		// compared with the body only when the body is valid text.
		validText := utf8.ValidString(body)
		for _, path := range paths {
			lastOrder := map[int]int{}
			for _, v := range emitDecoded(t, path, body) {
				if v.RawSpec == "" || (validText && !strings.Contains(body, v.RawSpec)) {
					t.Fatalf("%s: raw_spec %q is not text of the body %q", path, v.RawSpec, body)
				}
				numbers := v.VersionMajor != nil || v.VersionMinor != nil || v.VersionPatch != nil
				bounds := v.RangeLowerMajor != nil || v.RangeUpperMajor != nil || v.RangeLower != nil || v.RangeUpper != nil
				switch v.SpecKind {
				case SpecKindExact, SpecKindPartial:
					if v.VersionMajor == nil || bounds {
						t.Fatalf("%s: %s %q has major %v and bounds %v", path, v.SpecKind, v.RawSpec, v.VersionMajor, bounds)
					}
				case SpecKindRange:
					if numbers {
						t.Fatalf("%s: range %q fills version fields", path, v.RawSpec)
					}
				case SpecKindAlias, SpecKindUnresolved:
					if numbers || bounds {
						t.Fatalf("%s: %s %q carries numbers", path, v.SpecKind, v.RawSpec)
					}
				default:
					t.Fatalf("%s: unknown spec kind %q", path, v.SpecKind)
				}
				if isOperatorOnly(strings.TrimSpace(v.RawSpec)) && v.SpecKind != SpecKindAlias {
					t.Fatalf("%s: operator-only raw_spec %q classified %s", path, v.RawSpec, v.SpecKind)
				}
				if path == ".swift-version" && numbers && !swiftReleasePattern.MatchString(v.RawSpec) {
					t.Fatalf(".swift-version %q is not a plain release yet has version fields", v.RawSpec)
				}
				prev, seen := lastOrder[v.LineNumber]
				if (!seen && v.OrderIndex != 0) || (seen && v.OrderIndex != prev+1) {
					t.Fatalf("%s: line %d order_index %d follows %d (seen %v)", path, v.LineNumber, v.OrderIndex, prev, seen)
				}
				lastOrder[v.LineNumber] = v.OrderIndex
			}
		}
	})
}
