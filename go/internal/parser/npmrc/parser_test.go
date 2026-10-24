// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package npmrc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func writeNpmrc(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".npmrc")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write .npmrc: %v", err)
	}
	return path
}

func parseBody(t *testing.T, body string) map[string]any {
	t.Helper()
	payload, err := Parse(writeNpmrc(t, body), false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return payload
}

func rowsOf(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	return rows
}

type wantRow struct {
	name, value, scheme, host, path, scope string
	line                                   int
}

func TestParseEmitsRegistryRows(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want []wantRow
	}{
		{
			name: "default registry",
			body: "registry=https://registry.example.com/npm/\n",
			want: []wantRow{{"registry", "https://registry.example.com/npm/", "https", "registry.example.com", "/npm/", "", 1}},
		},
		{
			name: "scoped registry beside default",
			body: "registry=https://registry.example.com/\n@acme:registry=https://npm.acme.example/repo/\n",
			want: []wantRow{
				{"registry", "https://registry.example.com/", "https", "registry.example.com", "/", "", 1},
				{"@acme:registry", "https://npm.acme.example/repo/", "https", "npm.acme.example", "/repo/", "@acme", 2},
			},
		},
		{
			name: "spaces around the separator",
			body: "  registry = https://registry.example.com/npm/  \n",
			want: []wantRow{{"registry", "https://registry.example.com/npm/", "https", "registry.example.com", "/npm/", "", 1}},
		},
		{
			name: "userinfo is removed",
			body: "registry=https://deploy:hunter2secret@registry.example.com:8443/npm/\n",
			want: []wantRow{{"registry", "https://registry.example.com:8443/npm/", "https", "registry.example.com:8443", "/npm/", "", 1}},
		},
		{
			name: "user only userinfo is removed",
			body: "registry=https://tokenonly@registry.example.com/npm/\n",
			want: []wantRow{{"registry", "https://registry.example.com/npm/", "https", "registry.example.com", "/npm/", "", 1}},
		},
		{
			name: "query and fragment are removed",
			body: "registry=https://registry.example.com/npm/?token=abc123def456#frag\n",
			want: []wantRow{{"registry", "https://registry.example.com/npm/", "https", "registry.example.com", "/npm/", "", 1}},
		},
		{
			name: "host is lower cased and http is kept",
			body: "registry=HTTP://Registry.Example.COM/Npm\n",
			want: []wantRow{{"registry", "http://registry.example.com/Npm", "http", "registry.example.com", "/Npm", "", 1}},
		},
		{
			name: "quoted value",
			body: "registry=\"https://registry.example.com/npm/\"\n",
			want: []wantRow{{"registry", "https://registry.example.com/npm/", "https", "registry.example.com", "/npm/", "", 1}},
		},
		{
			name: "inline comment ends the value",
			body: "registry=https://registry.example.com/npm/ ; mirror\n",
			want: []wantRow{{"registry", "https://registry.example.com/npm/", "https", "registry.example.com", "/npm/", "", 1}},
		},
		{
			name: "crlf and a byte order mark",
			body: "\xef\xbb\xbfregistry=https://registry.example.com/npm/\r\n@acme:registry=https://npm.acme.example/\r\n",
			want: []wantRow{
				{"registry", "https://registry.example.com/npm/", "https", "registry.example.com", "/npm/", "", 1},
				{"@acme:registry", "https://npm.acme.example/", "https", "npm.acme.example", "/", "@acme", 2},
			},
		},
		{
			name: "ipv6 host",
			body: "registry=https://[2001:db8::1]:4873/\n",
			want: []wantRow{{"registry", "https://[2001:db8::1]:4873/", "https", "[2001:db8::1]:4873", "/", "", 1}},
		},
		{
			name: "a repeated key keeps every line",
			body: "registry=https://a.example.com/\nregistry=https://b.example.com/\n",
			want: []wantRow{
				{"registry", "https://a.example.com/", "https", "a.example.com", "/", "", 1},
				{"registry", "https://b.example.com/", "https", "b.example.com", "/", "", 2},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := rowsOf(t, parseBody(t, tc.body))
			if len(rows) != len(tc.want) {
				t.Fatalf("rows = %d (%v), want %d", len(rows), rows, len(tc.want))
			}
			for i, want := range tc.want {
				row := rows[i]
				got := wantRow{
					name: str(row["name"]), value: str(row["value"]), scheme: str(row["registry_scheme"]),
					host: str(row["registry_host"]), path: str(row["registry_path"]), scope: str(row["registry_scope"]),
					line: row["line_number"].(int),
				}
				if got != want {
					t.Errorf("row %d = %+v, want %+v", i, got, want)
				}
				if row["config_kind"] != "registry_url" || row["package_manager"] != "npm" ||
					row["lang"] != "npmrc" || row["lockfile"] != false {
					t.Errorf("row %d metadata = %v", i, row)
				}
			}
		})
	}
}

