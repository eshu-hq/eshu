// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	runtimedeclarationv1 "github.com/eshu-hq/eshu/sdk/go/factschema/runtimedeclaration/v1"
)

func testContext(path string) FixtureContext {
	return FixtureContext{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		ObservedAt:   time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC),
		SourceURI:    path,
	}
}

// emitDecoded emits one file and decodes every envelope through the typed
// contract seam, so a test sees exactly what a reducer would.
func emitDecoded(t *testing.T, path, body string) []runtimedeclarationv1.Version {
	t.Helper()
	envelopes := Emit(testContext(path), "repo-1", path, body)
	out := make([]runtimedeclarationv1.Version, 0, len(envelopes))
	for i, env := range envelopes {
		if env.FactKind != facts.RuntimeDeclarationVersionFactKind {
			t.Fatalf("envelope[%d].FactKind = %q, want %q", i, env.FactKind, facts.RuntimeDeclarationVersionFactKind)
		}
		if env.SchemaVersion != facts.RuntimeDeclarationSchemaVersionV1 {
			t.Fatalf("envelope[%d].SchemaVersion = %q", i, env.SchemaVersion)
		}
		decoded, err := factschema.DecodeRuntimeDeclarationVersion(factschema.Envelope{
			FactKind: env.FactKind, SchemaVersion: env.SchemaVersion, Payload: env.Payload,
		})
		if err != nil {
			t.Fatalf("envelope[%d] does not decode through the contract: %v", i, err)
		}
		out = append(out, decoded)
	}
	return out
}

func deref(p *int) int {
	if p == nil {
		return -1
	}
	return *p
}

func derefStr(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

type wantFact struct {
	scopeDir, sourceKind, runtime, tool, raw, specKind, dist string
	line, order, major, minor, patch                         int
}

func checkFacts(t *testing.T, got []runtimedeclarationv1.Version, want []wantFact) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("emitted %d facts, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.RepoID != "repo-1" || g.ScopeDir != w.scopeDir || g.SourceKind != w.sourceKind ||
			g.Runtime != w.runtime || derefStr(g.ToolName) != w.tool || g.RawSpec != w.raw ||
			g.SpecKind != w.specKind || derefStr(g.Distribution) != w.dist ||
			g.LineNumber != w.line || g.OrderIndex != w.order ||
			deref(g.VersionMajor) != w.major || deref(g.VersionMinor) != w.minor || deref(g.VersionPatch) != w.patch {
			t.Errorf("fact[%d] = %+v\n  (major=%d minor=%d patch=%d tool=%s dist=%s)\nwant %+v",
				i, g, deref(g.VersionMajor), deref(g.VersionMinor), deref(g.VersionPatch),
				derefStr(g.ToolName), derefStr(g.Distribution), w)
		}
	}
}

