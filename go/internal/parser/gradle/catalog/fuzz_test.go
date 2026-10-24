// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// expectation is what the grammar promises for one generated entry.
type expectation struct {
	emitted bool   // a row must exist for the alias
	state   string // dependency_resolution_state
	name    string // coordinate exactly as written
	value   string // version exactly as written, empty unless resolved
}

var (
	goodVersions = []string{"1.0", "2.3.4-RC1", "[1.0,2.0)", "1.+", "latest.release", "33.0.0-jre", "1.0.0.Final"}
	badVersions  = []string{"${x}", "1 0", "a'b", "1;2", "$v", "a\\\\b", "x|y"}
	badParts     = []string{"g g", "g/g", "g:g", "", "g'", "g\\\\", "g$"}
)

// generateEntry builds one library line and the outcome the grammar owes it.
// The oracle is written from the README grammar, not from the implementation.
func generateEntry(rng *rand.Rand, i int, refs map[string]string) (string, expectation) {
	alias := fmt.Sprintf("e%d", i)
	group, artifact := fmt.Sprintf("org.g%d", i), fmt.Sprintf("art-%d", i)
	coord := group + ":" + artifact
	good := goodVersions[rng.Intn(len(goodVersions))]
	bad := badVersions[rng.Intn(len(badVersions))]
	badPart := badParts[rng.Intn(len(badParts))]
	refKey := fmt.Sprintf("k%d", rng.Intn(len(goodVersions)))
	none := expectation{}
	switch rng.Intn(16) {
	case 0:
		return fmt.Sprintf("%s = \"%s:%s\"", alias, coord, good), expectation{true, "resolved", coord, good}
	case 1:
		return fmt.Sprintf("%s = \"%s\"", alias, coord), expectation{true, "partial", coord, ""}
	case 2:
		return fmt.Sprintf("%s = { module = \"%s\", version = \"%s\" }", alias, coord, good),
			expectation{true, "resolved", coord, good}
	case 3:
		return fmt.Sprintf("%s = { module = \"%s\", version.ref = \"%s\" }", alias, coord, refKey),
			expectation{true, "resolved", coord, refs[refKey]}
	case 4:
		return fmt.Sprintf("%s = { group = \"%s\", name = \"%s\", version = { ref = \"%s\" } }", alias, group, artifact, refKey),
			expectation{true, "resolved", coord, refs[refKey]}
	case 5:
		return fmt.Sprintf("%s = { module = \"%s\", version.ref = \"missing%d\" }", alias, coord, i),
			expectation{true, "unresolved", coord, ""}
	case 6:
		return fmt.Sprintf("%s = { module = \"%s\", version.ref = \"rich\" }", alias, coord),
			expectation{true, "unresolved", coord, ""}
	case 7:
		return fmt.Sprintf("%s = { module = \"%s\", version = { strictly = \"%s\" } }", alias, coord, good),
			expectation{true, "unresolved", coord, ""}
	case 8:
		return fmt.Sprintf("%s = { module = \"%s\", version = \"%s\" }", alias, coord, bad),
			expectation{true, "unresolved", coord, ""}
	case 9:
		return fmt.Sprintf("%s = { module = \"%s\", version = \"%s\", classifier = \"x\" }", alias, coord, good), none
	case 10:
		return fmt.Sprintf("%s = \"%s:%s:%s:jar\"", alias, group, artifact, good), none
	case 11:
		return fmt.Sprintf("%s = { module = \"%s:%s\" }", alias, badPart, artifact), none
	case 12:
		return fmt.Sprintf("%s = { module = \"%s\", group = \"%s\", name = \"%s\" }", alias, coord, group, artifact), none
	case 13:
		return fmt.Sprintf("%s = { module = 7 }", alias), none
	case 14:
		return fmt.Sprintf("%s.sub = \"%s:%s\"", alias, coord, good), none
	default:
		return fmt.Sprintf("%s = [\"%s\"]", alias, coord), none
	}
}

