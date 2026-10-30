// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"fmt"
	"strings"
	"testing"
)

// bs is one backslash, so escape sequences in test bodies stay real escapes
// whatever tool edits this file.
var bs = string(rune(92))

// prettyPackageJSON is a conventionally formatted package.json: every
// declaration sits on its own line, so line numbers are checkable.
const prettyPackageJSON = `{
  "name": "api",
  "version": "1.0.0",
  "engines": {
    "node": ">=18 <21",
    "npm": "^10.2.0"
  },
  "volta": {
    "node": "20.11.1",
    "yarn": "4.1.0"
  },
  "packageManager": "pnpm@9.1.0+sha512.0123456789abcdef"
}
`

func TestEmitPackageJSONReadsEnginesVoltaAndPackageManager(t *testing.T) {
	t.Parallel()

	got := emitDecoded(t, "services/api/package.json", prettyPackageJSON)
	checkFacts(t, got, []wantFact{
		{"services/api", "package_json_engines", "node", "<nil>", ">=18 <21", "range", "<nil>", 5, 0, -1, -1, -1},
		{"services/api", "package_json_engines", "npm", "<nil>", "^10.2.0", "range", "<nil>", 6, 0, -1, -1, -1},
		{"services/api", "package_json_volta", "node", "<nil>", "20.11.1", "exact", "<nil>", 9, 0, 20, 11, 1},
		{"services/api", "package_json_volta", "yarn", "<nil>", "4.1.0", "exact", "<nil>", 10, 0, 4, 1, 0},
		{"services/api", "package_json_package_manager", "pnpm", "<nil>", "9.1.0", "exact", "<nil>", 12, 0, 9, 1, 0},
	})
	if r := got[0]; deref(r.RangeLowerMajor) != 18 || deref(r.RangeUpperMajor) != 21 || r.VersionMajor != nil {
		t.Errorf("engines.node range = %+v, want lower 18 upper 21 and no version parts", r)
	}
}

func TestEmitPackageJSONPackageManagerStripsTheIntegritySuffix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value, wantRaw, wantRuntime, wantKind string
	}{
		{"pnpm@9.1.0+sha512.deadbeef", "9.1.0", "pnpm", "exact"},
		{"yarn@4.1.1", "4.1.1", "yarn", "exact"},
		{"npm@10.5.0+sha224.abc", "10.5.0", "npm", "exact"},
		{"pnpm@9", "9", "pnpm", "partial"},
		{"yarn@berry", "berry", "yarn", "alias"},
		{"pnpm@latest", "latest", "pnpm", "alias"},
		{"bun@1.1.0+sha512.deadbeef", "1.1.0", "bun", "exact"},
		{"deno@1.40.0", "1.40.0", "deno", "exact"},
		{"bun@latest", "latest", "bun", "alias"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()
			got := emitDecoded(t, "package.json", `{"packageManager": "`+tc.value+`"}`)
			if len(got) != 1 {
				t.Fatalf("emitted %d facts, want 1: %+v", len(got), got)
			}
			if g := got[0]; g.RawSpec != tc.wantRaw || g.Runtime != tc.wantRuntime || g.SpecKind != tc.wantKind ||
				g.SourceKind != "package_json_package_manager" || g.ScopeDir != "." {
				t.Errorf("fact = raw %q runtime %q kind %q source %q scope %q, want %q %q %q",
					g.RawSpec, g.Runtime, g.SpecKind, g.SourceKind, g.ScopeDir, tc.wantRaw, tc.wantRuntime, tc.wantKind)
			}
		})
	}
}

func TestEmitPackageJSONUnlistedPackageManagerIsRejected(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"PNPM@9.1.0", "pnpm9", "other@1.0.0"} {
		envelopes, skip := EmitFile(testContext("package.json"), "repo-1", "package.json", `{"packageManager":"`+value+`"}`)
		if len(envelopes) != 0 || skip.Reason != SkipReasonValueRejected || skip.Rejected != 1 {
			t.Errorf("%q: emitted %d, skip %+v, want nothing and a counted rejection", value, len(envelopes), skip)
		}
	}
}

