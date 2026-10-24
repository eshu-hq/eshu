// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget_test

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/nuget"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// The generator builds a model first and renders XML from it, so the oracle
// never reads the XML back and never shares the parser's code.

type fuzzPart struct {
	kind int // 0 literal, 1 reference, 2 property function, 3 item list, 4 metadata
	text string
}

type fuzzDecl struct {
	name      string
	value     string
	condition bool
	group     bool
}

type fuzzItem struct {
	kind      string
	name      string
	valid     bool
	version   []fuzzPart
	override  []fuzzPart
	child     bool
	condition bool
	group     bool
}

var (
	fuzzPropNames = []string{"Alpha", "Beta", "Gamma", "Delta"}
	fuzzLiterals  = []string{"1.0.0", "2.3.4-beta", "[1.0,2.0)", "*", "10"}
	fuzzKinds     = []string{"PackageVersion", "GlobalPackageReference", "PackageReference"}
)

func fuzzRefCase(r *rand.Rand, name string) string {
	switch r.Intn(3) {
	case 0:
		return strings.ToLower(name)
	case 1:
		return strings.ToUpper(name)
	}
	return name
}

func fuzzParts(r *rand.Rand) []fuzzPart {
	parts := make([]fuzzPart, 1+r.Intn(3))
	for i := range parts {
		switch n := r.Intn(10); {
		case n < 4:
			parts[i] = fuzzPart{0, fuzzLiterals[r.Intn(len(fuzzLiterals))]}
		case n < 8:
			parts[i] = fuzzPart{1, fuzzRefCase(r, fuzzPropNames[r.Intn(len(fuzzPropNames))])}
		case n == 8:
			parts[i] = fuzzPart{2, "$([MSBuild]::GetVersion())"}
		default:
			kind := 3 + r.Intn(2)
			text := map[int]string{3: "@(Items)", 4: "%(Meta)"}[kind]
			parts[i] = fuzzPart{kind, text}
		}
	}
	return parts
}

func renderParts(parts []fuzzPart) string {
	var b strings.Builder
	for _, part := range parts {
		if part.kind == 1 {
			b.WriteString("$(" + part.text + ")")
		} else {
			b.WriteString(part.text)
		}
	}
	return b.String()
}

// fuzzOracle is the independent expectation for property names.
type fuzzOracle struct {
	values  map[string]string
	differ  map[string]bool
	anyCond map[string]bool
	seen    map[string]bool
}

func newFuzzOracle(decls []fuzzDecl) fuzzOracle {
	o := fuzzOracle{map[string]string{}, map[string]bool{}, map[string]bool{}, map[string]bool{}}
	for _, d := range decls {
		key := strings.ToLower(d.name)
		if o.seen[key] && o.values[key] != d.value {
			o.differ[key] = true
		}
		if !o.seen[key] {
			o.values[key], o.seen[key] = d.value, true
		}
		if d.condition || d.group {
			o.anyCond[key] = true
		}
	}
	return o
}

// state returns "resolved", "ambiguous", or "unresolved" for a reference.
func (o fuzzOracle) state(name string) string {
	key := strings.ToLower(name)
	switch {
	case !o.seen[key]:
		return "unresolved"
	case o.differ[key] || o.anyCond[key]:
		return "ambiguous"
	case o.values[key] == "" || strings.Contains(o.values[key], "$("):
		return "unresolved"
	}
	return "resolved"
}

func (o fuzzOracle) expect(parts []fuzzPart) (value string, evidence string) {
	if len(parts) == 0 {
		return "", "missing_version"
	}
	var b strings.Builder
	ambiguous, unresolved, list := false, false, false
	hasRef := false
	for _, part := range parts {
		switch part.kind {
		case 0:
			b.WriteString(part.text)
		case 1:
			hasRef = true
			switch o.state(part.text) {
			case "ambiguous":
				ambiguous = true
			case "unresolved":
				unresolved = true
			default:
				b.WriteString(o.values[strings.ToLower(part.text)])
			}
		case 2:
			unresolved = true
		default:
			list = true
		}
	}
	raw := renderParts(parts)
	switch {
	case list:
		return raw, "unresolved_msbuild_property"
	case ambiguous:
		return raw, "ambiguous_msbuild_property"
	case unresolved:
		return raw, "unresolved_msbuild_property"
	case hasRef:
		return b.String(), "project_property"
	}
	return raw, "package_reference"
}