func TestParseEmitsNothingForLinesThatAreNotARegistryURL(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"empty file":              "",
		"comments only":           "# registry=https://registry.example.com/\n; registry=https://registry.example.com/\n",
		"env expansion":           "registry=${NPM_REGISTRY}\n",
		"env expansion in host":   "registry=https://${NPM_HOST}/npm/\n",
		"no scheme":               "registry=registry.example.com/npm/\n",
		"scheme relative":         "registry=//registry.example.com/npm/\n",
		"file scheme":             "registry=file:///srv/npm/\n",
		"javascript scheme":       "registry=javascript:alert(1)\n",
		"empty value":             "registry=\n",
		"no host":                 "registry=https:///npm/\n",
		"bad port":                "registry=https://registry.example.com:notaport/\n",
		"space in host":           "registry=https://reg istry.example.com/\n",
		"section scoped key":      "[extra]\nregistry=https://registry.example.com/\n",
		"array key":               "registry[]=https://registry.example.com/\n",
		"key without separator":   "registry https://registry.example.com/\n",
		"other key names":         "registry-url=https://registry.example.com/\nmyregistry=https://registry.example.com/\n",
		"bare scope key":          "@:registry=https://registry.example.com/\n",
		"scope with bad name":     "@a b:registry=https://registry.example.com/\n",
		"credential keys only":    "//registry.example.com/:_authToken=x\n_auth=y\n_password=z\nemail=a@b.example\n",
		"userinfo with bad host":  "registry=https://user:pass@/npm/\n",
		"slash in userinfo":       "registry=https://user:pass/@registry.example.com/\n",
		"overlong line":           "registry=https://registry.example.com/" + strings.Repeat("a", maxLineBytes) + "\n",
		"nul byte":                "registry=https://registry.example.com/\x00npm/\n",
		"control byte in value":   "registry=https://registry.example.com/\x01npm/\n",
		"non-ascii host":          "registry=https://regiéstry.example.com/\n",
		"unicode space separator": "registry =https://registry.example.com/\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if rows := rowsOf(t, parseBody(t, body)); len(rows) != 0 {
				t.Fatalf("rows = %v, want none", rows)
			}
		})
	}
}

// TestParseKeepsLineNumbersAcrossSkippedLines pins that line_number is the
// source line, counting blank, comment, and credential lines.
func TestParseKeepsLineNumbersAcrossSkippedLines(t *testing.T) {
	t.Parallel()

	rows := rowsOf(t, parseBody(t, "# header\n\n//registry.example.com/:_authToken=skipme\nregistry=https://registry.example.com/\n"))
	if len(rows) != 1 || rows[0]["line_number"] != 4 {
		t.Fatalf("rows = %v, want one row on line 4", rows)
	}
}