func TestDenoJSONPackageVersionIsNotRuntimeVersion(t *testing.T) {
	t.Parallel()

	body := `{"name":"@example/tool","version":"1.40.0","exports":"./mod.ts",` +
		`"engines":{"deno":"^2"},"runtimeVersion":"2.0.0"}`
	if extraction, ok := Extract("services/tool/deno.json", body); ok || extraction.Len() != 0 {
		t.Fatalf("deno.json extraction = (%d, %t), want no runtime declarations", extraction.Len(), ok)
	}
	if facts := Emit(testContext("deno.json"), "repo-1", "services/tool/deno.json", body); len(facts) != 0 {
		t.Fatalf("deno.json emitted %d runtime facts, want none", len(facts))
	}
}

func TestEmitPackageJSONClassifiesEngineSpecs(t *testing.T) {
	t.Parallel()

	cases := []struct{ spec, wantKind string }{
		{"20.11.1", SpecKindExact},
		{"20", SpecKindPartial},
		{">=18 <21", SpecKindRange},
		{"^18 || ^20", SpecKindRange},
		{"18 - 20", SpecKindRange},
		{"lts/iron", SpecKindAlias},
		{"lts/*", SpecKindAlias},
		{"latest", SpecKindAlias},
		{"*", SpecKindAlias},
		{"workspace:*", SpecKindAlias},
		{"catalog:", SpecKindAlias},
		{"catalog:react18", SpecKindAlias},
		{"npm:node@20", SpecKindAlias},
		{"${NODE_VERSION}", SpecKindUnresolved},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.spec, func(t *testing.T) {
			t.Parallel()
			got := emitDecoded(t, "package.json", `{"engines":{"node":"`+tc.spec+`"}}`)
			if len(got) != 1 || got[0].SpecKind != tc.wantKind || got[0].RawSpec != tc.spec {
				t.Fatalf("engines.node %q = %+v, want one %s fact with that raw_spec", tc.spec, got, tc.wantKind)
			}
			if tc.wantKind != SpecKindExact && tc.wantKind != SpecKindPartial && (got[0].VersionMajor != nil) {
				t.Errorf("%s %q carries version parts", tc.wantKind, tc.spec)
			}
		})
	}
}

func TestEmitPackageJSONIgnoresUnlistedRuntimesAndNonStringValues(t *testing.T) {
	t.Parallel()

	body := `{"engines":{"vscode":"^1.80.0","node":18,"npm":{"v":"10"},"yarn":["1"],"pnpm":null,"bun":">=1"},` +
		`"volta":{"extends":"../package.json","node":true},"packageManager":42}`
	if got := emitDecoded(t, "package.json", body); len(got) != 0 {
		t.Fatalf("emitted %d facts for unlisted runtimes and non-string values, want 0: %+v", len(got), got)
	}
}

func TestEmitPackageJSONOneLineFileGetsUniqueStableKeys(t *testing.T) {
	t.Parallel()

	body := `{"engines":{"node":">=18"},"volta":{"node":"20.11.1"},"packageManager":"npm@10.5.0"}`
	envelopes := Emit(testContext("package.json"), "repo-1", "package.json", body)
	if len(envelopes) != 3 {
		t.Fatalf("emitted %d envelopes, want 3", len(envelopes))
	}
	keys := map[string]bool{}
	for _, env := range envelopes {
		if keys[env.StableFactKey] {
			t.Fatalf("stable fact key %q repeats: two declarations on one line collide", env.StableFactKey)
		}
		keys[env.StableFactKey] = true
	}
	got := emitDecoded(t, "package.json", body)
	for i, v := range got {
		if v.LineNumber != 1 || v.OrderIndex != i {
			t.Errorf("fact[%d] line %d order %d, want line 1 order %d", i, v.LineNumber, v.OrderIndex, i)
		}
	}
}