func fuzzDocument(r *rand.Rand) (string, []fuzzDecl, []fuzzItem) {
	var decls []fuzzDecl
	var items []fuzzItem
	var b strings.Builder
	b.WriteString("<Project>\n")
	pad := func() string { return strings.Repeat(" ", r.Intn(3)) }
	for i, n := 0, r.Intn(5); i < n; i++ {
		d := fuzzDecl{name: fuzzRefCase(r, fuzzPropNames[r.Intn(len(fuzzPropNames))])}
		switch r.Intn(4) {
		case 0:
			d.value = "1.2"
		case 1:
			d.value = "9"
		case 2:
			d.value = "$(Other)"
		}
		d.condition, d.group = r.Intn(6) == 0, r.Intn(6) == 0
		decls = append(decls, d)
		group, elem := "", ""
		if d.group {
			group = ` Condition="'$(X)' == 'y'"`
		}
		if d.condition {
			elem = ` Condition="'$(Z)' == 'z'"`
		}
		fmt.Fprintf(&b, "<PropertyGroup%s><%s%s>%s%s%s</%s></PropertyGroup>\n", group, d.name, elem, pad(), d.value, pad(), d.name)
	}
	for i, n := 0, r.Intn(7); i < n; i++ {
		it := fuzzItem{kind: fuzzKinds[r.Intn(3)], valid: true, name: fmt.Sprintf("Pkg.%c%d", 'A'+rune(r.Intn(4)), i)}
		attrs := ""
		switch r.Intn(10) {
		case 0:
			it.valid, attrs = false, ` Include="A;B"`
		case 1:
			it.valid, attrs = false, ` Include="$(Dyn)"`
		case 2:
			it.valid, attrs = false, ` Update="Pkg.U"`
		case 3:
			it.valid = false
		default:
			attrs = fmt.Sprintf(` Include="%s"`, it.name)
		}
		switch r.Intn(10) {
		case 0, 1:
		case 2:
			it.override = fuzzParts(r)
		case 3:
			it.version, it.override = fuzzParts(r), fuzzParts(r)
			it.valid = false
		default:
			it.version = fuzzParts(r)
		}
		it.child = r.Intn(4) == 0
		it.condition, it.group = r.Intn(5) == 0, r.Intn(5) == 0
		items = append(items, it)
		group := ""
		if it.group {
			group = ` Condition="'$(TargetFramework)' == 'net8.0'"`
		}
		if it.condition {
			attrs += ` Condition="'$(Use)' == 'true'"`
		}
		inner := ""
		if len(it.version) > 0 {
			if it.child {
				inner += "<Version>" + pad() + renderParts(it.version) + pad() + "</Version>"
			} else {
				attrs += fmt.Sprintf(` Version="%s%s%s"`, pad(), renderParts(it.version), pad())
			}
		}
		if len(it.override) > 0 {
			attrs += fmt.Sprintf(` VersionOverride="%s"`, renderParts(it.override))
		}
		fmt.Fprintf(&b, "<ItemGroup%s><%s%s>%s</%s></ItemGroup>\n", group, it.kind, attrs, inner, it.kind)
	}
	b.WriteString("</Project>")
	return b.String(), decls, items
}

func TestParseCentralPropsMatchesIndependentOracle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Directory.Build.props")
	r := rand.New(rand.NewSource(8824))
	for caseNumber := 0; caseNumber < 3000; caseNumber++ {
		doc, decls, items := fuzzDocument(r)
		if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
		payload, err := nuget.Parse(path, false, shared.Options{})
		if err != nil {
			t.Fatalf("case %d: Parse error %v\n%s", caseNumber, err, doc)
		}
		checkFuzzCase(t, caseNumber, doc, payload, decls, items)
	}
}

