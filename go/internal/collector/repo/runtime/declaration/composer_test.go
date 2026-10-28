// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"
	"testing"
)

func TestComposerPHPDeclarations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		path, body, source, raw, kind string
	}{
		{"composer.json", `{"require":{"php":"^8.2"}}`, "composer_json_require_php", "^8.2", "range"},
		{"composer.json", `{"config":{"platform":{"php":"8.3.12"}}}`, "composer_json_platform_php", "8.3.12", "exact"},
		{"composer.lock", `{"platform":{"php":">=8.1"}}`, "composer_lock_platform_php", ">=8.1", "range"},
		{"composer.lock", `{"platform-overrides":{"php":"8.2.1"}}`, "composer_lock_platform_overrides_php", "8.2.1", "exact"},
	} {
		got := emitDecoded(t, tc.path, tc.body)
		if len(got) != 1 || got[0].Runtime != "php" || got[0].SourceKind != tc.source || got[0].RawSpec != tc.raw || got[0].SpecKind != tc.kind {
			t.Errorf("%s: got %+v, want %s %s %s", tc.path, got, tc.source, tc.raw, tc.kind)
		}
	}
}

func TestComposerPHPGuardAndMalformed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body, reason string
		rejected           int
	}{
		{"secret", `{"require":{"php":"0123456789abcdef-secret"}}`, SkipReasonValueRejected, 1},
		{"malformed", `{"require":{"php":"8.2",}`, SkipReasonManifestUnparseable, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			envelopes, skip := EmitFile(testContext("composer.json"), "repo-1", "composer.json", tc.body)
			if len(envelopes) != 0 || skip.Reason != tc.reason || skip.Rejected != tc.rejected {
				t.Errorf("emitted %d, skip %+v", len(envelopes), skip)
			}
		})
	}
}

func TestComposerPHPByteCap(t *testing.T) {
	body := `{"require":{"php":"^8.2"},"padding":"` + strings.Repeat("x", MaxManifestBytesPerFile) + `"}`
	envelopes, skip := EmitFile(testContext("composer.json"), "repo-1", "composer.json", body)
	if len(envelopes) != 0 || skip.Reason != SkipReasonByteCap || skip.ByteSize != len(body) {
		t.Fatalf("over-cap Composer body emitted %d declarations, skip %+v", len(envelopes), skip)
	}
}

func TestComposerPHPSourceKindsClassifyEachSpecShape(t *testing.T) {
	sources := []struct{ path, prefix, suffix, kind string }{
		{"composer.json", `{"require":{"php":"`, `"}}`, "composer_json_require_php"},
		{"composer.json", `{"config":{"platform":{"php":"`, `"}}}`, "composer_json_platform_php"},
		{"composer.lock", `{"platform":{"php":"`, `"}}`, "composer_lock_platform_php"},
		{"composer.lock", `{"platform-overrides":{"php":"`, `"}}`, "composer_lock_platform_overrides_php"},
	}
	specs := []struct{ raw, kind string }{{"8.3.1", "exact"}, {"^8.2", "range"}, {"latest", "alias"}, {"${PHP_VERSION}", "unresolved"}}
	for _, source := range sources {
		for _, spec := range specs {
			t.Run(source.kind+"/"+spec.kind, func(t *testing.T) {
				got := emitDecoded(t, source.path, source.prefix+spec.raw+source.suffix)
				if len(got) != 1 || got[0].SourceKind != source.kind || got[0].RawSpec != spec.raw || got[0].SpecKind != spec.kind {
					t.Fatalf("declarations = %+v", got)
				}
			})
		}
	}
}

func TestComposerPHPSourceKindsRejectMalformedAndCredentialValues(t *testing.T) {
	const secret = "0123456789abcdef-secret"
	for _, source := range []struct{ path, prefix, suffix, kind string }{
		{"composer.json", `{"require":{"php":"`, `"}}`, "composer_json_require_php"},
		{"composer.json", `{"config":{"platform":{"php":"`, `"}}}`, "composer_json_platform_php"},
		{"composer.lock", `{"platform":{"php":"`, `"}}`, "composer_lock_platform_php"},
		{"composer.lock", `{"platform-overrides":{"php":"`, `"}}`, "composer_lock_platform_overrides_php"},
	} {
		t.Run(source.kind, func(t *testing.T) {
			for _, tc := range []struct {
				name, body, reason string
				rejected           int
			}{
				{"malformed document", source.prefix + "8.2" + source.suffix + ` trailing`, SkipReasonManifestUnparseable, 0},
				{"credential", source.prefix + secret + source.suffix, SkipReasonValueRejected, 1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					envelopes, skip := EmitFile(testContext(source.path), "repo-1", source.path, tc.body)
					if len(envelopes) != 0 || skip.Reason != tc.reason || skip.Rejected != tc.rejected {
						t.Fatalf("%s emitted %d declarations, skip %+v", source.kind, len(envelopes), skip)
					}
				})
			}
		})
	}
}
