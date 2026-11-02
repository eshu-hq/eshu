// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

// resolverFixtureRow is one dependency fact of the resolver differential
// fixture.
type resolverFixtureRow struct {
	scope, generation, pm, name, version string
	extra                                string // extra entity_metadata JSON members
	tombstone                            bool
}

func (r resolverFixtureRow) sql(id int) string {
	metadata := fmt.Sprintf(`{"config_kind":"dependency","package_manager":%q,"value":%q,"lockfile":"true"`, r.pm, r.version)
	if r.extra != "" {
		metadata += "," + r.extra
	}
	metadata += "}"
	payload := fmt.Sprintf(`{"entity_type":"Variable","entity_name":%q,"repo_id":%q,"entity_metadata":%s}`, r.name, r.scope, metadata)
	return fmt.Sprintf(`('res-%d','%s','%s','content_entity','stable-res-%d','git','git','res-%d','2026-10-28T12:00:00Z',now(),%t,'%s'::jsonb)`,
		id, r.scope, r.generation, id, id, r.tombstone, strings.ReplaceAll(payload, "'", "''"))
}

// resolverSwiftFeedNames pairs the host/path name the production planner emits
// for a fixture swift location (case-preserved path, no scheme or ".git") with the name the OSV SwiftURL export spells for that
// package. Every value but the two github.com/upper rows is a literal from that
// export, whose 33 names are 30 host/path with no scheme and no ".git" and 3
// bare package names. The two upper rows pin a stored upper-case ".GIT".
var resolverSwiftFeedNames = map[string]string{
	"github.com/apple/swift-nio":         "github.com/apple/swift-nio",
	"github.com/pytorch/executorch":      "github.com/pytorch/executorch",
	"github.com/vapor/vapor":             "github.com/vapor/vapor",
	"github.com/grpc/grpc-swift":         "github.com/grpc/grpc-swift",
	"github.com/marmelroy/zip":           "github.com/marmelroy/Zip",
	"github.com/migueldeicaza/SwiftTerm": "github.com/migueldeicaza/SwiftTerm",
	"github.com/apple/swift-crypto":      "swift-crypto",
	"github.com/emqx/CocoaMQTT":          "CocoaMQTT",
	"github.com/upper/Case":              "github.com/upper/Case",
	"github.com/upper/Mixed":             "github.com/upper/Mixed",
}

func resolverFixtureRows(t *testing.T) []resolverFixtureRow {
	t.Helper()
	a := func(pm, name, version, extra string) resolverFixtureRow {
		return resolverFixtureRow{scope: "scope-res-a", generation: "gen-res-a2", pm: pm, name: name, version: version, extra: extra}
	}
	rows := []resolverFixtureRow{
		a("npm", "@Scope/Pkg", "1.0.0", ""), // npm scope, mixed-case stored
		a("npm", "left-pad", "1.3.0", ""),
		a("npm", "left-pad", "1.2.0", ""), // a second version is a second target
		a("npm", "CaseNpm", "2.0.0", ""),
		a("pypi", "PyYAML", "6.0.1", ""),
		a("pypi", "Foo_Bar", "1.0.0", ""),  // the feed spells it foo-bar
		a("pypi", "foo__bar", "1.0.1", ""), // a separator run folds onto foo-bar too
		a("pypi", "foo--bar", "1.0.2", ""),
		a("pypi", "foo.bar-baz", "2.0.0", ""),
		a("pypi", "Zope..Interface", "5.0.0", ""),
		a("pypi", "a.b_c-d.e_f-g", "1.0.0", ""), // six mixed separators
		a("pypi", "requests", "2.31.0", ""),
		a("maven", "org.apache:commons", "3.0", ""),
		a("maven", "a.first:hit", "1.0", ""),     // sorts into the first name chunk
		a("gradle", "org.gradle:Lib", "1.0", ""), // alias folds onto maven
		a("sbt", "com.x:plain", "1.0", `"sbt_cross_version":"none"`),
		a("sbt", "com.x:lib", "2.0", `"sbt_cross_version":"binary","sbt_binary_artifact":"com.x:lib_2.13"`),
		// A %% row whose artifact sorts into the last name chunk.
		a("sbt", "z.last:lib", "1.0", `"sbt_cross_version":"binary","sbt_binary_artifact":"z.last:lib_2.13"`),
		a("sbt", "com.x:unknown", "3.0", ""), // no cross version: names nothing
		a("sbt", "com.x:ranged", "[1.0,2.0)", `"sbt_cross_version":"none"`),
		a("go", "github.com/Foo/Bar", "v1.2.3", ""),
		a("gomod", "github.com/foo/bar", "v1.2.3", ""), // case variant, a distinct target
		a("go", "github.com/other/mod", "v0.1.0", ""),
		a("go", "github.com/unprefixed/mod", "1.2.3", ""), // no "v": the planner refuses it
		a("nuget", "Newtonsoft.Json", "13.0.1", ""),
		// The parser never writes these three swift rows. They pin what the
		// resolver does with a row the reader could still return: the first
		// shares an identity with a parsed row, so DISTINCT ON must keep the
		// located one; the planner queries no name for the other two.
		a("swift", "github.com/apple/swift-nio", "2.0.0", ""),
		a("swift", "local.example/tools/local-only", "1.0.0", `"source_location":"file:///tmp/local-only"`),
		a("swift", "local.example/tools/no-location", "1.0.0", ""),
		// The planner queries neither location: a dotless host has no repository
		// key, and a scheme-less "host/path" is a feed spelling. A resolver that
		// matched the raw location would resolve both.
		a("swift", "localhost/a/b", "1.0.0", `"source_location":"https://localhost/a/b"`),
		a("swift", "github.com/x/y", "1.0.0", `"source_location":"github.com/x/y"`),
		a("npm", "lib_${var}", "1.0.0", ""),  // unexpanded variable: the derivation skips it
		a("npm", "range-only", "^1.0.0", ""), // non-exact version: the derivation skips it
		a("npm", "latest-only", "latest", ""),
		a("npm", "mixed-versions", "1.0.0", ""), // one exact row beside one range row
		a("npm", "mixed-versions", "~2.0.0", ""),
		{scope: "scope-res-a", generation: "gen-res-a2", pm: "npm", name: "tombstoned", version: "1.0.0", tombstone: true},
		// A superseded generation, a scope with no active generation: neither
		// may ever resolve.
		{scope: "scope-res-a", generation: "gen-res-a1", pm: "npm", name: "stale-only", version: "9.9.9"},
		{scope: "scope-res-b", generation: "gen-res-b1", pm: "npm", name: "inactive-scope", version: "1.0.0"},
		{scope: "scope-res-c", generation: "gen-res-c1", pm: "npm", name: "left-pad", version: "1.3.0"}, // same identity, second repo
	}
	return append(rows, resolverParsedSwiftRows(t, "scope-res-a", "gen-res-a2", resolverSwiftPins)...)
}

