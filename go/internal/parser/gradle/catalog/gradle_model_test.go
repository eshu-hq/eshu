// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
)

// modelCases is the number of generated catalogs. Set
// ESHU_CATALOG_MODEL_CASES for a longer local run (for example 30000).
func modelCases(t *testing.T) int {
	t.Helper()
	if raw := os.Getenv("ESHU_CATALOG_MODEL_CASES"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			t.Fatalf("ESHU_CATALOG_MODEL_CASES=%q is not a positive integer", raw)
		}
		return n
	}
	return 3000
}

// gradleNormalize is Gradle's AliasNormalizer: each of '-', '_' and '.' in an
// alias becomes '.'.
func gradleNormalize(alias string) string {
	return strings.NewReplacer("-", ".", "_", ".").Replace(alias)
}

// modelAlias spells stem-suffix with a random separator, so two draws of one
// stem collide under Gradle's normalization. A '.' alias needs a quoted key.
func modelAlias(rng *rand.Rand, stem string) (key, alias string) {
	suffix := []string{"x", "y"}[rng.Intn(2)]
	switch rng.Intn(4) {
	case 0:
		alias = stem + "-" + suffix
	case 1:
		alias = stem + "_" + suffix
	case 2:
		alias = stem + "." + suffix
		return strconv.Quote(alias), alias
	default:
		alias = stem + suffix
	}
	return alias, alias
}

// modelVersion is one [versions] definition as the generator wrote it.
type modelVersion struct {
	text  string
	exact bool // drawn from goodVersions, not dynamicVersions or a rich table
}

// modelLib is one [libraries] or [plugins] entry as the generator wrote it.
type modelLib struct {
	alias     string
	name      string
	supported bool   // inside the grammar, so it yields a row
	kind      string // "literal", "ref", "none"
	text      string // literal text or reference as written
	exact     bool   // literal drawn from goodVersions
}

// modelDoc is one generated catalog and the inputs of its Gradle model.
type modelDoc struct {
	text      strings.Builder
	versions  map[string][]modelVersion // normalized alias -> definitions
	tables    [2]map[string]int         // libraries, plugins: normalized alias -> aliases
	entries   []modelLib
	versionKy map[string]bool // exact keys, to avoid a TOML duplicate
	keys      [2]map[string]bool
}

func (d *modelDoc) writeVersions(rng *rand.Rand) {
	d.text.WriteString("[versions]\n")
	for i, n := 0, rng.Intn(6); i < n; i++ {
		key, alias := modelAlias(rng, []string{"v", "w"}[rng.Intn(2)])
		if d.versionKy[alias] {
			continue
		}
		d.versionKy[alias] = true
		norm := gradleNormalize(alias)
		switch rng.Intn(3) {
		case 0:
			v := goodVersions[rng.Intn(len(goodVersions))]
			fmt.Fprintf(&d.text, "%s = %q\n", key, v)
			d.versions[norm] = append(d.versions[norm], modelVersion{v, true})
		case 1:
			v := dynamicVersions[rng.Intn(len(dynamicVersions))]
			fmt.Fprintf(&d.text, "%s = %q\n", key, v)
			d.versions[norm] = append(d.versions[norm], modelVersion{v, false})
		default:
			fmt.Fprintf(&d.text, "%s = { strictly = \"1.0\" }\n", key)
			d.versions[norm] = append(d.versions[norm], modelVersion{"", false})
		}
	}
}

func (d *modelDoc) writeEntries(rng *rand.Rand, table int, n int) {
	d.text.WriteString([]string{"[libraries]\n", "[plugins]\n"}[table])
	for i := 0; i < n; i++ {
		key, alias := modelAlias(rng, []string{"l", "m", "n"}[rng.Intn(3)])
		if d.keys[table][alias] {
			continue
		}
		d.keys[table][alias] = true
		d.tables[table][gradleNormalize(alias)]++
		e := modelLib{alias: alias, supported: true}
		name := fmt.Sprintf("org.g%d:a%d", i, table)
		if table == 1 {
			name = fmt.Sprintf("org.p%d", i)
		}
		e.name = name
		good := goodVersions[rng.Intn(len(goodVersions))]
		dynamic := dynamicVersions[rng.Intn(len(dynamicVersions))]
		_, ref := modelAlias(rng, []string{"v", "w"}[rng.Intn(2)])
		field := "module"
		if table == 1 {
			field = "id"
		}
		switch rng.Intn(6) {
		case 0:
			e.kind, e.text, e.exact = "literal", good, true
			fmt.Fprintf(&d.text, "%s = %q\n", key, name+":"+good)
		case 1:
			e.kind, e.text = "literal", dynamic
			fmt.Fprintf(&d.text, "%s = { %s = %q, version = %q }\n", key, field, name, dynamic)
		case 2:
			e.kind, e.text = "ref", ref
			fmt.Fprintf(&d.text, "%s = { %s = %q, version.ref = %q }\n", key, field, name, ref)
		case 3:
			e.kind = "none"
			fmt.Fprintf(&d.text, "%s = { %s = %q }\n", key, field, name)
		case 4:
			e.kind, e.text, e.exact = "literal", good, true
			fmt.Fprintf(&d.text, "%s = { %s = %q, version = %q }\n", key, field, name, good)
		default:
			// A string notation with a part missing is a Gradle error.
			e.supported = false
			missing := name
			if table == 1 {
				missing = name + ":" + good + ":extra"
			}
			fmt.Fprintf(&d.text, "%s = %q\n", key, missing)
		}
		d.entries = append(d.entries, e)
	}
}

