// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package npmrc

import (
	"encoding/json"
	"strings"
	"testing"
)

// Synthetic tokens in the shapes real hosts issue. None is a real credential.
const (
	hexToken = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b"
	// noDigitToken is 20 bytes over [A-Za-z0-9_-] with no digit, the shape a
	// digit-keyed heuristic misses about 1 in 30 times.
	noDigitToken = "kfybzfYXipnRzKaSNQQb"
	// underscoreToken is a 20-byte token split by an underscore.
	underscoreToken = "NxkAzWomdHq_aHbmDsEM"
	// lowerDashToken is 20 bytes of lower case and a digit split by a dash.
	lowerDashToken = "kfybzfyxipnrz8k-snq7"
)

// TestParseKeepsDocumentedTokenFreeLayouts pins the allowlist: each row is a
// registry layout whose vendor documents the credential in a config key and not
// in the URL path.
func TestParseKeepsDocumentedTokenFreeLayouts(t *testing.T) {
	t.Parallel()

	for _, url := range []string{
		"https://registry.npmjs.org/",
		"https://registry.npmjs.org",
		"https://npm.pkg.github.com/",
		"https://verdaccio.example.com:4873/",
		"https://acme.jfrog.io/artifactory/api/npm/npm-virtual/",
		"https://artifacts.example.com/artifactory/api/npm/npm-virtual",
		"https://nexus.example.com/repository/npm-group/",
		"https://nexus.example.com/nexus/repository/npm-group/",
		"https://pkgs.dev.azure.com/acme/_packaging/feed-name/npm/registry/",
		"https://pkgs.dev.azure.com/acme/platform/_packaging/feed-name/npm/registry/",
		"https://acme.pkgs.visualstudio.com/_packaging/feed-name/npm/registry/",
		"https://gitlab.example.com/api/v4/projects/12345678/packages/npm/",
		"https://gitlab.example.com/api/v4/groups/42/-/packages/npm/",
		"https://gitlab.example.com/api/v4/packages/npm/",
		"https://gitea.example.com/api/packages/acme/npm/",
		"https://npm.cloudsmith.io/acme/platform/",
		"https://us-central1-npm.pkg.dev/acme-project/npm-repo/",
		"https://acme-123456789012.d.codeartifact.us-east-1.amazonaws.com/npm/npm-repo/",
	} {
		rows := rowsOf(t, parseBody(t, "registry="+url+"\n"))
		if len(rows) != 1 || rows[0]["value"] != url && rows[0]["value"] != strings.TrimSuffix(url, "/") {
			t.Errorf("%s: rows = %v, want the URL kept", url, rows)
			continue
		}
		if rows[0]["registry_path_withheld"] != nil {
			t.Errorf("%s: path withheld, want kept", url)
		}
	}
}

// TestParseWithholdsEveryTokenInAPathSlot puts a token in the first path
// segment, the documented Gemfury position, and in each variable slot of each
// allowlisted layout, for several token shapes. Every path is withheld and the
// host stays.
func TestParseWithholdsEveryTokenInAPathSlot(t *testing.T) {
	t.Parallel()

	tokens := []string{
		hexToken, noDigitToken, underscoreToken, lowerDashToken,
		"123e4567-e89b-12d3-a456-426614174000", "T0kEnAbCd12345", "9f86d081884c7d659a2",
	}
	layouts := []struct{ host, pattern string }{
		{"npm-proxy.fury.io", "/%s/acme/"},
		{"npm.fury.io", "/%s/"},
		{"unlisted.example.com", "/%s/acme/"},
		{"unlisted.example.com", "/t/%s/npm/"},
		{"acme.jfrog.io", "/artifactory/api/npm/%s/"},
		{"nexus.example.com", "/repository/%s/"},
		{"nexus.example.com", "/nexus/repository/%s/"},
		{"pkgs.dev.azure.com", "/%s/_packaging/feed/npm/registry/"},
		{"pkgs.dev.azure.com", "/acme/_packaging/%s/npm/registry/"},
		{"pkgs.dev.azure.com", "/acme/%s/_packaging/feed/npm/registry/"},
		{"gitlab.example.com", "/api/v4/projects/%s/packages/npm/"},
		{"gitlab.example.com", "/api/v4/groups/%s/-/packages/npm/"},
		{"gitea.example.com", "/api/packages/%s/npm/"},
		{"npm.cloudsmith.io", "/%s/platform/"},
		{"npm.cloudsmith.io", "/acme/%s/"},
		{"us-central1-npm.pkg.dev", "/%s/npm-repo/"},
		{"acme-123456789012.d.codeartifact.us-east-1.amazonaws.com", "/npm/%s/"},
	}
	for _, layout := range layouts {
		for _, token := range tokens {
			path := strings.Replace(layout.pattern, "%s", token, 1)
			rows := rowsOf(t, parseBody(t, "registry=https://"+layout.host+path+"\n"))
			if len(rows) != 1 {
				t.Errorf("%s%s: rows = %v, want one host-only row", layout.host, path, rows)
				continue
			}
			row := rows[0]
			raw, _ := json.Marshal(row)
			if row["registry_path"] != "" || row["registry_path_withheld"] != true ||
				row["value"] != "https://"+layout.host || strings.Contains(string(raw), token) {
				t.Errorf("%s%s: row = %s, want scheme and host only, path withheld", layout.host, path, raw)
			}
		}
	}
}

// TestParseWithholdsAPathOffTheAllowlist pins that an unknown layout is
// withheld even when it holds no token-shaped segment.
func TestParseWithholdsAPathOffTheAllowlist(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/npm/", "/organizations/acme/npm-releases/", "/artifactory/api/pypi/repo/",
		"/repository/", "/repository/a/b/", "/repository//npm/", "/repository/npm/extra",
		"/repository/npm%2Fgroup/", "/repository/npm=x/",
		"/key=abc", "/npm/%2540tok%253D/", "/password%3Dhunter2secret/", "/AKIAABCDEFGHIJKLMNOP/",
	} {
		rows := rowsOf(t, parseBody(t, "registry=https://registry.example.com"+path+"\n"))
		if len(rows) != 1 || rows[0]["registry_path"] != "" || rows[0]["registry_path_withheld"] != true ||
			rows[0]["value"] != "https://registry.example.com" {
			t.Errorf("path %q: rows = %v, want a host-only row with the path withheld", path, rows)
		}
	}
}
