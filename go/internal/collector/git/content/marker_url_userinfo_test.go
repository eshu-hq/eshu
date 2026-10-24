// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"strings"
	"testing"
	"unicode/utf8"
)

var testURLUserinfoKeys = []string{"npmRegistryServer", "npmPublishRegistry", "httpProxy", "httpsProxy"}

func redactURLForTest(t *testing.T, body string) string {
	t.Helper()
	got, _, err := redactMarkerWithURLKeys(body, testCredentialKeys, testURLUserinfoKeys)
	if err != nil {
		t.Fatalf("redactMarkerWithURLKeys(%q): %v", body, err)
	}
	return got
}

// TestRedactMarkerURLUserinfoKeepsTheHost pins the userinfo-only rule the
// #8812 binding comment on #8833 adds: a URL-valued listed key keeps its scheme,
// host, port, and path, and loses only the userinfo. Anything the rule cannot
// read as exactly one URL is redacted whole, like a credential key.
func TestRedactMarkerURLUserinfoKeepsTheHost(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, in, want string }{
		{"user and password", "npmRegistryServer: \"https://user:pass@npm.example.com/\"\n", "npmRegistryServer: \"https://REDACTED_secret_literal@npm.example.com/\"\n"},
		{"unquoted with port", "httpsProxy: http://u:p@proxy.example.com:8080\n", "httpsProxy: http://REDACTED_secret_literal@proxy.example.com:8080\n"},
		{"token only userinfo", "npmPublishRegistry: 'https://tok3n@registry.example.com/'\n", "npmPublishRegistry: 'https://REDACTED_secret_literal@registry.example.com/'\n"},
		{"raw at sign in password", "httpProxy: \"http://user:p@ss@proxy.example.com\"\n", "httpProxy: \"http://REDACTED_secret_literal@proxy.example.com\"\n"},
		{"nested scope", "npmScopes:\n  acme:\n    npmRegistryServer: \"https://u:p@acme.example.com\"\n", "npmScopes:\n  acme:\n    npmRegistryServer: \"https://REDACTED_secret_literal@acme.example.com\"\n"},
		{"key is case insensitive", "HTTPSPROXY: http://u:p@proxy.example.com\n", "HTTPSPROXY: http://REDACTED_secret_literal@proxy.example.com\n"},
		// No userinfo: the value is kept.
		{"no userinfo", "npmRegistryServer: \"https://registry.example.com/\"\n", "npmRegistryServer: \"https://registry.example.com/\"\n"},
		{"at sign in the path only", "npmRegistryServer: \"https://npm.example.com/@acme/\"\n", "npmRegistryServer: \"https://npm.example.com/@acme/\"\n"},
		{"env reference", "npmRegistryServer: \"${REGISTRY}\"\n", "npmRegistryServer: \"${REGISTRY}\"\n"},
		// Fail closed: anything that is not exactly one readable URL goes whole.
		{"raw slash in password", "httpsProxy: \"http://user:pa/ss@proxy.example.com\"\n", "httpsProxy: \"REDACTED_secret_literal\"\n"},
		{"not a url", "npmRegistryServer: hunter2hunter2\n", "npmRegistryServer: REDACTED_secret_literal\n"},
		{"trailing comment", "npmRegistryServer: \"https://h.example.com/\" # was u:p@h\n", "npmRegistryServer: \"REDACTED_secret_literal\"\n"},
		{"two urls", "httpProxy: http://a.example.com http://u:p@b.example.com\n", "httpProxy: REDACTED_secret_literal\n"},
		{"env default", "npmRegistryServer: \"${REGISTRY:-https://u:p@h.example.com}\"\n", "npmRegistryServer: \"REDACTED_secret_literal\"\n"},
		{"block scalar", "npmRegistryServer: |\n  https://u:p@h.example.com\nx: 1\n", "npmRegistryServer: REDACTED_secret_literal\n  REDACTED_secret_literal\nx: 1\n"},
		{"unterminated quote", "npmRegistryServer: \"https://u:p@h\nmore\"\nx: 1\n", "npmRegistryServer: \"REDACTED_secret_literal\"\nREDACTED_secret_literal\nx: 1\n"},
		// Any other detector match on the line makes the whole value go, with the
		// line's raw kind.
		{"token in the query", "npmRegistryServer: \"https://u:p@h.example.com/?token=abcdefgh\"\n", "npmRegistryServer: \"REDACTED_api_token\"\n"},
		{"ipv6 host", "httpsProxy: \"http://u:p@[::1]:3128\"\n", "httpsProxy: \"http://REDACTED_secret_literal@[::1]:3128\"\n"},
		{"ipv6 host without userinfo", "httpsProxy: \"http://[::1]\"\n", "httpsProxy: \"http://[::1]\"\n"},
		// A flow mapping holding a URL key and a credential key: the credential
		// rule from the leftmost key's separator covers both values.
		{"flow mapping url first", "{npmRegistryServer: \"https://u:p@h.example.com\", npmAuthToken: tok}\n", "{npmRegistryServer: \"REDACTED_api_token\"\n"},
		{"flow mapping token first", "{npmAuthToken: tok, httpsProxy: \"http://u:p@h.example.com\"}\n", "{npmAuthToken: REDACTED_api_token\n"},
		// An earlier marker in the userinfo is kept as it is, whatever kind the
		// rewritten line would now give.
		{"existing userinfo marker", "httpsProxy: http://REDACTED_api_token@h.example.com\n", "httpsProxy: http://REDACTED_api_token@h.example.com\n"},
	}
	for _, tc := range cases {
		if got := redactURLForTest(t, tc.in); got != tc.want {
			t.Errorf("%s:\n got  %q\n want %q", tc.name, got, tc.want)
		}
	}
}

