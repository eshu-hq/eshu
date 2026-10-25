// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"
	"testing"
)

// FuzzEmitManifestInvariants checks the contract invariants on arbitrary
// package.json and go.mod bodies: no panic, every fact decodes through the
// contract, raw_spec passed the credential guard, spec fields match the spec
// kind, stable keys never repeat, line numbers sit inside the body, and an
// unreadable package.json emits nothing.
func FuzzEmitManifestInvariants(f *testing.F) {
	for _, seed := range []string{
		prettyPackageJSON, realisticGoMod, `{"engines":{"node":">=18"}}`, `{"engines":{"node":"18"}`,
		"\xef\xbb\xbf{\"volta\":{\"node\":\"20.1.0\"}} // c", `{"packageManager":"pnpm@9.1.0+sha512.ab"}`,
		"go 1.22\ntoolchain go1.22.3\n", "go 1.22\ngo 1.23\n", "require (\ngo 1.22\n)\n", `{"a":[[[[1]]]],"engines":{"npm":"^10"}}`,
		`{"engines":{"node":">=18"}}`, "go \"1.22\" // x\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body string) {
		if len(body) > MaxManifestBytesPerFile {
			return
		}
		for _, path := range []string{"package.json", "go.mod"} {
			envelopes, skip := EmitFile(testContext(path), "repo-1", path, body)
			if skip.Reason == SkipReasonManifestUnparseable && len(envelopes) != 0 {
				t.Fatalf("%s: unparseable body emitted %d facts", path, len(envelopes))
			}
			if len(envelopes) > 9 || (path == "go.mod" && len(envelopes) > 2) {
				t.Fatalf("%s: emitted %d facts, more than the manifest can declare", path, len(envelopes))
			}
			keys := map[string]bool{}
			for _, env := range envelopes {
				if keys[env.StableFactKey] {
					t.Fatalf("%s: stable key %q repeats on %q", path, env.StableFactKey, body)
				}
				keys[env.StableFactKey] = true
			}
			lines := 1 + strings.Count(body, "\n")
			for _, v := range emitDecoded(t, path, body) {
				if !safeSpec(v.RawSpec) {
					t.Fatalf("%s: raw_spec %q did not pass the guard", path, v.RawSpec)
				}
				if v.LineNumber < 1 || v.LineNumber > lines {
					t.Fatalf("%s: line %d outside 1..%d", path, v.LineNumber, lines)
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
			}
		}
	})
}