func TestEmitPackageJSONDuplicateKeysLastWins(t *testing.T) {
	t.Parallel()

	body := "{\n\"engines\": {\"node\": \"16\"},\n\"engines\": {\"node\": \"18\", \"node\": \"20\"}\n}\n"
	got := emitDecoded(t, "package.json", body)
	if len(got) != 1 || got[0].RawSpec != "20" || got[0].LineNumber != 3 {
		t.Fatalf("duplicate keys = %+v, want only the last engines.node 20 on line 3 (JSON.parse semantics)", got)
	}
}

// TestEmitPackageJSONLaterNonStringMemberReplacesTheEarlierString proves the
// last-wins rule holds when the later member is not a string: JSON.parse gives
// the number, null, object or array, none of which is a declaration, so the
// earlier string must not survive.
func TestEmitPackageJSONLaterNonStringMemberReplacesTheEarlierString(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"number":         `{"engines":{"node":"18","node":18}}`,
		"null":           `{"engines":{"node":"18","node":null}}`,
		"object":         `{"engines":{"node":"18","node":{"v":"18"}}}`,
		"array":          `{"volta":{"node":"18","node":["18"]}}`,
		"boolean":        `{"volta":{"npm":"10","npm":true}}`,
		"whole section":  `{"engines":{"node":"18"},"engines":"node 18"}`,
		"packageManager": `{"packageManager":"npm@10.0.0","packageManager":10}`,
		"then a string":  `{"engines":{"node":18,"node":"20"}}`,
	} {
		name, body := name, body
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := emitDecoded(t, "package.json", body)
			if name == "then a string" {
				if len(got) != 1 || got[0].RawSpec != "20" {
					t.Fatalf("emitted %+v, want only the later string 20", got)
				}
				return
			}
			if len(got) != 0 {
				t.Fatalf("emitted %+v, want nothing: the later non-string member wins", got)
			}
		})
	}
}

func TestEmitPackageJSONToleratesBOMCommentsAndCRLF(t *testing.T) {
	t.Parallel()

	body := "\xef\xbb\xbf{\r\n// pinned for CI\r\n  \"engines\": { /* node only */ \"node\": \">=18\" }\r\n}\r\n"
	got := emitDecoded(t, "package.json", body)
	if len(got) != 1 || got[0].RawSpec != ">=18" || got[0].LineNumber != 3 {
		t.Fatalf("BOM + comments + CRLF = %+v, want engines.node >=18 on line 3", got)
	}
}

func TestEmitPackageJSONDecodesEscapedKeysAndValues(t *testing.T) {
	t.Parallel()

	// Real backslash escapes: a Go raw string keeps them, and the test checks
	// that the body holds them so the case cannot go vacuous again.
	body := `{"eng` + bs + `u0069nes":{"no` + bs + `u0064e":"` + bs + `u003e=18"}}`
	if !strings.Contains(body, bs+"u0069") || !strings.Contains(body, bs+"u003e") {
		t.Fatalf("body %q holds no escape", body)
	}
	got := emitDecoded(t, "package.json", body)
	if len(got) != 1 || got[0].RawSpec != ">=18" || got[0].Runtime != "node" || got[0].SourceKind != "package_json_engines" {
		t.Fatalf("escaped keys = %+v, want engines.node >=18", got)
	}
}

func TestEmitPackageJSONSkipsStructuralNoise(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty":                  "",
		"whitespace":             " \n\t",
		"array root":             `[{"engines":{"node":"18"}}]`,
		"scalar root":            `"engines"`,
		"no sections":            `{"name":"x","scripts":{"build":"tsc"}}`,
		"engines in a nested":    `{"config":{"engines":{"node":"18"}}}`,
		"volta in dependencies":  `{"dependencies":{"volta":{"node":"18"}}}`,
		"engines not an object":  `{"engines":"node 18"}`,
		"packageManager no @":    `{"packageManager":"pnpm"}`,
		"packageManager empty":   `{"packageManager":""}`,
		"empty engines value":    `{"engines":{"node":""}}`,
		"whitespace-only engine": `{"engines":{"node":"   "}}`,
	}
	for name, body := range cases {
		name, body := name, body
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := emitDecoded(t, "package.json", body); len(got) != 0 {
				t.Fatalf("emitted %+v, want nothing", got)
			}
		})
	}
}