func TestRedactMarkerURLUserinfoIsAFixedPoint(t *testing.T) {
	t.Parallel()

	for _, in := range []string{
		"npmRegistryServer: \"https://user:p@ss@npm.example.com/\"\n",
		"httpsProxy: \"http://user:pa/ss@proxy.example.com\"\n",
		"npmRegistryServer: \"https://u:p@h.example.com/?token=abcdefgh\"\n",
	} {
		once := redactURLForTest(t, in)
		if twice := redactURLForTest(t, once); twice != once {
			t.Errorf("not a fixed point: %q -> %q -> %q", in, once, twice)
		}
		for _, secret := range []string{"user", "p@ss", "pa/ss", "abcdefgh"} {
			if strings.Contains(once, secret) {
				t.Errorf("%q survived in %q", secret, once)
			}
		}
	}
}

// TestRedactMarkerWithoutURLKeysIsUnchanged pins that #8812's redactor keeps
// its behavior when a definition lists no URL keys: a URL key is an ordinary
// unlisted key, and redactMarker is the same function with no URL keys.
func TestRedactMarkerWithoutURLKeysIsUnchanged(t *testing.T) {
	t.Parallel()

	in := "npmRegistryServer: \"https://user:pass@npm.example.com/\"\nnpmAuthToken: abc\n"
	plain, _, err := redactMarker(in, testCredentialKeys)
	if err != nil {
		t.Fatal(err)
	}
	none, _, err := redactMarkerWithURLKeys(in, testCredentialKeys, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plain != none || plain != "npmRegistryServer: \"https://user:pass@npm.example.com/\"\nnpmAuthToken: REDACTED_api_token\n" {
		t.Fatalf("redactMarker = %q, with nil URL keys = %q", plain, none)
	}
}

// FuzzRedactMarkerURLKeys checks the marker invariants with URL keys listed:
// the line count is kept, a second pass is a no-op, and planted userinfo under
// a URL key never survives.
func FuzzRedactMarkerURLKeys(f *testing.F) {
	for _, s := range []string{
		"npmRegistryServer: \"https://u:p@h/\"\n", "httpsProxy: http://u:p@[::1]:3128\n", "httpProxy: |\n  x\n",
		"npmRegistryServer: 'https://a@b@c/'\n", "{httpsProxy: http://u:p@h, npmAuthToken: t}\n", "npmPublishRegistry: \"x\n",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out, _, err := redactMarkerWithURLKeys(in, testCredentialKeys, testURLUserinfoKeys)
		if err != nil {
			if utf8.ValidString(in) && !strings.Contains(in, "\x00") {
				t.Fatalf("unexpected err %v", err)
			}
			return
		}
		if strings.Count(out, "\n") != strings.Count(in, "\n") {
			t.Fatalf("line count changed %q -> %q", in, out)
		}
		if again, _, _ := redactMarkerWithURLKeys(out, testCredentialKeys, testURLUserinfoKeys); again != out {
			t.Fatalf("not a fixed point:\n in   %q\n out  %q\n out2 %q", in, out, again)
		}
		for _, planted := range []string{
			in + "\nnpmRegistryServer: \"https://svc:QqZzSeed9988@h.example.com/\"\n",
			in + "\nhttpsProxy: http://QqZzSeed9988@proxy.example.com:8080\n",
			in + "\nhttpProxy: \"http://svc:QqZz/Seed9988@h\"\n",
		} {
			po, _, perr := redactMarkerWithURLKeys(planted, testCredentialKeys, testURLUserinfoKeys)
			if perr == nil && (strings.Contains(po, "QqZzSeed9988") || strings.Contains(po, "Seed9988")) {
				t.Fatalf("planted userinfo survived: planted=%q out=%q", planted, po)
			}
		}
	})
}
