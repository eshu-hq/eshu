// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget_test

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/nuget"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// The generator builds a model first and renders XML from it, so the oracle
// (props_fuzz_oracle_test.go) never reads the XML back and never shares the
// parser's code. Item classes cover the reviewer's MSBuild constructs:
// conditioned metadata children, later Update and Remove, duplicate Includes,
// an empty child over a set attribute, Exclude, case-variant attributes and
// item types, Import, and ManagePackageVersionsCentrally.

type fuzzPart struct {
	kind int // 0 literal, 1 reference, 2 property function, 3 item list, 4 metadata, 5 %XX escape
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
	valid     bool // a row is emitted
	literal   bool // Include names one literal package (counts as a duplicate)
	version   []fuzzPart
	override  []fuzzPart
	child     bool
	condition bool
	group     bool
	condChild bool
	exclude   bool
	// unresolvedSpec: an Include list or expression, or a case-variant item
	// type, that could name any package of this type.
	unresolvedSpec bool
	op             string // "update" or "remove" for an Update/Remove item
	opTarget       string
	opWild         bool
	// PrivateAssets and IncludeAssets as declared, each as an attribute or a
	// child element; a conditioned child makes the value unproven.
	private, include         string
	privateCond, includeCond bool
}

type fuzzModel struct {
	decls       []fuzzDecl
	items       []fuzzItem
	unevaluated bool
}