func checkFuzzCase(t *testing.T, caseNumber int, doc string, payload map[string]any, decls []fuzzDecl, items []fuzzItem) {
	t.Helper()
	fail := func(format string, args ...any) {
		t.Helper()
		t.Fatalf("case %d: %s\n%s", caseNumber, fmt.Sprintf(format, args...), doc)
	}
	oracle := newFuzzOracle(decls)
	rows := variableRows(t, payload)
	next, skipped := 0, 0
	for _, it := range items {
		if !it.valid {
			skipped++
			continue
		}
		if next >= len(rows) {
			fail("missing row for %s", it.name)
		}
		row := rows[next]
		next++
		parts := it.version
		if len(parts) == 0 {
			parts = it.override
		}
		wantValue, wantEvidence := oracle.expect(parts)
		if row["name"] != it.name || row["section"] != it.kind {
			fail("row %d = %v/%v, want %s/%s", next, row["name"], row["section"], it.name, it.kind)
		}
		if row["version_evidence"] != wantEvidence || row["value"] != wantValue {
			fail("%s: evidence/value = %v/%q, want %s/%q", it.name, row["version_evidence"], row["value"], wantEvidence, wantValue)
		}
		if wantEvidence == "package_reference" && row["value"] != row["requested_version"] {
			fail("%s: a resolved literal must equal the text as written: %v vs %v", it.name, row["value"], row["requested_version"])
		}
		conditional := it.condition || it.group
		wantPartial := conditional || (wantEvidence != "package_reference" && wantEvidence != "project_property")
		if got, _ := row["partial_evidence"].(bool); got != wantPartial {
			fail("%s: partial_evidence = %v, want %v", it.name, got, wantPartial)
		}
		if got, _ := row["conditional_declaration"].(bool); got != conditional {
			fail("%s: conditional_declaration = %v, want %v", it.name, got, conditional)
		}
		if (it.kind == "PackageVersion") == hasKey(row, "direct_dependency") {
			fail("%s: direct_dependency presence wrong for %s", it.name, it.kind)
		}
	}
	for _, d := range decls {
		if next >= len(rows) {
			fail("missing property row for %s", d.name)
		}
		row := rows[next]
		next++
		var want string
		switch key := strings.ToLower(d.name); {
		case d.condition || d.group:
			want = "conditional"
		case oracle.differ[key] || oracle.anyCond[key]:
			want = "ambiguous"
		case d.value == "":
			want = "empty"
		case strings.Contains(d.value, "$("):
			want = "msbuild_expression"
		default:
			want = "literal"
		}
		if row["name"] != d.name || row["value"] != d.value || row["value_evidence"] != want {
			fail("property %s: got %v/%q/%v, want %q/%s", d.name, row["name"], row["value"], row["value_evidence"], d.value, want)
		}
	}
	if next != len(rows) {
		fail("emitted %d rows, oracle expects %d", len(rows), next)
	}
	if got := limitCount(t, payload, "skipped_items"); got != skipped {
		fail("skipped_items = %d, want %d", got, skipped)
	}
}

// TestParseCentralPropsSurvivesMutatedDocuments feeds truncated, spliced, and
// byte-flipped documents. Each must return an error or rows whose resolved
// versions are still proven; none may panic.
func TestParseCentralPropsSurvivesMutatedDocuments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Directory.Packages.props")
	r := rand.New(rand.NewSource(88240))
	for caseNumber := 0; caseNumber < 2000; caseNumber++ {
		doc, _, _ := fuzzDocument(r)
		data := []byte(doc)
		for edits := 1 + r.Intn(3); edits > 0 && len(data) > 0; edits-- {
			switch at := r.Intn(len(data)); r.Intn(4) {
			case 0:
				data = data[:at]
			case 1:
				data = append(data[:at], data[at+1:]...)
			case 2:
				data[at] = "<>&\"'/ ="[r.Intn(8)]
			default:
				data = append(data[:at], append([]byte("<!--"), data[at:]...)...)
			}
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		payload, err := nuget.Parse(path, false, shared.Options{})
		if err != nil {
			continue
		}
		for _, row := range variableRows(t, payload) {
			evidence, _ := row["version_evidence"].(string)
			if evidence == "package_reference" && strings.Contains(fmt.Sprint(row["value"]), "$(") {
				t.Fatalf("case %d: literal row carries an expression %v\n%s", caseNumber, row, data)
			}
			if evidence == "project_property" && strings.Contains(fmt.Sprint(row["value"]), "$(") {
				t.Fatalf("case %d: resolved row still holds an expression %v\n%s", caseNumber, row, data)
			}
			if name, ok := row["name"].(string); ok && row["section"] != "PropertyGroup" && !strings.Contains(string(data), name) {
				t.Fatalf("case %d: invented package name %q\n%s", caseNumber, name, data)
			}
		}
	}
}
