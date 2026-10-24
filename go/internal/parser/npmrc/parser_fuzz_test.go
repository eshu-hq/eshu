// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package npmrc

import (
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// fuzzSeed is appended on its own lines in every credential position the
// parser must never read, as the user name, the password, the token, and the
// query of a registry line. The fuzzer mutates the body in front of it, so a
// finding means a mutated prefix made the parser echo a credential value.
const fuzzSeed = "FuzzSeedCredential0123456789"

const fuzzCredentialTail = "\n" +
	"//registry.example.com/:_authToken=" + fuzzSeed + "\n" +
	"//registry.example.com/:_auth=" + fuzzSeed + "\n" +
	"//registry.example.com/:_password=" + fuzzSeed + "\n" +
	"//registry.example.com/:username=" + fuzzSeed + "\n" +
	"email=" + fuzzSeed + "@seed.example\n" +
	"registry=https://" + fuzzSeed + ":" + fuzzSeed + "@registry.example.com/repository/npm/?token=" + fuzzSeed + "#" + fuzzSeed + "\n" +
	// Userinfo that net/url splits outside the authority: the user name or the
	// password lands in the host or the path.
	"registry=https://" + fuzzSeed + ":#" + fuzzSeed + "@registry.example.com/repository/npm/\n" +
	"registry=https://" + fuzzSeed + ":;" + fuzzSeed + "@registry.example.com/repository/npm/\n" +
	"registry=https://" + fuzzSeed + ":?" + fuzzSeed + "@registry.example.com/repository/npm/\n" +
	"registry=https://" + fuzzSeed + ":/" + fuzzSeed + "@registry.example.com/repository/npm/\n" +
	"registry=https://" + fuzzSeed + ":8080/" + fuzzSeed + "@registry.example.com/repository/npm/\n" +
	"registry=https://" + fuzzSeed + ":2024#" + fuzzSeed + "@registry.example.com/repository/npm/\n" +
	"registry=https://u:p@" + fuzzSeed + "/x@registry.example.com/repository/npm/\n" +
	"registry=\"https://" + fuzzSeed + ":#" + fuzzSeed + "@registry.example.com/repository/npm/\"\n" +
	// A token as the first path segment, the Gemfury position, and in a slot.
	"@fury:registry=https://npm-proxy.fury.io/" + fuzzSeed + "/acme/\n" +
	"@slot:registry=https://nexus.example.com/repository/" + fuzzSeed + "/\n"

// FuzzParseNeverEmitsACredential fuzzes the body and asserts the security
// properties of the parser: the seeded credential values never reach the
// payload, no emitted URL field holds an '@', every emitted URL is an absolute
// http or https URL with no userinfo, query, or fragment, and every emitted path
// is on a documented layout for its host.
func FuzzParseNeverEmitsACredential(f *testing.F) {
	for _, seed := range []string{
		"",
		"registry=https://registry.example.com/\n",
		"@acme:registry=https://npm.acme.example/repository/repo/\n",
		"registry=https://u:p@registry.example.com/repository/npm/?t=1#f\n",
		"registry=${NPM_REGISTRY}\n",
		"[section]\nregistry=https://registry.example.com/\n",
		"registry=\"https://registry.example.com/\"\n",
		"registry=https://[::1]:4873/\n",
		"registry=https://user:pass/@registry.example.com/\n",
		"registry=https://registry.example.com/repository/a" + bs + ";b/\n",
		"\xef\xbb\xbfregistry=https://registry.example.com/\r\n",
		seededBody,
		fuzzCredentialTail,
	} {
		f.Add([]byte(seed))
	}
	dir := f.TempDir()
	path := filepath.Join(dir, ".npmrc")
	f.Fuzz(func(t *testing.T, body []byte) {
		if err := os.WriteFile(path, append(append([]byte{}, body...), fuzzCredentialTail...), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		payload, err := Parse(path, false, shared.Options{})
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		// The fuzzed prefix cannot contain the seed unless the fuzzer guessed it.
		if !strings.Contains(string(body), fuzzSeed) && strings.Contains(string(raw), fuzzSeed) {
			t.Fatalf("payload holds the seeded credential: %s", raw)
		}
		rows, _ := payload["variables"].([]map[string]any)
		for _, row := range rows {
			assertCleanRow(t, row)
		}
	})
}

// assertCleanRow checks the invariants of one emitted row against the row's own
// fields. Re-parsing the emitted value alone cannot see a username that became
// the host, so it also checks that no URL field holds an '@'.
func assertCleanRow(t *testing.T, row map[string]any) {
	t.Helper()
	value, _ := row["value"].(string)
	for _, field := range []string{"value", "registry_scheme", "registry_host", "registry_path"} {
		if text, _ := row[field].(string); strings.ContainsAny(text, "@?#") {
			t.Fatalf("row field %s = %q holds an '@', '?', or '#'", field, text)
		}
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		t.Fatalf("row value %q is not a clean absolute registry URL", value)
	}
	hostPort, _ := row["registry_host"].(string)
	hostname := hostPort
	if h, _, splitErr := net.SplitHostPort(hostPort); splitErr == nil {
		hostname = h
	}
	hostname = strings.Trim(hostname, "[]")
	if path, _ := row["registry_path"].(string); path != "" && !registryPathAllowed(hostname, path) {
		t.Fatalf("row path %q on %q is not on a documented layout but was not withheld", path, hostname)
	}
	if row["registry_path_withheld"] == true && row["registry_path"] != "" {
		t.Fatalf("row withheld its path but still carries %q", row["registry_path"])
	}
}
