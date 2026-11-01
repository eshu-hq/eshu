// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"slices"
	"strings"
	"testing"
)

func TestOwnedNameLookupFormsFoldLikeTheIndexExpression(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		ecosystem string
		input     string
		want      []string
	}{
		{"npm scope keeps its slash and folds ASCII case", "npm", "@Types/Node", []string{"@types/node"}},
		{"go module path folds case", "go", "GitHub.com/Foo/Bar", []string{"github.com/foo/bar"}},
		{"gomod alias resolves to the go fold", "gomod", "GitHub.com/Foo/Bar", []string{"github.com/foo/bar"}},
		{"maven coordinate adds the slash spelling", "maven", "Org.Foo:Bar", []string{"org.foo/bar", "org.foo:bar"}},
		{"nuget folds case only", "nuget", "Newtonsoft.Json", []string{"newtonsoft.json"}},
		{"surrounding spaces are trimmed like TRIM", "npm", "  left-pad ", []string{"left-pad"}},
		{"non ASCII bytes are left alone like COLLATE C", "npm", "ÀB", []string{"Àb"}},
	}
	for _, tc := range cases {
		got, partial := ownedNameLookupForms(tc.ecosystem, tc.input)
		slices.Sort(got)
		if !slices.Equal(got, tc.want) || partial {
			t.Errorf("%s: forms(%q, %q) = %v partial=%v, want %v partial=false",
				tc.name, tc.ecosystem, tc.input, got, partial, tc.want)
		}
	}
}

func TestOwnedNameLookupFormsPyPINormalizesEveryRun(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ input, want string }{
		{"foo-bar", "foo-bar"},
		{"Foo_Bar", "foo-bar"},
		{"foo__bar", "foo-bar"},
		{"foo.-_bar", "foo-bar"},
		{"a-b-c-d-e-f-g", "a-b-c-d-e-f-g"},
		{"a.b_c-d.e_f-g", "a-b-c-d-e-f-g"},
		{"requests", "requests"},
	} {
		got, partial := ownedNameLookupForms("pypi", tc.input)
		if !slices.Equal(got, []string{tc.want}) || partial {
			t.Errorf("forms(pypi, %q) = %v partial=%v, want [%s] partial=false", tc.input, got, partial, tc.want)
		}
	}
}

func TestOwnedNameLookupFormsSwiftUsesTheRepositoryKey(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input, want string
		partial     bool
	}{
		{"github.com/marmelroy/Zip", "github.com/marmelroy/zip", false},
		{"https://github.com/apple/swift-nio.git", "github.com/apple/swift-nio", false},
		{"git@github.com:vapor/vapor.git", "github.com/vapor/vapor", false},
		{"swift-crypto", "swift-crypto", true},
		{"CocoaMQTT", "cocoamqtt", true},
	} {
		got, partial := ownedNameLookupForms("swift", tc.input)
		if !slices.Equal(got, []string{tc.want}) || partial != tc.partial {
			t.Errorf("forms(swift, %q) = %v partial=%v, want [%s] partial=%v", tc.input, got, partial, tc.want, tc.partial)
		}
	}
}

func TestGroupOwnedLookupsRefusesAnEcosystemOutsideTheOwnedSet(t *testing.T) {
	t.Parallel()
	for _, ecosystem := range []string{"", "crates.io", "Packagist", "SwiftURL", "mystery", "sbt"} {
		_, _, err := groupOwnedLookups([]OwnedPackageNameLookup{{Ecosystem: ecosystem, Name: "serde"}})
		if err == nil {
			t.Errorf("groupOwnedLookups(%q) returned no error", ecosystem)
			continue
		}
		// The span records this error, and a span never carries a package name.
		if strings.Contains(err.Error(), "serde") {
			t.Errorf("groupOwnedLookups(%q) error names the package: %v", ecosystem, err)
		}
	}
	groups, order, err := groupOwnedLookups([]OwnedPackageNameLookup{
		{Ecosystem: "GOMOD", Name: "github.com/foo/bar"}, {Ecosystem: "gradle", Name: "a:b"}, {Ecosystem: "cargo", Name: "serde"},
	})
	if err != nil {
		t.Fatalf("groupOwnedLookups: %v", err)
	}
	if want := []string{"go", "maven", "cargo"}; !slices.Equal(order, want) || len(groups) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}