const resolverFixtureScopes = `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ('scope-res-a','repository','git','repo-res-a','git','repository:res-a',now(),now(),'active','gen-res-a2'),
       ('scope-res-b','repository','git','repo-res-b','git','repository:res-b',now(),now(),'active',NULL),
       ('scope-res-c','repository','git','repo-res-c','git','repository:res-c',now(),now(),'active','gen-res-c1');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('gen-res-a1','scope-res-a','snapshot',now(),now(),'superseded'),
       ('gen-res-a2','scope-res-a','snapshot',now(),now(),'active'),
       ('gen-res-b1','scope-res-b','snapshot',now(),now(),'pending'),
       ('gen-res-c1','scope-res-c','snapshot',now(),now(),'active');
`

func resolverFixtureSQL(t *testing.T) string {
	t.Helper()
	return resolverSeedSQL(resolverFixtureRows(t))
}

func resolverSeedSQL(rows []resolverFixtureRow) string {
	values := make([]string, 0, len(rows))
	for i, row := range rows {
		values = append(values, row.sql(i))
	}
	return resolverFixtureScopes + `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
    source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ` + strings.Join(values, ",\n") + ";"
}

var osvSeparatorRun = regexp.MustCompile(`[-_.]+`)

// osvSpelling is how the advisory feed spells a planned name: ASCII-lowered,
// PyPI collapsed to the PEP 503 form, swift from the literal feed table. A
// planned swift name with no table entry returns "", which fails the caller.
func osvSpelling(ecosystem, name string) string {
	if ecosystem == "swift" {
		return resolverSwiftFeedNames[name]
	}
	folded := packageidentity.LowerASCII(name)
	if ecosystem == "pypi" {
		return osvSeparatorRun.ReplaceAllString(folded, "-")
	}
	return folded
}

// resolutionMembers flattens a resolution into comparable members, one per
// target and one per partial lookup, sorted. A duplicate stays in, because the
// resolver promises one target per distinct identity.
func resolutionMembers(resolution OwnedPackageResolution) []string {
	members := make([]string, 0, len(resolution.Targets)+len(resolution.Partial))
	for _, target := range resolution.Targets {
		members = append(members, fmt.Sprintf("target|%s|%s|%s|%s|%s|%d|%d",
			target.Lookup.Ecosystem, target.Lookup.Name, target.Ecosystem, target.EntityName, target.Version,
			target.KeyHashLow24, target.VirtualSlot))
	}
	for _, lookup := range resolution.Partial {
		members = append(members, "partial|"+lookup.Ecosystem+"|"+lookup.Name)
	}
	slices.Sort(members)
	return members
}

// assertResolutionMembers compares a batch with the deduplicated union of its
// single calls, which repeat a lookup per planned version. The batch is not
// deduplicated: a repeated member there is a defect.
func assertResolutionMembers(t *testing.T, label string, got OwnedPackageResolution, want []string) {
	t.Helper()
	slices.Sort(want)
	want = slices.Compact(want)
	if members := resolutionMembers(got); !slices.Equal(members, want) {
		t.Fatalf("%s members differ from the single calls\n got %d: %v\nwant %d: %v", label, len(members), members, len(want), want)
	}
}

func resolvedEntities(resolution OwnedPackageResolution) []string {
	entities := make([]string, 0, len(resolution.Targets))
	for _, target := range resolution.Targets {
		entities = append(entities, target.EntityName+"@"+target.Version)
	}
	slices.Sort(entities)
	return entities
}