func TestEmitPackageJSONMalformedEmitsNothingAndReportsIt(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"truncated":          `{"engines":{"node":">=18"`,
		"trailing garbage":   `{"engines":{"node":">=18"}} x`,
		"trailing comma":     `{"engines":{"node":">=18",}}`,
		"unquoted key":       `{engines:{"node":">=18"}}`,
		"raw newline":        "{\"engines\":{\"node\":\">=\n18\"}}",
		"bad escape":         `{"engines":{"node":"\q"}}`,
		"bad unicode":        `{"engines":{"node":"\u12"}}`,
		"unterminated cmt":   `{"engines":{"node":">=18"}} /* open`,
		"bad number":         `{"a":01x,"engines":{"node":"18"}}`,
		"bad literal":        `{"a":tru,"engines":{"node":"18"}}`,
		"duplicate colons":   `{"engines"::{"node":"18"}}`,
		"engines before eof": `{"engines":{"node":"18"}`,
	}
	for name, body := range cases {
		name, body := name, body
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			envelopes, skip := EmitFile(testContext("package.json"), "repo-1", "package.json", body)
			if len(envelopes) != 0 {
				t.Fatalf("emitted %d envelopes for malformed input, want 0 (a prefix would pass for the whole file)", len(envelopes))
			}
			if skip.Reason != SkipReasonManifestUnparseable {
				t.Fatalf("skip = %+v, want reason %q", skip, SkipReasonManifestUnparseable)
			}
		})
	}
}

func TestEmitPackageJSONCredentialShapedValuesNeverReachAFact(t *testing.T) {
	t.Parallel()

	// Secret-shaped literals are built by concatenation: GitHub push
	// protection blocks the whole literal in a diff.
	githubToken := "gh" + "p_" + "0123456789abcdefghijklmnopqrstuvwxyzAB"
	awsKey := "AK" + "IA" + "0123456789ABCDEF"
	slackToken := "xo" + "xb-" + "123456789012-abcdefghijklmnopqrstuvwx"
	jwt := "ey" + "JhbGciOiJIUzI1NiJ9.ey" + "JzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N"
	urlWithPassword := "https://deploy:" + "hunter2" + "@registry.example.com/node"
	cases := map[string]string{
		"engines.node github token":    `{"engines":{"node":"` + githubToken + `"}}`,
		"engines.npm aws key":          `{"engines":{"npm":"` + awsKey + `"}}`,
		"volta.node slack token":       `{"volta":{"node":"` + slackToken + `"}}`,
		"volta.yarn jwt":               `{"volta":{"yarn":"` + jwt + `"}}`,
		"engines url with password":    `{"engines":{"node":"` + urlWithPassword + `"}}`,
		"packageManager token spec":    `{"packageManager":"pnpm@` + githubToken + `"}`,
		"packageManager token name":    `{"packageManager":"` + githubToken + `@1.0.0"}`,
		"packageManager token suffix":  `{"packageManager":"pnpm@9.1.0+` + githubToken + `"}`,
		"version with token tail":      `{"engines":{"node":"20.0.0-` + githubToken + `"}}`,
		"range with token clause":      `{"engines":{"node":">=18 ` + githubToken + `"}}`,
		"engines value over the bound": `{"engines":{"node":"` + strings.Repeat("1.", 200) + `"}}`,
	}
	secrets := []string{githubToken, awsKey, slackToken, jwt, "hunter2", "registry.example.com"}
	for name, body := range cases {
		name, body := name, body
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			envelopes, skip := EmitFile(testContext("package.json"), "repo-1", "package.json", body)
			for _, env := range envelopes {
				flat := fmt.Sprint(env.Payload)
				for _, secret := range secrets {
					if strings.Contains(flat, secret) {
						t.Fatalf("payload %s carries the credential-shaped value", flat)
					}
				}
				// The only fact allowed is the packageManager token-suffix case,
				// where the integrity suffix is stripped and the version stays.
				if name != "packageManager token suffix" {
					t.Fatalf("emitted a fact %s for a credential-shaped value", flat)
				}
			}
			if name != "packageManager token suffix" && skip.Reason != SkipReasonValueRejected {
				t.Fatalf("skip = %+v, want reason %q so an operator can see the rejection", skip, SkipReasonValueRejected)
			}
		})
	}
}

