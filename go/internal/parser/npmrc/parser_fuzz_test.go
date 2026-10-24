// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package npmrc

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// fuzzSeed is appended on its own lines in every credential position the
// parser must never read. The fuzzer mutates the body in front of it, so a
// finding means a mutated prefix made the parser echo a credential value.
const fuzzSeed = "FuzzSeedCredential0123456789"

const fuzzCredentialTail = "\n" +
	"//registry.example.com/:_authToken=" + fuzzSeed + "\n" +
	"//registry.example.com/:_auth=" + fuzzSeed + "\n" +
	"//registry.example.com/:_password=" + fuzzSeed + "\n" +
	"//registry.example.com/:username=" + fuzzSeed + "\n" +
	"email=" + fuzzSeed + "@seed.example\n" +
	"registry=https://" + fuzzSeed + ":" + fuzzSeed + "@registry.example.com/npm/?token=" + fuzzSeed + "#" + fuzzSeed + "\n"

// FuzzParseNeverEmitsACredential fuzzes the body and asserts the two security
// properties of the parser: the seeded credential values never reach the
// payload, and every emitted URL is an absolute http or https URL with no
// userinfo, query, or fragment.
func FuzzParseNeverEmitsACredential(f *testing.F) {
	for _, seed := range []string{
		"",
		"registry=https://registry.example.com/\n",
		"@acme:registry=https://npm.acme.example/repo/\n",
		"registry=https://u:p@registry.example.com/npm/?t=1#f\n",
		"registry=${NPM_REGISTRY}\n",
		"[section]\nregistry=https://registry.example.com/\n",
		"registry=\"https://registry.example.com/\"\n",
		"registry=https://[::1]:4873/\n",
		"registry=https://user:pass/@registry.example.com/\n",
		"\xef\xbb\xbfregistry=https://registry.example.com/\r\n",
		seededBody,
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
			value, _ := row["value"].(string)
			parsed, perr := url.Parse(value)
			if perr != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
				(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
				t.Fatalf("row value %q is not a clean absolute registry URL", value)
			}
			if strings.ContainsAny(value, "?#") {
				t.Fatalf("row value %q holds a query or fragment marker", value)
			}
		}
	})
}