func TestEmitSingleVersionFiles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, path, body string
		want             []wantFact
	}{
		{
			"nvmrc in a monorepo package", "services/api/.nvmrc", "v20.11.1\n",
			[]wantFact{{"services/api", "nvmrc", "node", "<nil>", "v20.11.1", "exact", "<nil>", 1, 0, 20, 11, 1}},
		},
		{
			"nvmrc at the repo root uses a dot scope", ".nvmrc", "20\n",
			[]wantFact{{".", "nvmrc", "node", "<nil>", "20", "partial", "<nil>", 1, 0, 20, -1, -1}},
		},
		{
			"node-version alias is never resolved", "web/.node-version", "lts/iron\n",
			[]wantFact{{"web", "node_version", "node", "<nil>", "lts/iron", "alias", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"unexpanded placeholder is unresolved", ".nvmrc", "${NODE_VERSION}\n",
			[]wantFact{{".", "nvmrc", "node", "<nil>", "${NODE_VERSION}", "unresolved", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"comment lines, a BOM and CRLF are skipped", ".nvmrc", "\xef\xbb\xbf# pinned\r\n\r\n18.19.0 # lts\r\n",
			[]wantFact{{".", "nvmrc", "node", "<nil>", "18.19.0", "exact", "<nil>", 3, 0, 18, 19, 0}},
		},
		{
			"only the first effective line of a ruby pin counts", ".ruby-version", "ruby-3.2.2\n3.1.0\n",
			[]wantFact{{".", "ruby_version", "ruby", "<nil>", "ruby-3.2.2", "exact", "<nil>", 1, 0, 3, 2, 2}},
		},
		{
			"every line of a python pin counts", ".python-version", "3.11.4\n3.10.2\n",
			[]wantFact{
				{".", "python_version", "python", "<nil>", "3.11.4", "exact", "<nil>", 1, 0, 3, 11, 4},
				{".", "python_version", "python", "<nil>", "3.10.2", "exact", "<nil>", 2, 0, 3, 10, 2},
			},
		},
		{
			"go prefix is dropped from parsed parts only", ".go-version", "go1.22.1\n",
			[]wantFact{{".", "go_version", "go", "<nil>", "go1.22.1", "exact", "<nil>", 1, 0, 1, 22, 1}},
		},
		{
			"java and terraform pins", "infra/.terraform-version", "latest:^1.5\n",
			[]wantFact{{"infra", "terraform_version", "terraform", "<nil>", "latest:^1.5", "alias", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"java pin", ".java-version", "17\n",
			[]wantFact{{".", "java_version", "java", "<nil>", "17", "partial", "<nil>", 1, 0, 17, -1, -1}},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkFacts(t, emitDecoded(t, tc.path, tc.body), tc.want)
		})
	}
}

func TestEmitToolVersionsOneFactPerLineAndToken(t *testing.T) {
	t.Parallel()

	body := "# managed by asdf\nnodejs 20.1.0 18.0.0\nruby 3.2 system\ngolang 1.22.1\npython 3.12\nrust stable\n"
	got := emitDecoded(t, "app/.tool-versions", body)
	checkFacts(t, got, []wantFact{
		{"app", "tool_versions", "node", "nodejs", "20.1.0", "exact", "<nil>", 2, 0, 20, 1, 0},
		{"app", "tool_versions", "node", "nodejs", "18.0.0", "exact", "<nil>", 2, 1, 18, 0, 0},
		{"app", "tool_versions", "ruby", "<nil>", "3.2", "partial", "<nil>", 3, 0, 3, 2, -1},
		{"app", "tool_versions", "ruby", "<nil>", "system", "alias", "<nil>", 3, 1, -1, -1, -1},
		{"app", "tool_versions", "go", "golang", "1.22.1", "exact", "<nil>", 4, 0, 1, 22, 1},
		{"app", "tool_versions", "python", "<nil>", "3.12", "partial", "<nil>", 5, 0, 3, 12, -1},
		{"app", "tool_versions", "rust", "<nil>", "stable", "alias", "<nil>", 6, 0, -1, -1, -1},
	})
}

func TestEmitSdkmanrcCarriesDistribution(t *testing.T) {
	t.Parallel()

	got := emitDecoded(t, ".sdkmanrc", "# sdkman\njava=17.0.2-tem\ngradle=8.5\n")
	checkFacts(t, got, []wantFact{
		{".", "sdkmanrc", "java", "<nil>", "17.0.2-tem", "exact", "tem", 2, 0, 17, 0, 2},
		{".", "sdkmanrc", "gradle", "<nil>", "8.5", "partial", "<nil>", 3, 0, 8, 5, -1},
	})
}

func TestEmitRustToolchain(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, path, body string
		want             []wantFact
	}{
		{
			"legacy plain channel", "rust-toolchain", "nightly-2024-01-01\n",
			[]wantFact{{".", "rust_toolchain", "rust", "<nil>", "nightly-2024-01-01", "alias", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"legacy plain numeric release", "rust-toolchain", "1.75.0\n",
			[]wantFact{{".", "rust_toolchain", "rust", "<nil>", "1.75.0", "exact", "<nil>", 1, 0, 1, 75, 0}},
		},
		{
			"toml channel on its own line", "crates/core/rust-toolchain.toml",
			"[toolchain]\ncomponents = [\"clippy\"]\nchannel = \"1.80\"\n",
			[]wantFact{{"crates/core", "rust_toolchain", "rust", "<nil>", "1.80", "partial", "<nil>", 3, 0, 1, 80, -1}},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkFacts(t, emitDecoded(t, tc.path, tc.body), tc.want)
		})
	}
}

// TestEmitEmitsNothingWithoutADeclaration proves empty and comment-only pin
// files, a toml file with no channel, and a credential carrier produce no
// fact.
func TestEmitEmitsNothingWithoutADeclaration(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, path, body string }{
		{"empty nvmrc", ".nvmrc", ""},
		{"comment only", ".tool-versions", "# nothing pinned\n\n"},
		{"tool name without versions", ".tool-versions", "nodejs\n"},
		{"toml without channel", "rust-toolchain.toml", "[toolchain]\ncomponents = [\"clippy\"]\n"},
		{"npmrc is a credential carrier", ".npmrc", "//registry.example.com/:_authToken=secret\nengine-strict=true\n"},
		{"env file", ".env", "NODE_VERSION=20\n"},
		{"similar but unadmitted name", "node-version", "20\n"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Emit(testContext(tc.path), "repo-1", tc.path, tc.body); len(got) != 0 {
				t.Fatalf("Emit(%q) = %d envelopes, want none", tc.path, len(got))
			}
		})
	}
}

func TestSourceKindForPathCoversEveryAdmittedName(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		".nvmrc": "nvmrc", ".node-version": "node_version", ".python-version": "python_version",
		".ruby-version": "ruby_version", ".java-version": "java_version", ".go-version": "go_version",
		".terraform-version": "terraform_version", ".tool-versions": "tool_versions", ".sdkmanrc": "sdkmanrc",
		"rust-toolchain": "rust_toolchain", "rust-toolchain.toml": "rust_toolchain",
	}
	for name, kind := range want {
		for _, path := range []string{name, "a/b/" + name} {
			got, ok := SourceKindForPath(path)
			if !ok || got != kind {
				t.Errorf("SourceKindForPath(%q) = %q, %v, want %q, true", path, got, ok, kind)
			}
		}
	}
	for _, name := range []string{".npmrc", ".yarnrc.yml", ".pypirc", ".netrc", "mise.toml", ".env", ".nvmrc.bak", "nvmrc"} {
		if _, ok := SourceKindForPath(name); ok {
			t.Errorf("SourceKindForPath(%q) admitted a name outside the pin list", name)
		}
	}
}

// TestEmitStableKeysAreDistinctAndDeterministic proves two tokens on one line
// get distinct keys and a re-emit of the same file reproduces them.
func TestEmitStableKeysAreDistinctAndDeterministic(t *testing.T) {
	t.Parallel()

	body := "nodejs 20.1.0 18.0.0\n"
	first := Emit(testContext(".tool-versions"), "repo-1", ".tool-versions", body)
	second := Emit(testContext(".tool-versions"), "repo-1", ".tool-versions", body)
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("emitted %d and %d envelopes, want 2 and 2", len(first), len(second))
	}
	if first[0].StableFactKey == first[1].StableFactKey {
		t.Fatalf("tokens on one line share stable key %q", first[0].StableFactKey)
	}
	for i := range first {
		if first[i].StableFactKey != second[i].StableFactKey || first[i].FactID != second[i].FactID {
			t.Fatalf("re-emit changed identity of envelope %d", i)
		}
	}
}
