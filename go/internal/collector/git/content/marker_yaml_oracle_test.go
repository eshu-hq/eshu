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
	if node == nil || depth > 20000 {
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
// cannot pass for being no leak at all. It asserts nothing for a seed yaml.v3
// rejects (parsed=false): every NEL/LS/PS-split, break-prefixed, and
// residual-neighbor directive seed passes here vacuously, so their coverage
// lives in TestRedactMarkerFlowShapesFailClosed,
// TestRedactMarkerVersionDirectiveVariantQuotedContinuationFailsClosed, and the
// js-yaml/PyYAML oracle instead. That gap is noted rather than papered over:
// no honest yaml.v3-based assertion can see a class yaml.v3 rejects.
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
	// Marker text, partial replacements, and nesting deeper than any cap.
	"REDACTED_api_token", "{T} REDACTED_api_token", "{T}-REDACTED_api_token", "REDACTED_secret_literal {T}",
	"{T}, httpsProxy: http://user:pw@h/, z: 1}", "{T}, note: " + "AKIA" + "ABCDEFGHIJKLMNOP" + ", z: 1}",
	"httpsProxy: http://user:pw@h/, npmAuthToken: abcd", strings.Repeat("[", 70), "{T}" + strings.Repeat("]", 70),
	// Quoted scalars that run onto comment-looking lines, closers followed by
	// text, and keys opened inside a flow collection.
	"npmAuthToken: [\"abcd", "{npmAuthToken: ['abcd", "{npmAuthToken: {a: \"abcd", "{npmAuthToken: [!!str \"abcd", "\"abcd",
	"#{T}\"]}", "#{T}'", "\t#{T}\"]", "  #{T}\"", "]},{T}", ",{T}]", "], {T}", "}, {T}}", "{a: \"x", "{T}\"]}", "...{T}\"]",
	// Quote characters that are plain text, and quotes after them.
	"a\"b", "it's", "[a\"b, \"abcd", "[it's, 'abcd", "{npmAuthToken: [a\"b, \"abcd", "{npmAuthToken: [it's, 'abcd", "b\"c, \"abcd",
	"{npmAuthToken: [\"x\", b\"c, \"abcd",
	// A quoted scalar opened on the line after a bare listed key, closed at an
	// indent at or below the key: the P0 continuation shape.
	"  \"abcd", "  'abcd", "      \"abcd", "      'abcd", "  \"https://user:abcd",
	// Token-bearing quoted tails closing at the key's indent or below: with the
	// "..." fragment, a bare key, and an opener above they compose the P1
	// "..."-prefixed quoted-continuation shape, which only the lexical fallback
	// covers.
	"abcd{T}\"", "abcd{T}'",
	// A "%YAML 1.2" version-directive line: with a "---" fragment, a bare key,
	// and an opener and tail above it composes the residual-P1
	// directive-prefixed quoted-continuation shape, which yaml.v3 rejects and
	// only the lexical fallback covers. yamlTokensUnderListedKey cannot see
	// this class, so this fragment is its coverage: the js-yaml/PyYAML oracle
	// reads what it composes.
	"%YAML 1.2",
	// BOM-prefixed, tab-indented, and unspaced directive variants: js-yaml
	// 4.1.1 reads each as a directive (PyYAML 6.0.3 reads the BOM-prefixed
	// one too) while yaml.v3 rejects all three, so they compose the same
	// fail-closed shape through the widened directive arm.
	"\uFEFF%YAML 1.2", "\t%YAML 1.2", "%YAML1.2",
	// CR-prefixed directive variant: the break-prefixed class (CR, NEL, LS,
	// PS, and a late CR-separated directive) splits a "\n"-only line before
	// the "%", while both downstream parsers read the segment as a directive
	// line and yaml.v3 rejects it, so it composes the same fail-closed shape.
	"\r%YAML 1.2",
	// Letter-suffixed and reserved directive variants: js-yaml 4.1.1 reads
	// each as a directive (PyYAML 6.0.3 reads a subset) while yaml.v3 rejects
	// each, so they compose the same fail-closed shape through the
	// any-directive arm.
	"%YAMLx", "%FOO",
	// NEL-split and leading-NEL directive fragments: yaml.v3 rejects each, so
	// the in-process oracle is blind to them and only the lexical fallback
	// covers them; the js-yaml/PyYAML oracle reads what they compose. A
	// leading-break fragment joined after another fragment also composes the
	// late-break shape. LS and PS share the pre-tail scanner path, so NEL
	// stands for the three breaks here; the seed and variant tables cover all
	// three explicitly.
	"%\u0085YAML 1.2", "\u0085%YAML 1.2",
	// Residual split-neighbor fragments: a space or tab between the break and
	// the name, a trailing break with no name on the line (which joins with
	// "---" into the trailing-break shape), and a multi-break run. js-yaml
	// 4.1.1 decodes each while yaml.v3 and PyYAML 6.0.3 reject each, so only
	// the widened lone-"%" arm covers them.
	"%\u0085 YAML 1.2", "%\u0085\tYAML 1.2", "%\u0085", "%\u0085\u0085YAML 1.2",
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
	// for a one-off run; the defaults keep the test around a second. N grows
	// with the fragment table: each directive-line fragment breaks yaml.v3
	// parsing in the bodies it joins, so a wider table needs more draws to
	// keep the 600-decoding-body floor with margin. The thirteen
	// directive-line fragments (seven before, six NEL-split/break/neighbor
	// ones) set N at 230000 for ~750 decoding bodies against the 600 floor.
	iterations, seed := envInt("ESHU_8833_YAML_ORACLE_N", 230000), envInt("ESHU_8833_YAML_ORACLE_SEED", 8833)
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
	if decodedBodies < 600 {
		t.Fatalf("the generator reached only %d decoding bodies; widen it", decodedBodies)
	}
	t.Logf("decoding bodies %d, failed closed %d", decodedBodies, failedClosed)
}