func TestEmitPackageJSONRejectionKeepsTheSafeNeighbours(t *testing.T) {
	t.Parallel()

	token := "gh" + "p_" + "0123456789abcdefghijklmnopqrstuvwxyzAB"
	body := `{"engines":{"node":">=18","npm":"` + token + `"},"volta":{"node":"20.11.1"}}`
	envelopes, skip := EmitFile(testContext("package.json"), "repo-1", "package.json", body)
	if len(envelopes) != 2 || skip.Reason != SkipReasonValueRejected || skip.Rejected != 1 {
		t.Fatalf("envelopes = %d, skip = %+v, want the two safe facts and one rejected value", len(envelopes), skip)
	}
}

func TestEmitPackageJSONNestedDependencyDirectoriesEmitNothing(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"node_modules/left-pad/package.json",
		"web/node_modules/@scope/pkg/package.json",
		"vendor/github.com/acme/lib/package.json",
		"app/vendor/x/package.json",
		"bower_components/jquery/package.json",
		"jspm_packages/npm/x@1/package.json",
		"go.mod-vendor/node_modules/a/package.json",
	} {
		if got := emitDecoded(t, path, `{"engines":{"node":">=18"}}`); len(got) != 0 {
			t.Errorf("%s emitted %d facts, want 0: a dependency's own manifest is not the repository's declaration", path, len(got))
		}
	}
	// A directory that merely contains the word is not a dependency directory.
	for _, path := range []string{"my_node_modules_notes/package.json", "vendored/package.json", "packages/vendor-x/package.json"} {
		if got := emitDecoded(t, path, `{"engines":{"node":">=18"}}`); len(got) != 1 {
			t.Errorf("%s emitted %d facts, want 1", path, len(got))
		}
	}
}

func TestEmitPackageJSONOnlyMatchesTheExactFileName(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"package-lock.json", "Package.json", "PACKAGE.JSON", "package.json5", "package.jsonc",
		"packages.json", "composer.json", "tsconfig.json", "app/package.json.bak", ".npmrc",
	} {
		if got := emitDecoded(t, path, `{"engines":{"node":">=18"}}`); len(got) != 0 {
			t.Errorf("%s emitted %d facts, want 0", path, len(got))
		}
	}
}

func TestEmitPackageJSONMonorepoKeepsOneScopeDirPerFile(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"package.json":                  `{"engines":{"node":">=18"}}`,
		"packages/web/package.json":     `{"engines":{"node":"^20"}}`,
		"packages/api/package.json":     `{"volta":{"node":"20.11.1"}}`,
		"packages/api/dev/package.json": `{"packageManager":"pnpm@9.1.0"}`,
	}
	want := map[string]string{
		"package.json": ".", "packages/web/package.json": "packages/web",
		"packages/api/package.json": "packages/api", "packages/api/dev/package.json": "packages/api/dev",
	}
	for path, body := range files {
		got := emitDecoded(t, path, body)
		if len(got) != 1 || got[0].ScopeDir != want[path] || got[0].RelativePath != path {
			t.Errorf("%s = %+v, want one fact with scope_dir %q", path, got, want[path])
		}
	}
}
