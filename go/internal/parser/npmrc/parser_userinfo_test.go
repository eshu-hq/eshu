// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package npmrc

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestParseDropsALineWhoseUserinfoSplitsOutsideTheAuthority covers the forms
// where net/url ends the authority at a ':' followed by '/', '?', or '#', or at
// an all-digit port, so the username or password lands in the host or the path
// and an '@' is left behind. The whole line must produce no row, because no
// field can be trusted to be the registry.
func TestParseDropsALineWhoseUserinfoSplitsOutsideTheAuthority(t *testing.T) {
	t.Parallel()

	const user, pass = "deployUser", "SecretPw0123456789"
	for name, value := range map[string]string{
		"hash after colon":        "https://" + user + ":#" + pass + "@registry.example.com/repository/npm/",
		"semicolon after colon":   "https://" + user + ":;" + pass + "@registry.example.com/repository/npm/",
		"question after colon":    "https://" + user + ":?" + pass + "@registry.example.com/repository/npm/",
		"quoted hash after colon": "\"https://" + user + ":#" + pass + "@registry.example.com/repository/npm/\"",
		"slash after colon":       "https://" + user + ":/" + pass + "@registry.example.com/repository/npm/",
		"digits then slash":       "https://" + user + ":8080/" + pass + "@registry.example.com/repository/npm/",
		"digits then hash":        "https://" + user + ":2024#" + pass + "@registry.example.com/repository/npm/",
		"password with at":        "https://" + user + ":p@" + pass + "/x@registry.example.com/repository/npm/",
		"two at signs":            "https://" + user + ":" + pass + "@mid@registry.example.com/repository/npm/",
		"at in the path":          "https://registry.example.com/repository/" + pass + "@npm/",
		"at in the query":         "https://registry.example.com/repository/npm/?e=" + pass + "@x",
		"at in the comment":       "https://registry.example.com/repository/npm/ ; " + pass + "@x",
		"at in the fragment":      "https://registry.example.com/repository/npm/#" + pass + "@x",
	} {
		payload := parseBody(t, "registry="+value+"\n")
		raw, _ := json.Marshal(payload)
		if rows := rowsOf(t, payload); len(rows) != 0 {
			t.Errorf("%s: rows = %v, want none", name, rows)
		}
		for _, secret := range []string{user, strings.ToLower(user), pass} {
			if strings.Contains(string(raw), secret) {
				t.Errorf("%s: payload holds %q: %s", name, secret, raw)
			}
		}
	}
}

// TestParseKeepsAWellFormedUserinfoLine pins that the new rule does not drop
// the ordinary userinfo forms.
func TestParseKeepsAWellFormedUserinfoLine(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"https://deploy:hunter2secret@registry.example.com/repository/npm/",
		"https://tokenonly@registry.example.com/repository/npm/",
		"https://deploy:@registry.example.com/repository/npm/",
		"https://deploy:hunter2secret@registry.example.com:8443/repository/npm/ ; ci",
		"\"https://deploy:hunter2secret@registry.example.com/repository/npm/\"",
	} {
		rows := rowsOf(t, parseBody(t, "registry="+value+"\n"))
		if len(rows) != 1 || strings.Contains(str(rows[0]["value"]), "@") ||
			strings.Contains(str(rows[0]["value"]), "deploy") || strings.Contains(str(rows[0]["value"]), "tokenonly") {
			t.Errorf("%s: rows = %v, want one row without userinfo", value, rows)
		}
	}
}

// bs is a literal backslash, spelled out so no escape rewriting can change it.
const bs = "\x5c"

// TestParseReadsIniBackslashEscapes follows npm's ini reader: a backslash
// before ';' or '#' makes the character literal, so the value does not end
// there. A '#' stays in the URL and the URL parser removes it as a fragment;
// a ';' stays in the path and the allowlist then withholds the path.
func TestParseReadsIniBackslashEscapes(t *testing.T) {
	t.Parallel()

	rows := rowsOf(t, parseBody(t, "registry=https://registry.example.com/repository/npm/"+bs+"#frag ; comment\n"))
	if len(rows) != 1 || rows[0]["value"] != "https://registry.example.com/repository/npm/" {
		t.Errorf("escaped hash: rows = %v, want the URL without the fragment", rows)
	}
	rows = rowsOf(t, parseBody(t, "registry=https://registry.example.com/repository/a"+bs+";b/\n"))
	if len(rows) != 1 || rows[0]["registry_path"] != "" || rows[0]["registry_path_withheld"] != true ||
		rows[0]["value"] != "https://registry.example.com" {
		t.Errorf("escaped semicolon: rows = %v, want a host-only row, never a truncated path", rows)
	}
	// An unescaped ';' still ends the value.
	rows = rowsOf(t, parseBody(t, "registry=https://registry.example.com/repository/npm/;tail\n"))
	if len(rows) != 1 || rows[0]["value"] != "https://registry.example.com/repository/npm/" {
		t.Errorf("plain semicolon: rows = %v", rows)
	}
}