var (
	fuzzPropNames = []string{"Alpha", "Beta", "Gamma", "Delta"}
	fuzzLiterals  = []string{"1.0.0", "2.3.4-beta", "[1.0,2.0)", "*", "10"}
	fuzzKinds     = []string{"PackageVersion", "GlobalPackageReference", "PackageReference"}
	fuzzCentral   = "ManagePackageVersionsCentrally"
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
			kind := 3 + r.Intn(3)
			text := map[int]string{3: "@(Items)", 4: "%(Meta)", 5: "1%2E0"}[kind]
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

func fuzzDecls(r *rand.Rand, b *strings.Builder) []fuzzDecl {
	var decls []fuzzDecl
	pad := func() string { return strings.Repeat(" ", r.Intn(3)) }
	n := r.Intn(5)
	central := r.Intn(5) == 0
	for i := 0; i < n || central; i++ {
		var d fuzzDecl
		if i >= n {
			// ManagePackageVersionsCentrally in some case and value.
			central = false
			d.name = fuzzRefCase(r, fuzzCentral)
			d.value = []string{"true", "false", "TRUE", "$(Other)"}[r.Intn(4)]
			d.condition = r.Intn(4) == 0
		} else {
			d.name = fuzzRefCase(r, fuzzPropNames[r.Intn(len(fuzzPropNames))])
			d.value = []string{"1.2", "9", "$(Other)", "", "1%2E2"}[r.Intn(5)]
			d.condition, d.group = r.Intn(6) == 0, r.Intn(6) == 0
		}
		decls = append(decls, d)
		group, elem := "", ""
		if d.group {
			group = ` Condition="'$(X)' == 'y'"`
		}
		if d.condition {
			elem = ` Condition="'$(Z)' == 'z'"`
		}
		fmt.Fprintf(b, "<PropertyGroup%s><%s%s>%s%s%s</%s></PropertyGroup>\n", group, d.name, elem, pad(), d.value, pad(), d.name)
	}
	return decls
}

// fuzzOp renders an Update or Remove item aimed at an earlier item name, a
// name nobody declared, or a wildcard.
func fuzzOp(r *rand.Rand, items []fuzzItem, op string) (fuzzItem, string) {
	it := fuzzItem{kind: fuzzKinds[r.Intn(3)], op: op, opTarget: "Pkg.U"}
	switch n := r.Intn(5); {
	case n == 0:
		it.opTarget, it.opWild = []string{"Pkg.*", "Pkg%2EA1"}[r.Intn(2)], true
	case n < 4 && len(items) > 0:
		earlier := items[r.Intn(len(items))]
		if earlier.name != "" {
			it.opTarget = fuzzRefCase(r, earlier.name)
			if r.Intn(2) == 0 {
				it.kind = earlier.kind
			}
		}
	}
	attr := map[string]string{"update": "Update", "remove": "Remove"}[op]
	extra := ""
	if op == "update" {
		extra = ` Version="5.0.0"`
	}
	return it, fmt.Sprintf("<ItemGroup><%s %s=\"%s\"%s /></ItemGroup>\n", it.kind, attr, it.opTarget, extra)
}

// fuzzName returns a fresh name, or the name of an earlier literal Include of
// the same type in another case (a duplicate).
func fuzzName(r *rand.Rand, items []fuzzItem, kind string, i int) string {
	if r.Intn(6) == 0 {
		for _, earlier := range items {
			if earlier.literal && earlier.kind == kind {
				return fuzzRefCase(r, earlier.name)
			}
		}
	}
	return fmt.Sprintf("Pkg.%c%d", 'A'+rune(r.Intn(4)), i)
}

func fuzzPlainItem(r *rand.Rand, items []fuzzItem, i int) (fuzzItem, string) {
	pad := func() string { return strings.Repeat(" ", r.Intn(3)) }
	it := fuzzItem{kind: fuzzKinds[r.Intn(3)], valid: true}
	it.name = fuzzName(r, items, it.kind, i)
	attrs := ""
	switch r.Intn(10) {
	case 0:
		it.valid, it.unresolvedSpec, it.name, attrs = false, true, "", ` Include="A;B"`
	case 1:
		it.valid, it.unresolvedSpec, it.name, attrs = false, true, "", ` Include="$(Dyn)"`
	case 2:
		it.valid, it.literal = false, true
		attrs = fmt.Sprintf(` Include="%s" version="9.9.9"`, it.name)
	case 3:
		it.valid, it.name = false, ""
	case 4:
		// MSBuild unescapes %2E to ".", so this could name any package.
		it.valid, it.unresolvedSpec, it.name, attrs = false, true, "", fmt.Sprintf(` Include="Pkg%%2EE%d"`, i)
	default:
		it.literal = true
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
	group := ""
	if it.group {
		group = ` Condition="'$(TargetFramework)' == 'net8.0'"`
	}
	if it.condition {
		attrs += ` Condition="'$(Use)' == 'true'"`
	}
	if r.Intn(8) == 0 {
		it.exclude = true
		attrs += fmt.Sprintf(` Exclude="%s"`, it.name)
	}
	inner := ""
	if len(it.version) > 0 {
		switch {
		case it.child:
			cond := ""
			if r.Intn(3) == 0 {
				it.condChild, cond = true, ` Condition="'$(C)' == 'c'"`
			}
			inner += "<Version" + cond + ">" + pad() + renderParts(it.version) + pad() + "</Version>"
		default:
			attrs += fmt.Sprintf(` Version="%s%s%s"`, pad(), renderParts(it.version), pad())
			if r.Intn(8) == 0 {
				// An empty child applied after a set attribute.
				it.valid, inner = false, "<Version></Version>"
			}
		}
	}
	if len(it.override) > 0 {
		attrs += fmt.Sprintf(` VersionOverride="%s"`, renderParts(it.override))
	}
	assetAttrs, assetInner := fuzzAssets(r, &it)
	attrs, inner = attrs+assetAttrs, inner+assetInner
	return it, fmt.Sprintf("<ItemGroup%s><%s%s>%s</%s></ItemGroup>\n", group, it.kind, attrs, inner, it.kind)
}

func fuzzDocument(r *rand.Rand) (string, fuzzModel) {
	var model fuzzModel
	var b strings.Builder
	b.WriteString("<Project>\n")
	importAt := -1
	if r.Intn(8) == 0 {
		model.unevaluated, importAt = true, r.Intn(2)
	}
	if importAt == 0 {
		b.WriteString("<Import Project=\"other.props\" />\n")
	}
	model.decls = fuzzDecls(r, &b)
	for i, n := 0, r.Intn(9); i < n; i++ {
		var it fuzzItem
		var text string
		switch r.Intn(14) {
		case 0:
			it, text = fuzzOp(r, model.items, "update")
		case 1:
			it, text = fuzzOp(r, model.items, "remove")
		case 2:
			kind := fuzzKinds[r.Intn(3)]
			it = fuzzItem{kind: kind, unresolvedSpec: true}
			text = fmt.Sprintf("<ItemGroup><%s Include=\"Pkg.Z%d\" Version=\"1.0.0\" /></ItemGroup>\n", strings.ToLower(kind), i)
		default:
			it, text = fuzzPlainItem(r, model.items, i)
		}
		model.items = append(model.items, it)
		b.WriteString(text)
	}
	if importAt == 1 {
		b.WriteString("<Import Project=\"other.props\" />\n")
	}
	b.WriteString("</Project>")
	return b.String(), model
}

// fuzzCases is the oracle fuzz size. ESHU_PROPS_FUZZ_CASES raises it for a
// one-off deep run (for example 30000); the default keeps the test fast.
func fuzzCases(t *testing.T) int {
	t.Helper()
	if raw := os.Getenv("ESHU_PROPS_FUZZ_CASES"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			t.Fatalf("ESHU_PROPS_FUZZ_CASES = %q, want a positive integer", raw)
		}
		return n
	}
	return 3000
}

func TestParseCentralPropsMatchesIndependentOracle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Directory.Build.props")
	r := rand.New(rand.NewSource(8824))
	tally := map[string]int{}
	cases := fuzzCases(t)
	for caseNumber := 0; caseNumber < cases; caseNumber++ {
		doc, model := fuzzDocument(r)
		if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
		payload, err := nuget.Parse(path, false, shared.Options{})
		if err != nil {
			t.Fatalf("case %d: Parse error %v\n%s", caseNumber, err, doc)
		}
		checkFuzzCase(t, caseNumber, doc, payload, model, tally)
	}
	// Every demotion class must actually occur, or the oracle proves nothing.
	for _, reason := range []string{
		"central_management_unproven", "conditional_metadata", "duplicate_include", "exclude",
		"removed", "unevaluated_element", "unresolved_item_spec", "updated", "skipped",
		"unresolved_asset_metadata", "escaped_version", "global_development", "asset_development",
	} {
		if tally[reason] < cases/100 {
			t.Fatalf("class %s occurred %d times in %d cases, want >= %d", reason, tally[reason], cases, cases/100)
		}
	}
	t.Logf("%d cases, class tally %v", cases, tally)
}

