// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"
	"testing"
)

// TestDartManifestContentOnlyDispatch drives the manifestDartPubspec and
// manifestDartFvmrc Extract arms through the content-only path, asserting
// section mapping, order indexes, and spec kinds.
func TestDartManifestContentOnlyDispatch(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, path, body, kind, raw, spec string
		line, order                       int
	}{
		{"pubspec sdk", "pubspec.yaml", "name: demo\nenvironment:\n  sdk: '>=3.0.0 <4.0.0'\n", "dart_pubspec_environment_sdk", ">=3.0.0 <4.0.0", "range", 3, 0},
		{"nested pubspec", "services/api/pubspec.yaml", "name: demo\nenvironment:\n  sdk: ^3.0.0\n", "dart_pubspec_environment_sdk", "^3.0.0", "range", 3, 0},
		{"fvmrc exact", ".fvmrc", `{"flutter":"3.22.0"}`, "dart_fvmrc_flutter", "3.22.0", "exact", 1, 0},
		{"nested fvmrc", "services/api/.fvmrc", "{\n  \"flutter\": \"3.22.0\"\n}", "dart_fvmrc_flutter", "3.22.0", "exact", 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := emitDecoded(t, tc.path, tc.body)
			if len(got) == 0 {
				t.Fatalf("Emit(%q) emitted nothing", tc.path)
			}
			first := got[0]
			if first.SourceKind != tc.kind || first.RawSpec != tc.raw || first.SpecKind != tc.spec || first.Runtime != "dart" || first.LineNumber != tc.line || first.OrderIndex != tc.order {
				t.Fatalf("fact = %+v, want kind=%s raw=%s spec=%s line=%d order=%d", first, tc.kind, tc.raw, tc.spec, tc.line, tc.order)
			}
		})
	}
}

// TestDartManifestSkips proves malformed and credential bodies skip without
// emitting, mirroring the sibling dotnet dispatch tests.
func TestDartManifestSkips(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, path, body, reason string
		rejected                 int
	}{
		{"pubspec malformed", "pubspec.yaml", "name: demo\nenvironment:\n  sdk: [unclosed\n", SkipReasonManifestUnparseable, 0},
		{"pubspec credential", "pubspec.yaml", "name: demo\nenvironment:\n  sdk: 'https://user:password@example.invalid'\n", SkipReasonValueRejected, 1},
		{"fvmrc malformed", ".fvmrc", `{"flutter":`, SkipReasonManifestUnparseable, 0},
		{"fvmrc credential", ".fvmrc", `{"flutter":"https://user:password@example.invalid"}`, SkipReasonValueRejected, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes, skip := EmitFile(testContext(tc.path), "repo-1", tc.path, tc.body)
			if len(envelopes) != 0 || skip.Reason != tc.reason || skip.Rejected != tc.rejected {
				t.Fatalf("emitted %d, skip %+v", len(envelopes), skip)
			}
		})
	}
}

// TestDartManifestByteCap proves an over-cap manifest skips whole with the
// manifest byte cap, mirroring the sibling dotnet byte-cap test.
func TestDartManifestByteCap(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, path, body string }{
		{"pubspec", "pubspec.yaml", "name: demo\nenvironment:\n  sdk: '>=3.0.0'\n# " + strings.Repeat("x", MaxManifestBytesPerFile)},
		{"fvmrc", ".fvmrc", `{"flutter":"3.22.0","padding":"` + strings.Repeat("x", MaxManifestBytesPerFile) + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes, skip := EmitFile(testContext(tc.path), "repo-1", tc.path, tc.body)
			if len(envelopes) != 0 || skip.Reason != SkipReasonByteCap || skip.ByteSize != len(tc.body) || skip.ByteCap != MaxManifestBytesPerFile {
				t.Fatalf("over-cap %s emitted %d declarations, skip %+v", tc.name, len(envelopes), skip)
			}
		})
	}
}