// expect applies Gradle's rules as this package must report them: a colliding
// alias never resolves, a reference resolves only to exactly one plain exact
// definition under the normalized name, and a selector never resolves.
func (d *modelDoc) expect(e modelLib, table int) (state, value string, key string) {
	if d.tables[table][gradleNormalize(e.alias)] > 1 {
		return "unresolved", "", "catalog_alias"
	}
	switch e.kind {
	case "none":
		return "partial", "", ""
	case "literal":
		if e.exact {
			return "resolved", e.text, ""
		}
		return "unresolved", "", "version"
	default:
		defs := d.versions[gradleNormalize(e.text)]
		if len(defs) == 1 && defs[0].exact {
			return "resolved", defs[0].text, ""
		}
		return "unresolved", "", e.text
	}
}

func TestScanMatchesTheGradleAliasAndSelectorModel(t *testing.T) {
	cases := modelCases(t)
	rng := rand.New(rand.NewSource(88221))
	var rows, collided, resolved int
	for doc := 0; doc < cases; doc++ {
		d := &modelDoc{
			versions: map[string][]modelVersion{}, versionKy: map[string]bool{},
			tables: [2]map[string]int{{}, {}}, keys: [2]map[string]bool{{}, {}},
		}
		d.writeVersions(rng)
		d.writeEntries(rng, 0, 1+rng.Intn(8))
		libs := len(d.entries)
		d.writeEntries(rng, 1, rng.Intn(4))
		result := Scan([]byte(d.text.String()))
		if result.State != StateParsed {
			t.Fatalf("doc %d: State=%q (%s)\n%s", doc, result.State, result.ParseError, d.text.String())
		}
		wantCollided := 0
		for _, defs := range d.versions {
			if len(defs) > 1 {
				wantCollided += len(defs)
			}
		}
		for _, table := range d.tables {
			for _, n := range table {
				if n > 1 {
					wantCollided += n
				}
			}
		}
		if result.Counts.Collided != wantCollided {
			t.Fatalf("doc %d: Collided=%d, want %d\n%s", doc, result.Counts.Collided, wantCollided, d.text.String())
		}
		byAlias := [2]map[string]map[string]any{{}, {}}
		for _, row := range result.Rows {
			table := 0
			if row["section"] == "plugins" {
				table = 1
			}
			byAlias[table][row["catalog_alias"].(string)] = row
		}
		emitted := 0
		for i, e := range d.entries {
			table := 0
			if i >= libs {
				table = 1
			}
			row, found := byAlias[table][e.alias]
			if found != e.supported {
				t.Fatalf("doc %d: %s row found=%v, want %v\n%s", doc, e.alias, found, e.supported, d.text.String())
			}
			if !found {
				continue
			}
			emitted++
			state, value, key := d.expect(e, table)
			keys, _ := row["dependency_unresolved_keys"].([]string)
			if row["name"] != e.name || row["dependency_resolution_state"] != state || row["value"] != value ||
				(key != "" && (len(keys) != 1 || keys[0] != key)) {
				t.Fatalf("doc %d: %s row=%v, want state=%s value=%q key=%q\n%s",
					doc, e.alias, row, state, value, key, d.text.String())
			}
			if state == "resolved" {
				resolved++
			}
		}
		if emitted != len(result.Rows) {
			t.Fatalf("doc %d: %d rows, want %d", doc, len(result.Rows), emitted)
		}
		rows += emitted
		collided += wantCollided
	}
	t.Logf("cases=%d rows=%d resolved=%d collided aliases=%d", cases, rows, resolved, collided)
	if collided == 0 || resolved == 0 {
		t.Fatalf("generator never produced a collision (%d) or a resolved row (%d)", collided, resolved)
	}
}
