// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestResolveOwnedPackageNamesPinsCacheDescribeOnEveryStatement pins the pgx
// exec mode of each statement the resolver sends. The by-name seek, the binary
// artifact read and the PyPI prefix read bind a name array whose cost depends
// on a custom plan; the swift prefix read binds only $1, the package_manager
// alias set. Under the pool default a statement goes generic at its sixth
// execution and the planner leaves the folded index (#9586). The evidence
// note's Scale figures hold only in this mode, so a statement that drops it
// must fail here.
func TestResolveOwnedPackageNamesPinsCacheDescribeOnEveryStatement(t *testing.T) {
	t.Parallel()
	parts, err := ownedResolverPartsOnce()
	if err != nil {
		t.Fatalf("ownedResolverPartsOnce: %v", err)
	}
	cases := []struct {
		name   string
		lookup OwnedPackageNameLookup
		want   []string
	}{
		{
			"by-name seek",
			OwnedPackageNameLookup{Ecosystem: "npm", Name: "left-pad"},
			[]string{ownedResolverByNameQuery(parts)},
		},
		{
			"by-name seek and binary artifact",
			OwnedPackageNameLookup{Ecosystem: "maven", Name: "org.typelevel:cats-core"},
			[]string{ownedResolverByNameQuery(parts), ownedResolverBinaryArtifactQuery(parts)},
		},
		{
			"pypi prefix read",
			OwnedPackageNameLookup{Ecosystem: "pypi", Name: "Foo_Bar"},
			[]string{ownedResolverPyPIQuery(parts)},
		},
		{
			"swift prefix read",
			OwnedPackageNameLookup{Ecosystem: "swift", Name: "github.com/apple/swift-nio"},
			[]string{ownedResolverSwiftQuery(parts)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := &fakeExecQueryer{queryResponses: make([]queueFakeRows, len(tc.want))}
			if _, err := NewFactStore(db).ResolveOwnedPackageNames(
				context.Background(), []OwnedPackageNameLookup{tc.lookup},
			); err != nil {
				t.Fatalf("ResolveOwnedPackageNames: %v", err)
			}
			if got, want := len(db.queries), len(tc.want); got != want {
				t.Fatalf("statements = %d, want %d", got, want)
			}
			for i, call := range db.queries {
				if call.query != tc.want[i] {
					t.Errorf("statement %d is not the expected resolver statement", i+1)
				}
				if !call.hasExecMode || call.execMode != pgx.QueryExecModeCacheDescribe {
					t.Errorf("statement %d exec mode = %v (set %v), want QueryExecModeCacheDescribe",
						i+1, call.execMode, call.hasExecMode)
				}
			}
		})
	}
}

// TestGroupOwnedLookupsCanonicalizesAndSkipsBlankNames pins the lookup a
// caller sees echoed on a target: the owned ecosystem spelling and a name
// trimmed of spaces, with a blank name skipped and its ecosystem left out of
// the read order.
func TestGroupOwnedLookupsCanonicalizesAndSkipsBlankNames(t *testing.T) {
	t.Parallel()
	groups, order, err := groupOwnedLookups([]OwnedPackageNameLookup{
		{Ecosystem: "cargo", Name: "   "},
		{Ecosystem: "gomod", Name: "  github.com/Foo/Bar "},
		{Ecosystem: "go", Name: ""},
	})
	if err != nil {
		t.Fatalf("groupOwnedLookups: %v", err)
	}
	if want := []string{"go"}; !slices.Equal(order, want) {
		t.Fatalf("order = %v, want %v (a blank name must not open an ecosystem)", order, want)
	}
	got := groups["go"]
	if len(got) != 1 {
		t.Fatalf("go lookups = %+v, want one (the blank name is skipped)", got)
	}
	if want := (OwnedPackageNameLookup{Ecosystem: "go", Name: "github.com/Foo/Bar"}); got[0].lookup != want {
		t.Errorf("lookup = %+v, want %+v", got[0].lookup, want)
	}
}

// TestMatchOwnedResolverRowsSortsTargetsAndKeepsTheirLookup pins the order the
// result type documents (lookup name, ecosystem, entity name, version) for rows
// that arrive unsorted, and that each target carries the lookup it matched.
func TestMatchOwnedResolverRowsSortsTargetsAndKeepsTheirLookup(t *testing.T) {
	t.Parallel()
	spellings := []ownedLookupSpelling{
		{lookup: OwnedPackageNameLookup{Ecosystem: "npm", Name: "zeta"}, forms: []string{"zeta"}},
		{lookup: OwnedPackageNameLookup{Ecosystem: "npm", Name: "alpha"}, forms: []string{"alpha"}},
	}
	byForm := map[string][]int{"zeta": {0}, "alpha": {1}}
	rows := []ownedResolverRow{
		{ecosystem: "npm", name: "zeta", version: "2.0.0"},
		{ecosystem: "npm", name: "alpha", version: "9.0.0"},
		{ecosystem: "npm", name: "zeta", version: "1.0.0"},
		{ecosystem: "npm", name: "Alpha", version: "1.0.0"},
	}
	got := matchOwnedResolverRows(spellings, byForm, rows)
	want := []string{
		"alpha|npm|Alpha@1.0.0", "alpha|npm|alpha@9.0.0",
		"zeta|npm|zeta@1.0.0", "zeta|npm|zeta@2.0.0",
	}
	var members []string
	for _, target := range got {
		if target.Lookup.Ecosystem != "npm" {
			t.Errorf("target %s@%s lookup = %+v, want the lookup it matched", target.EntityName, target.Version, target.Lookup)
		}
		members = append(members, target.Lookup.Name+"|"+target.Ecosystem+"|"+target.EntityName+"@"+target.Version)
	}
	if !slices.Equal(members, want) {
		t.Errorf("targets = %v, want %v", members, want)
	}
}