// TestScanGeneratedCatalogsMatchTheGrammarOracle drives a few thousand seeded
// catalogs through Scan and checks the two invariants the grammar exists for:
// a resolved row carries the coordinate and version exactly as written, and a
// construct outside the grammar never yields a row.
func TestScanGeneratedCatalogsMatchTheGrammarOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(8822))
	for doc := 0; doc < 3000; doc++ {
		refs := map[string]string{}
		var text strings.Builder
		text.WriteString("[versions]\n")
		for k := range goodVersions {
			key := fmt.Sprintf("k%d", k)
			refs[key] = goodVersions[k]
			fmt.Fprintf(&text, "%s = \"%s\"\n", key, goodVersions[k])
		}
		text.WriteString("rich = { strictly = \"1.0\", prefer = \"1.1\" }\n[libraries]\n")
		want := map[string]expectation{}
		for i, n := 0, 1+rng.Intn(12); i < n; i++ {
			line, exp := generateEntry(rng, i, refs)
			text.WriteString(line + "\n")
			want[fmt.Sprintf("e%d", i)] = exp
		}
		result := Scan([]byte(text.String()))
		if result.State != StateParsed {
			t.Fatalf("doc %d: State=%q (%s)\n%s", doc, result.State, result.ParseError, text.String())
		}
		got := map[string]map[string]any{}
		for _, row := range result.Rows {
			got[row["catalog_alias"].(string)] = row
		}
		for alias, exp := range want {
			row, found := got[alias]
			switch {
			case !exp.emitted && found:
				t.Fatalf("doc %d: %s outside the grammar produced a row %v\n%s", doc, alias, row, text.String())
			case exp.emitted && !found:
				t.Fatalf("doc %d: %s inside the grammar produced no row\n%s", doc, alias, text.String())
			case !exp.emitted:
				continue
			}
			if row["name"] != exp.name || row["value"] != exp.value || row["dependency_resolution_state"] != exp.state {
				t.Fatalf("doc %d: %s row = %v, want name=%q value=%q state=%q\n%s",
					doc, alias, row, exp.name, exp.value, exp.state, text.String())
			}
		}
		if len(got) != len(result.Rows) {
			t.Fatalf("doc %d: duplicate alias rows", doc)
		}
	}
}

// TestScanMutatedCatalogsNeverResolveATextThatIsNotInTheSource flips, drops and
// inserts bytes in valid catalogs and checks that Scan neither panics nor emits
// a resolved row whose coordinate or version is absent from the source.
func TestScanMutatedCatalogsNeverResolveATextThatIsNotInTheSource(t *testing.T) {
	rng := rand.New(rand.NewSource(88222))
	alphabet := []byte("\"'[]{}=.:#\\\n \t$a1")
	base := []byte(positiveCatalog)
	for round := 0; round < 4000; round++ {
		doc := append([]byte(nil), base...)
		for m, n := 0, 1+rng.Intn(6); m < n && len(doc) > 0; m++ {
			at := rng.Intn(len(doc))
			switch rng.Intn(3) {
			case 0:
				doc[at] = alphabet[rng.Intn(len(alphabet))]
			case 1:
				doc = append(doc[:at], doc[at+1:]...)
			default:
				doc = append(doc[:at], append([]byte{alphabet[rng.Intn(len(alphabet))]}, doc[at:]...)...)
			}
		}
		result := Scan(doc)
		if result.State != StateParsed && len(result.Rows) != 0 {
			t.Fatalf("round %d: State=%q with %d rows", round, result.State, len(result.Rows))
		}
		source := string(doc)
		for _, row := range result.Rows {
			name, _ := row["name"].(string)
			for _, part := range strings.Split(name, ":") {
				if !strings.Contains(source, part) {
					t.Fatalf("round %d: row name %q is not in the source\n%s", round, name, source)
				}
			}
			if row["dependency_resolution_state"] == "resolved" {
				if value, _ := row["value"].(string); !strings.Contains(source, value) {
					t.Fatalf("round %d: resolved value %q is not in the source\n%s", round, value, source)
				}
			} else if row["value"] != "" {
				t.Fatalf("round %d: %v row carries value %v", round, row["dependency_resolution_state"], row["value"])
			}
		}
	}
}