func TestParseNeverEmitsSource(t *testing.T) {
	t.Parallel()

	path := writeNpmrc(t, "registry=https://registry.example.com/\n")
	payload, err := Parse(path, false, shared.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if _, has := payload["source"]; has {
		t.Fatal("payload carries source; .npmrc bodies must never be echoed")
	}
}

func TestParseReportsAMissingFile(t *testing.T) {
	t.Parallel()

	if _, err := Parse(filepath.Join(t.TempDir(), ".npmrc"), false, shared.Options{}); err == nil {
		t.Fatal("Parse() of a missing file error = nil, want error")
	}
}

func TestParseIsDependencyFlagAndBaseShape(t *testing.T) {
	t.Parallel()

	path := writeNpmrc(t, "registry=https://registry.example.com/\n")
	payload, err := Parse(path, true, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if payload["is_dependency"] != true || payload["lang"] != "npmrc" || payload["path"] != path {
		t.Fatalf("payload base = %v", payload)
	}
	for _, bucket := range []string{"functions", "classes", "imports", "function_calls"} {
		if _, ok := payload[bucket].([]map[string]any); !ok {
			t.Errorf("%s = %T, want []map[string]any", bucket, payload[bucket])
		}
	}
}

// credentialSeeds are values that must reach no part of the payload.
var credentialSeeds = []string{
	"SeedAuthToken0123456789abcdef",
	"SeedAuthBase64Value0123456789",
	"SeedPasswordValue0123456789",
	"SeedUsernameValue0123456789",
	"seed.email.value@seed-mail.example",
	"SeedUserinfoPass0123456789",
	"SeedQueryToken0123456789",
	"SeedFragmentToken0123456789",
	"SeedAltUser0123456789",
}

const seededBody = "" +
	"//registry.example.com/:_authToken=SeedAuthToken0123456789abcdef\n" +
	"//registry.example.com/:_auth=SeedAuthBase64Value0123456789\n" +
	"//registry.example.com/:_password=SeedPasswordValue0123456789\n" +
	"//registry.example.com/:username=SeedUsernameValue0123456789\n" +
	"//registry.example.com/:email=seed.email.value@seed-mail.example\n" +
	"_authToken=SeedAuthToken0123456789abcdef\n" +
	"always-auth=true\n" +
	"registry=https://SeedAltUser0123456789:SeedUserinfoPass0123456789@registry.example.com/npm/?token=SeedQueryToken0123456789#SeedFragmentToken0123456789\n" +
	"@acme:registry=https://npm.acme.example/\n"

// TestParseNeverEmitsACredentialValue is the seeded-credential scan for the
// parser: every credential line and every credential-bearing part of a
// registry line is seeded, and no seed may appear anywhere in the payload.
func TestParseNeverEmitsACredentialValue(t *testing.T) {
	t.Parallel()

	payload := parseBody(t, seededBody)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	for _, seed := range credentialSeeds {
		if strings.Contains(string(raw), seed) {
			t.Errorf("payload holds seed %q: %s", seed, raw)
		}
	}
	rows := rowsOf(t, payload)
	if len(rows) != 2 {
		t.Fatalf("rows = %v, want the two registry lines only", rows)
	}
	for _, key := range []string{"_authToken", "_auth", "_password", "username", "email", "always-auth"} {
		if strings.Contains(string(raw), key) {
			t.Errorf("payload names credential key %q: %s", key, raw)
		}
	}
}

func str(value any) string {
	text, _ := value.(string)
	return text
}

// TestParseKeepsOrdinaryRegistryPaths pins that the path-token guard does not
// withhold the registry paths real hosts use.
func TestParseKeepsOrdinaryRegistryPaths(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/", "/npm/", "/artifactory/api/npm/npm-virtual/", "/repository/npm-group/",
		"/org/project/_packaging/feed-name/npm/registry/", "/api/v4/projects/12345678/packages/npm/",
		"/organizations/acme-corp-platform/npm-releases/", "/nexus/content/groups/npm-all-internal-mirror/",
		"/acme/npm-virtual-prod-us-east-1-mirror/",
	} {
		rows := rowsOf(t, parseBody(t, "registry=https://registry.example.com"+path+"\n"))
		if len(rows) != 1 || rows[0]["registry_path"] != path || rows[0]["registry_path_withheld"] != nil {
			t.Errorf("path %q: rows = %v, want the path kept", path, rows)
		}
	}
}

// TestParseWithholdsATokenShapedPath pins the security-review finding: a
// credential placed as a URL path segment (no key, no known value shape) must
// not be emitted. The row keeps scheme and host, drops the path, and says so.
func TestParseWithholdsATokenShapedPath(t *testing.T) {
	t.Parallel()

	const hexToken = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b"
	const b64Token = "Zk93Qm1xV2RkS2pQeUxhTnVfWmQ4LXJFcTRz"
	for name, path := range map[string]string{
		"hex token":         "/t/" + hexToken + "/npm/",
		"base64url token":   "/" + b64Token + "/",
		"uuid":              "/feeds/123e4567-e89b-12d3-a456-426614174000/npm/",
		"key value segment": "/npm/apikey=abc",
		"colon segment":     "/npm/user:pw/",
		"percent segment":   "/npm/%2540tok%253D/",
		"encoded token":     "/%5A%6B%39%33%51%6D%31%78%56%32%52%6B%53%32%70%51%65%55%78%68/",
		"encoded detector":  "/password%3Dhunter2secret/",
		"detector shape":    "/AKIAABCDEFGHIJKLMNOP/",
		"detector key":      "/npm/password=hunter2secret",
		"slack shape":       "/xoxb-1234567890-abcdefghij/",
	} {
		rows := rowsOf(t, parseBody(t, "registry=https://registry.example.com"+path+"\n"))
		if len(rows) != 1 {
			t.Errorf("%s: rows = %v, want one host-only row", name, rows)
			continue
		}
		row := rows[0]
		if row["registry_path"] != "" || row["registry_path_withheld"] != true ||
			row["value"] != "https://registry.example.com" || row["registry_host"] != "registry.example.com" {
			t.Errorf("%s: row = %v, want scheme and host only with registry_path_withheld", name, row)
		}
		raw, _ := json.Marshal(row)
		for _, secret := range []string{hexToken, b64Token, "hunter2secret", "123e4567", "AKIAABCDEFGHIJKLMNOP", "xoxb-1234567890", "Zk93Qm1x"} {
			if strings.Contains(string(raw), secret) {
				t.Errorf("%s: row holds %q: %s", name, secret, raw)
			}
		}
	}
}
