// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// tomlRejects reports whether go-toml's full decode into a map rejects text.
// The map decode tracks every key, unlike a decode into an empty struct,
// which skips the keys under any table it has no field for. Tests use it as
// the reference for "is this valid TOML"; Scan never decodes into a map.
func tomlRejects(text string) bool {
	var document map[string]any
	return toml.Unmarshal([]byte(text), &document) != nil
}

// A key under a catalog table that TOML forbids, because it repeats a key or
// redefines a table another form already defined, makes the whole file
// malformed with no rows. The semantic decode into an empty struct skipped
// every key under a table header, so these files used to parse and resolve.
func TestScanRedefinedKeyUnderACatalogTableIsMalformed(t *testing.T) {
	t.Parallel()
	for name, text := range map[string]string{
		"dotted then sub-table":           "[libraries]\nok = \"g:b:2.0\"\ncd.module = \"g:a\"\n[libraries.cd]\nversion = \"1\"\n",
		"value then sub-table":            "[libraries]\nx = \"g:a:1.0\"\n[libraries.x]\nmodule = \"g:a\"\n",
		"value then dotted":               "[libraries]\nx = \"g:a:1.0\"\nx.module = \"g:a\"\n",
		"dotted then value":               "[libraries]\nx.module = \"g:a\"\nx = \"g:a:1.0\"\n",
		"value then array table":          "[libraries]\nx = \"g:a:1.0\"\n[[libraries.x]]\nmodule = \"g:a\"\n",
		"dotted repeated":                 "[libraries]\nx.module = \"g:a\"\nx.module = \"g:b\"\ny = \"g:y:1.0\"\n",
		"deep dotted then deep sub-table": "[libraries]\nx.module = \"g:a\"\nx.version.ref = \"v\"\n[libraries.x.version]\nstrictly = \"1\"\ny = \"g:y:1.0\"\n",
		"inline then dotted":              "[libraries]\nx = { module = \"g:a\" }\nx.version = \"1\"\ny = \"g:y:1.0\"\n",
		"inline then sub-table":           "[libraries]\nx = { module = \"g:a\" }\n[libraries.x]\nversion = \"1\"\n",
		"inline key repeated":             "[libraries]\nx = { module = \"g:a\", module = \"g:b\" }\ny = \"g:y:1.0\"\n",
		"inline dotted repeated":          "[libraries]\nx = { module = \"g:a\", version.ref = \"v\", version.ref = \"w\" }\ny = \"g:y:1.0\"\n",
		"sub-table repeated":              "[libraries]\ny = \"g:y:1.0\"\n[libraries.x]\nmodule = \"g:a\"\n[libraries.x]\nversion = \"1\"\n",
		"sub-table key repeated":          "[libraries]\ny = \"g:y:1.0\"\n[libraries.x]\nmodule = \"g:a\"\nmodule = \"g:b\"\n",
		"array table then value":          "[[libraries.x]]\nmodule = \"g:a\"\n[libraries]\nx = \"g:a:1.0\"\n",
		"versions dotted":                 "[versions]\nv = \"1.0\"\nv.strictly = \"2.0\"\n[libraries]\nx = { module = \"g:a\", version.ref = \"v\" }\n",
		"versions sub-table":              "[versions]\nv = \"1.0\"\n[versions.v]\nstrictly = \"2.0\"\n[libraries]\nx = { module = \"g:a\", version.ref = \"v\" }\n",
		"plugins dotted":                  "[plugins]\np = \"org.p:1.0\"\np.id = \"org.q\"\n[libraries]\ny = \"g:y:1.0\"\n",
		"plugins sub-table":               "[plugins]\np = \"org.p:1.0\"\n[plugins.p]\nid = \"org.q\"\n[libraries]\ny = \"g:y:1.0\"\n",
		"bundles dotted":                  "[bundles]\nb = [\"y\"]\nb.x = 1\n[libraries]\ny = \"g:y:1.0\"\n",
		"bundles sub-table":               "[libraries]\ny = \"g:y:1.0\"\n[bundles]\nb = [\"y\"]\n[bundles.b]\nx = 1\n",
		"unsupported table key repeated":  "[metadata]\na = 1\na = 2\n[libraries]\ny = \"g:y:1.0\"\n",
		"table under a value":             "[libraries]\ny = \"g:y:1.0\"\n[libraries.y.version]\nref = \"v\"\n",
		"large inline key repeated":       "[metadata]\nm = { " + manyInlineKeys(maxInlineKeys+1) + ", k0 = 2 }\n[libraries]\ny = \"g:y:1.0\"\n",
		"large inline value as table":     "[metadata]\nm = { " + manyInlineKeys(maxInlineKeys+1) + ", k0.a = 2 }\n[libraries]\ny = \"g:y:1.0\"\n",
		"nested inline key repeated":      "[libraries]\ny = \"g:y:1.0\"\nx = { module = \"g:a\", version = { strictly = \"1\", strictly = \"2\" } }\n",
		"inline in array key repeated":    "[bundles]\nb = [{ a = 1, a = 2 }]\n[libraries]\ny = \"g:y:1.0\"\n",
		"dotted into a defined table":     "[libraries.x]\nmodule = \"g:a\"\n[libraries]\ny = \"g:y:1.0\"\nx.version = \"1\"\n",
		"dotted through an array table":   "[[libraries.x]]\nmodule = \"g:a\"\n[libraries]\ny = \"g:y:1.0\"\nx.version = \"1\"\n",
		"table over an array table":       "[libraries]\ny = \"g:y:1.0\"\n[[libraries.x]]\nmodule = \"g:a\"\n[libraries.x]\nversion = \"1\"\n",
	} {
		if !tomlRejects(text) {
			t.Fatalf("%s: the reference decode accepts this text; the case is not invalid TOML\n%s", name, text)
		}
		result := Scan([]byte(text))
		if result.State != StateMalformed || len(result.Rows) != 0 || result.ParseError == "" {
			t.Errorf("%s: State=%q rows=%d err=%q, want malformed with none\n%s",
				name, result.State, len(result.Rows), result.ParseError, text)
		}
	}
}

