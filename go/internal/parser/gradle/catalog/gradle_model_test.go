// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"fmt"
	"math/rand"
	"os"
	"regexp"
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

// gradleSeparator is the split pattern of Gradle's AbstractSourceGenerator.
var gradleSeparator = regexp.MustCompile(`[.\-_]`)

// gradleJavaName is Gradle's toJavaName: split on '.', '-' and '_' and
// capitalize each part. Two aliases with one Java name fail accessor
// generation (LibrariesSourceGenerator.assertUnique).
func gradleJavaName(alias string) string {
	var b strings.Builder
	for _, part := range gradleSeparator.Split(alias, -1) {
		if part != "" {
			b.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return b.String()
}

// modelAlias spells stem-suffix with a random separator, or as stemSuffix, so
// two draws of one stem collide under Gradle's normalization or its accessor
// names. A '.' alias needs a quoted key.
func modelAlias(rng *rand.Rand, stem string) (key, alias string) {
	suffix := []string{"x", "y"}[rng.Intn(2)]
	switch rng.Intn(5) {
	case 4:
		alias = stem + strings.ToUpper(suffix)
		return alias, alias
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
	alias string
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
//
// Gradle walks each table's keySet, so an alias written as a dotted entry
// (alias.module = ...) or as a [libraries.alias] sub-table registers like a
// one-segment key. The parser emits no row for those forms, but the model
// counts them in the alias groups.
type modelDoc struct {
	text      strings.Builder
	tail      strings.Builder           // sub-tables, written after every table
	versions  map[string][]modelVersion // Java name -> definitions
	tables    [2]map[string]int         // libraries, plugins: Java name -> aliases
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
		java := gradleJavaName(alias)
		switch rng.Intn(5) {
		case 0:
			v := goodVersions[rng.Intn(len(goodVersions))]
			fmt.Fprintf(&d.text, "%s = %q\n", key, v)
			d.versions[java] = append(d.versions[java], modelVersion{alias, v, true})
		case 1:
			v := dynamicVersions[rng.Intn(len(dynamicVersions))]
			fmt.Fprintf(&d.text, "%s = %q\n", key, v)
			d.versions[java] = append(d.versions[java], modelVersion{alias, v, false})
		case 2:
			fmt.Fprintf(&d.text, "%s = { strictly = \"1.0\" }\n", key)
			d.versions[java] = append(d.versions[java], modelVersion{alias, "", false})
		case 3:
			// A rich version written as dotted keys.
			fmt.Fprintf(&d.text, "%s.strictly = \"1.0\"\n%s.prefer = \"1.1\"\n", key, key)
			d.versions[java] = append(d.versions[java], modelVersion{alias, "", false})
		default:
			fmt.Fprintf(&d.tail, "[versions.%s]\nstrictly = \"1.0\"\n", key)
			d.versions[java] = append(d.versions[java], modelVersion{alias, "", false})
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
		d.tables[table][gradleJavaName(alias)]++
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
		section := []string{"libraries", "plugins"}[table]
		switch rng.Intn(8) {
		case 6:
			// The dotted entry form: Gradle registers alias, no row.
			e.supported = false
			fmt.Fprintf(&d.text, "%s.%s = %q\n%s.version = %q\n", key, field, name, key, good)
		case 7:
			// The sub-table form: Gradle registers alias, no row.
			e.supported = false
			fmt.Fprintf(&d.tail, "[%s.%s]\n%s = %q\nversion.ref = %q\n", section, key, field, name, ref)
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

// expect applies Gradle's rules as this package must report them: an alias
// that shares a normalized spelling or a Java name never resolves, a
// reference resolves only to exactly one plain exact definition under the
// normalized name, and a selector never resolves.
func (d *modelDoc) expect(e modelLib, table int) (state, value string, key string) {
	if d.tables[table][gradleJavaName(e.alias)] > 1 {
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
		defs := d.versions[gradleJavaName(e.text)]
		if len(defs) == 1 && gradleNormalize(defs[0].alias) == gradleNormalize(e.text) && defs[0].exact {
			return "resolved", defs[0].text, ""
		}
		return "unresolved", "", e.text
	}
}

func TestScanMatchesTheGradleAliasAndSelectorModel(t *testing.T) {
	cases := modelCases(t)
	rng := rand.New(rand.NewSource(88221))
	var rows, collided, resolved, nested int
	for doc := 0; doc < cases; doc++ {
		d := &modelDoc{
			versions: map[string][]modelVersion{}, versionKy: map[string]bool{},
			tables: [2]map[string]int{{}, {}}, keys: [2]map[string]bool{{}, {}},
		}
		d.writeVersions(rng)
		d.writeEntries(rng, 0, 1+rng.Intn(8))
		libs := len(d.entries)
		d.writeEntries(rng, 1, rng.Intn(4))
		d.text.WriteString(d.tail.String())
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
			if !e.supported {
				nested++
			}
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
	t.Logf("cases=%d rows=%d resolved=%d collided aliases=%d no-row entries=%d", cases, rows, resolved, collided, nested)
	if collided == 0 || resolved == 0 || nested == 0 {
		t.Fatalf("generator never produced a collision (%d), a resolved row (%d) or a no-row entry (%d)", collided, resolved, nested)
	}
}
