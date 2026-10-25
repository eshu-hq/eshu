// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget_test

import (
	"math/rand"
	"regexp"
	"strings"
)

// The asset half of the oracle fuzz. The generator declares PrivateAssets and
// IncludeAssets on an item; the oracle states the dependency_scope NuGet gives
// it from the model alone, never from the parser's code.

var (
	fuzzPrivateValues = []string{"", "All", "all", "none", "Compile", "$(P)", "%41ll", "contentFiles;Analyzers"}
	fuzzIncludeValues = []string{"", "none", "None", "Runtime", "$(I)", "%6Eone"}
	fuzzEscapePattern = regexp.MustCompile(`%[0-9A-Fa-f]{2}`)
)

// fuzzEscaped reports an MSBuild %XX escape, which MSBuild unescapes before
// it uses a value.
func fuzzEscaped(text string) bool {
	return fuzzEscapePattern.MatchString(text)
}

// fuzzAssets declares each asset value as an attribute or as a child element,
// never both, and sometimes conditions the child.
func fuzzAssets(r *rand.Rand, it *fuzzItem) (attrs, inner string) {
	render := func(name, value string, cond *bool) {
		if value == "" {
			return
		}
		if r.Intn(2) == 0 {
			attrs += " " + name + `="` + value + `"`
			return
		}
		condition := ""
		if r.Intn(3) == 0 {
			*cond, condition = true, ` Condition="'$(A)' == 'a'"`
		}
		inner += "<" + name + condition + ">" + value + "</" + name + ">"
	}
	it.private = fuzzPrivateValues[r.Intn(len(fuzzPrivateValues))]
	it.include = fuzzIncludeValues[r.Intn(len(fuzzIncludeValues))]
	render("PrivateAssets", it.private, &it.privateCond)
	render("IncludeAssets", it.include, &it.includeCond)
	return attrs, inner
}

// fuzzProven reports whether an asset value is unconditional and needs no
// MSBuild evaluation, so it can decide a scope.
func fuzzProven(value string, conditional bool) bool {
	return !conditional && !strings.Contains(value, "$(") && !strings.Contains(value, "@(") &&
		!strings.Contains(value, "%(") && !fuzzEscaped(value)
}

func fuzzAssetHas(value, token string) bool {
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ';' || r == ',' || r == ' ' }) {
		if strings.EqualFold(part, token) {
			return true
		}
	}
	return false
}

// fuzzScope is the dependency_scope NuGet gives the item, or "" when the
// asset metadata that decides it is unproven. NuGet.targets builds a
// PackageReference from each GlobalPackageReference with PrivateAssets="All"
// set on the element, which wins over the item's own metadata.
func fuzzScope(it fuzzItem) string {
	name := strings.ToLower(it.name)
	if strings.Contains(name, "test") {
		return "test"
	}
	if it.kind == "GlobalPackageReference" {
		return "development"
	}
	privateProven := fuzzProven(it.private, it.privateCond)
	includeProven := fuzzProven(it.include, it.includeCond)
	switch {
	case privateProven && fuzzAssetHas(it.private, "all"):
		return "development"
	case includeProven && fuzzAssetHas(it.include, "none"):
		return "development"
	case !privateProven || !includeProven:
		return ""
	}
	return "runtime"
}

// checkFuzzScope compares the row's scope fields with the oracle and returns
// a failure message, or "" when they agree.
func checkFuzzScope(it fuzzItem, row map[string]any, tally map[string]int) string {
	want := fuzzScope(it)
	switch {
	case want == "development" && it.kind == "GlobalPackageReference":
		tally["global_development"]++
	case want == "development":
		tally["asset_development"]++
	}
	for _, part := range append(append([]fuzzPart{}, it.version...), it.override...) {
		if part.kind == 5 {
			tally["escaped_version"]++
			break
		}
	}
	got, present := row["dependency_scope"].(string)
	if want == "" && present {
		return "dependency_scope = " + got + ", want absent (unproven asset metadata)"
	}
	if want != "" && got != want {
		return "dependency_scope = " + got + ", want " + want
	}
	if dev := want == "development" || want == "test"; hasKey(row, "development_dependency") != dev {
		return "development_dependency presence wrong for scope " + want
	}
	return ""
}