// Each rule reports its own reason, so a reader of parse_error can tell a
// repeated key from a table defined twice or a value used as a table.
func TestScanKeyRedefinitionReasons(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ text, want string }{
		"repeated key":            {"[libraries]\nx = \"g:a:1\"\nx = \"g:a:2\"\n", "alias repeated in one table"},
		"repeated dotted key":     {"[libraries]\nx.module = \"g:a\"\nx.module = \"g:b\"\n", reasonKeyRepeated},
		"repeated inline key":     {"[libraries]\nx = { module = \"g:a\", module = \"g:b\" }\n", reasonKeyRepeated},
		"repeated catalog header": {"[libraries]\ny = \"g:y:1\"\n[libraries]\n", "table header repeated"},
		"repeated header":         {"[libraries.x]\n[libraries.x]\n", reasonTableRepeated},
		"header over dotted":      {"[libraries]\nx.module = \"g:a\"\n[libraries.x]\n", reasonTableRepeated},
		"header over a value":     {"[libraries]\nx = \"g:a:1\"\n[libraries.x]\n", reasonValueAsTable},
		"dotted over a value":     {"[libraries]\nx = \"g:a:1\"\nx.module = \"g:a\"\n", reasonValueAsTable},
		"inline over a value":     {"[libraries]\nx = { a = 1, a.b = 2 }\n", reasonValueAsTable},
		"dotted into a header":    {"[libraries.x]\n[libraries]\nx.version = \"1\"\n", reasonClosedTable},
		"header over an array":    {"[[libraries.x]]\n[libraries.x]\n", reasonArrayTableMixed},
		"array over a header":     {"[libraries.x]\n[[libraries.x]]\n", reasonArrayTableMixed},
		"dotted through an array": {"[[libraries.x]]\n[libraries]\nx.version = \"1\"\n", reasonArrayTableMixed},
	} {
		if !tomlRejects(tc.text) {
			t.Fatalf("%s: the reference decode accepts this text", name)
		}
		result := Scan([]byte(tc.text))
		if result.State != StateMalformed || result.ParseError != tc.want {
			t.Errorf("%s: State=%q err=%q, want malformed (%s)", name, result.State, result.ParseError, tc.want)
		}
	}
}