func checkFuzzCase(t *testing.T, caseNumber int, doc string, payload map[string]any, model fuzzModel, tally map[string]int) {
	t.Helper()
	fail := func(format string, args ...any) {
		t.Helper()
		t.Fatalf("case %d: %s\n%s", caseNumber, fmt.Sprintf(format, args...), doc)
	}
	oracle := newFuzzOracle(model.decls)
	rows := variableRows(t, payload)
	next, skipped := 0, 0
	for p, it := range model.items {
		if !it.valid {
			skipped++
			tally["skipped"]++
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
		reasons := fuzzReasons(model, oracle, p)
		for _, reason := range strings.Split(reasons, ",") {
			tally[reason]++
		}
		if got, _ := row["item_ambiguity"].(string); got != reasons || hasKey(row, "item_ambiguity") != (reasons != "") {
			fail("%s: item_ambiguity = %q, want %q", it.name, got, reasons)
		}
		conditional := it.condition || it.group
		wantPartial := conditional || reasons != "" || (wantEvidence != "package_reference" && wantEvidence != "project_property")
		if got, _ := row["partial_evidence"].(bool); got != wantPartial {
			fail("%s: partial_evidence = %v, want %v", it.name, got, wantPartial)
		}
		if got, _ := row["conditional_declaration"].(bool); got != conditional {
			fail("%s: conditional_declaration = %v, want %v", it.name, got, conditional)
		}
		if (it.kind == "PackageVersion") == hasKey(row, "direct_dependency") {
			fail("%s: direct_dependency presence wrong for %s", it.name, it.kind)
		}
		if msg := checkFuzzScope(it, row, tally); msg != "" {
			fail("%s: %s", it.name, msg)
		}
	}
	for _, d := range model.decls {
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
		case strings.Contains(d.value, "$(") || fuzzEscaped(d.value):
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
		doc, _ := fuzzDocument(r)
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
