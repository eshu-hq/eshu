// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"errors"
	"io"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var oracleURLKeys = []string{"npmRegistryServer", "httpsProxy"}

// oracleTokenPattern finds the numbered tokens the generator plants.
var oracleTokenPattern = regexp.MustCompile(regexp.QuoteMeta(flowSeedToken) + `\d*`)

// yamlTokensUnderListedKey is the independent oracle for the #8833 flow hole:
// it parses body with yaml.v3 and returns every token that is a scalar (or a
// key) anywhere in the value of a listed credential or URL key. It says nothing
// about how the redactor reads the lines, so a redactor that stores such a
// body with one of those tokens in it is wrong whatever its own state machine
// believes. parsed is false when the body is not valid YAML.
func yamlTokensUnderListedKey(body string, listed []string) (tokens map[string]bool, parsed bool) {
	tokens = map[string]bool{}
	decoder := yaml.NewDecoder(strings.NewReader(body))
	for {
		var document yaml.Node
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			return tokens, true
		}
		if err != nil {
			return map[string]bool{}, false
		}
		collectTokens(&document, listed, false, 0, tokens)
	}
}

func collectTokens(node *yaml.Node, listed []string, inside bool, depth int, tokens map[string]bool) {
	if node == nil || depth > 64 {
		return
	}
	switch node.Kind {
	case yaml.AliasNode:
		collectTokens(node.Alias, listed, inside, depth+1, tokens)
	case yaml.ScalarNode:
		if inside {
			for _, token := range oracleTokenPattern.FindAllString(node.Value, -1) {
				tokens[token] = true
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			collectTokens(key, listed, inside, depth+1, tokens)
			collectTokens(value, listed, inside || isListedKey(key.Value, listed), depth+1, tokens)
		}
	default:
		for _, child := range node.Content {
			collectTokens(child, listed, inside, depth+1, tokens)
		}
	}
}

func envInt(name string, fallback int) int {
	if value, err := strconv.Atoi(os.Getenv(name)); err == nil && value > 0 {
		return value
	}
	return fallback
}

// TestRedactMarkerFlowHostileSeedsDecodeTheToken keeps the seed table honest:
// every body yaml.v3 can parse must hold the token under a listed key, so a seed
// cannot pass for being no leak at all.
func TestRedactMarkerFlowHostileSeedsDecodeTheToken(t *testing.T) {
	t.Parallel()

	listed := append(append([]string{}, yarnrcCredentialKeys...), oracleURLKeys...)
	for name, template := range flowHostileSeeds {
		tokens, parsed := yamlTokensUnderListedKey(flowSeedBody(template), listed)
		if parsed && len(tokens) == 0 {
			t.Errorf("%s: parses but holds no token under a listed key", name)
		}
	}
}

// oracleFragments are the lines the generator joins. They mix flow and block
// openers, listed keys with and without inline values, scalars at every
// indent, comments, blank lines, document markers, tabs, and closers.
var oracleFragments = []string{
	"{", "}", "[", "]", "},", "]}", "{}", "---", "...", "# c", "", "  # c",
	"npmAuthToken:", "npmAuthToken: abcd", "npmAuthToken: |", "npmAuthToken: >-", "npmAuthToken: &a", "npmAuthToken: !!str",
	"npmAuthToken: # c", "npmAuthToken: [", "npmAuthToken: {", "npmAuthToken: \"abcd", "npmAuthToken: 'abcd",
	"  npmAuthToken:", "    npmAuthToken: abcd", "\"npmAuthToken\":", "  \"npmAuthToken\":", "- npmAuthToken:", "  - npmAuthToken:",
	"{npmAuthToken:", "{npmAuthIdent: abcd", "acme: {npmAuthToken:", "  acme: {npmAuthToken:", "{\"npmAuthToken\":", "[{npmAuthToken:",
	"npmScopes:", "  acme:", "    acme:", "npmScopes: {", "npmScopes: {acme: {", "- {npmAuthIdent:", "npmRegistryServer:",
	"{npmRegistryServer:", "httpsProxy: abcd", "  httpsProxy:",
	"{T}", " {T}", "  {T}", "    {T}", "\t{T}", "\"{T}\"", "  \"{T}\"", "{T}}", " {T}}", "{T},", "{T}\"", "{T}'", "{T}]",
	"- {T}", "  - {T}", "-\t{T}", "- \"{T}\"", "a: {T}", "  a: {T}", "&a {T}", "{T}: 1", "[{T}]", "{a: {T}}", "{T} #c", "*a",
	"? {T}", ": {T}",
}

// randomOracleBody joins random fragments, numbering each planted token so the
// oracle can tell which occurrence survived.
func randomOracleBody(rng *rand.Rand) string {
	parts := make([]string, 0, 7)
	for n := 2 + rng.Intn(5); n > 0; n-- {
		fragment := oracleFragments[rng.Intn(len(oracleFragments))]
		parts = append(parts, strings.ReplaceAll(fragment, "{T}", flowSeedToken+strconv.Itoa(len(parts))))
	}
	sep := "\n"
	if rng.Intn(6) == 0 {
		sep = "\r\n"
	}
	return strings.Join(parts, sep) + sep
}

// TestRedactMarkerNeverStoresADecodedCredential is the property behind the
// fix: for any generated body that yaml.v3 parses with the token under a
// listed key, redactMarkerWithURLKeys either fails closed or returns a body
// without the token. The generator is seeded, so a failure replays.
func TestRedactMarkerNeverStoresADecodedCredential(t *testing.T) {
	t.Parallel()

	listed := append(append([]string{}, yarnrcCredentialKeys...), oracleURLKeys...)
	// ESHU_8833_YAML_ORACLE_N and ESHU_8833_YAML_ORACLE_SEED widen the search
	// for a one-off run; the defaults keep the test under a second.
	iterations, seed := envInt("ESHU_8833_YAML_ORACLE_N", 120000), envInt("ESHU_8833_YAML_ORACLE_SEED", 8833)
	rng := rand.New(rand.NewSource(int64(seed)))
	decodedBodies, failedClosed := 0, 0
	for i := 0; i < iterations; i++ {
		body := randomOracleBody(rng)
		tokens, _ := yamlTokensUnderListedKey(body, listed)
		if len(tokens) == 0 {
			continue
		}
		decodedBodies++
		out, _, err := redactMarkerWithURLKeys(body, yarnrcCredentialKeys, oracleURLKeys)
		if err != nil {
			if !errors.Is(err, errYAMLIndirection) {
				t.Fatalf("unexpected err %v for %q", err, body)
			}
			failedClosed++
			continue
		}
		for token := range tokens {
			// A token is a prefix of a longer numbered one at most for the
			// two-digit case, which the generator never reaches (seven parts).
			if strings.Contains(out, token) {
				t.Fatalf("a decoded credential %s was stored:\n in  %q\n out %q", token, body, out)
			}
		}
	}
	if decodedBodies < 1000 {
		t.Fatalf("the generator reached only %d decoding bodies; widen it", decodedBodies)
	}
	t.Logf("decoding bodies %d, failed closed %d", decodedBodies, failedClosed)
}