// The legal ways to spread one alias over several lines must stay parsed:
// dotted keys that share a prefix, a sub-table of a dotted-key table, and a
// super-table header after its sub-table.
func TestScanLegalSpreadAliasStaysParsed(t *testing.T) {
	t.Parallel()
	for name, text := range map[string]string{
		"dotted module and version":   "[libraries]\nx.module = \"g:a\"\nx.version = \"1\"\ny = \"g:y:1.0\"\n",
		"dotted module and ref":       "[libraries]\nx.module = \"g:a\"\nx.version.ref = \"v\"\ny = \"g:y:1.0\"\n",
		"dotted then deeper header":   "[libraries]\nx.module = \"g:a\"\n[libraries.x.version]\nref = \"v\"\n[bundles]\nb = [\"y\"]\n[plugins]\np = \"org.p:1.0\"\n",
		"sub-table before super":      "[libraries.x]\nmodule = \"g:a\"\n[libraries]\ny = \"g:y:1.0\"\n",
		"sub-table and deeper":        "[libraries]\ny = \"g:y:1.0\"\n[libraries.x]\nmodule = \"g:a\"\n[libraries.x.version]\nref = \"v\"\n",
		"array tables repeated":       "[libraries]\ny = \"g:y:1.0\"\n[[libraries.x]]\nmodule = \"g:a\"\n[[libraries.x]]\nmodule = \"g:b\"\n",
		"versions dotted rich":        "[versions]\nv.strictly = \"1.0\"\nv.prefer = \"1.1\"\n[libraries]\ny = \"g:y:1.0\"\n",
		"same key in two tables":      "[versions]\ny = \"1.0\"\n[libraries]\ny = { module = \"g:y\", version.ref = \"y\" }\n[plugins]\ny = \"org.y:1.0\"\n",
		"inline dotted keys":          "[libraries]\ny = { module = \"g:y\", version.ref = \"v\", version.strictly = \"1\" }\n",
		"quoted and bare differ":      "[libraries]\n\"x.y\" = \"g:a:1.0\"\nx = \"g:x:1.0\"\n",
		"unsupported table then same": "[metadata]\na = 1\n[other]\na = 1\n[libraries]\ny = \"g:y:1.0\"\n",
		"one name at two levels":      "[libraries]\nx.x = \"1\"\ny = { module = \"g:y\", version.version = \"1\" }\n[x]\nx = 1\n",
		"large inline table":          "[metadata]\nm = { " + manyInlineKeys(maxInlineKeys+1) + " }\n[libraries]\ny = \"g:y:1.0\"\n",
	} {
		if tomlRejects(text) {
			t.Fatalf("%s: the reference decode rejects this text; the case is not valid TOML\n%s", name, text)
		}
		result := Scan([]byte(text))
		if result.State != StateParsed {
			t.Errorf("%s: State=%q err=%q, want parsed\n%s", name, result.State, result.ParseError, text)
		}
	}
}

// manyInlineKeys writes n distinct inline-table pairs k0 = 1, k1 = 1, ...
// More than maxInlineKeys moves the table from the list scan to the index.
func manyInlineKeys(n int) string {
	pairs := make([]string, n)
	for i := range pairs {
		pairs[i] = fmt.Sprintf("k%d = 1", i)
	}
	return strings.Join(pairs, ", ")
}
