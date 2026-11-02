// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9777 PostgreSQL 18.6 differential proof that the owned-name resolver returns, for every query the production vulnerability planner puts in a pinned slot scope (npm scopes, PyPI separators, Go case variants, Maven coordinates, Gradle and sbt rows, swift source locations), the same key_hash_low24 and virtual slot the owned-package reader and planner assigned, from the folded entity index and the reader's own active-generation CTE text; isolated bootstrap schema
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// plannedSlotQuery is one query the production planner placed in a pinned
// slot scope, with the virtual slot its scope id carries.
type plannedSlotQuery struct {
	Ecosystem   string `json:"ecosystem"`
	PackageName string `json:"package_name"`
	Version     string `json:"version"`
	slot        uint32
}

var slotScopeIDPattern = regexp.MustCompile(`/_slot/(\d+)/\d+$`)

// planSlotQueries runs the real planner over the reader's page and returns
// every planned query with its pinned virtual slot.
func planSlotQueries(t *testing.T, page workflow.OwnedPackageDependencyTargetPage, ecosystems []string) []plannedSlotQuery {
	t.Helper()
	observedAt := time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)
	ecosystemJSON, err := json.Marshal(ecosystems)
	if err != nil {
		t.Fatalf("marshal ecosystems: %v", err)
	}
	instance := workflow.CollectorInstance{
		InstanceID: "collector-vulnerability-intelligence", CollectorKind: scope.CollectorVulnerabilityIntelligence,
		Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true,
		Configuration: `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":` + string(ecosystemJSON) +
			`,"planning_mode":"single_pass","target_limit":5000,"slot_scopes":true}}`,
		LastObservedAt: observedAt, CreatedAt: observedAt, UpdatedAt: observedAt,
	}
	run, _, err := vulnerability.IntelligenceWorkPlanner{}.PlanVulnerabilityIntelligenceWork(context.Background(), vulnerability.IntelligencePlanRequest{
		Instance: instance, ObservedAt: observedAt, PlanKey: "continuous-single-pass",
		OwnedPackageTargets: page.Targets, OwnedPackageWindow: page.Window,
	})
	if err != nil {
		t.Fatalf("PlanVulnerabilityIntelligenceWork: %v", err)
	}
	var decoded struct {
		Targets []struct {
			ScopeID string             `json:"scope_id"`
			Queries []plannedSlotQuery `json:"queries"`
		} `json:"targets"`
	}
	if err := json.Unmarshal([]byte(run.RequestedScopeSet), &decoded); err != nil {
		t.Fatalf("decode requested_scope_set: %v", err)
	}
	var planned []plannedSlotQuery
	for _, target := range decoded.Targets {
		match := slotScopeIDPattern.FindStringSubmatch(target.ScopeID)
		if match == nil {
			t.Fatalf("planned scope %q is not a pinned slot scope; the differential needs slot scopes", target.ScopeID)
		}
		slot, _ := strconv.ParseUint(match[1], 10, 32)
		for _, query := range target.Queries {
			query.slot = uint32(slot)
			planned = append(planned, query)
		}
	}
	return planned
}

func identityKey(ecosystem, name, version string) string {
	return ecosystem + "|" + name + "|" + version
}

// TestResolveOwnedPackageNamesDifferentialLive proves the resolver agrees
// with the production reader and planner: for every query the planner pinned
// to a virtual slot, resolving that query's name (as planned and as the feed
// would spell it) returns the target with the reader's key_hash_low24 and the
// planner's virtual slot, and it never returns an identity the reader does
// not (stale generation, inactive scope, tombstone). One batch call returns
// exactly the union of the single calls.
func TestResolveOwnedPackageNamesDifferentialLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "owned_resolver_9777", "ESHU_POSTGRES_DSN is not set")
	ctx, cancel := context.WithTimeout(ctx, 80*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, resolverFixtureSQL(t)); err != nil {
		t.Fatalf("seed resolver fixture: %v", err)
	}
	store := NewFactStore(SQLDB{DB: db})
	ecosystems := []string{"npm", "pypi", "maven", "go", "nuget", "swift"}
	page, err := store.ListOwnedPackageDependencyTargets(ctx, workflow.OwnedPackageDependencyTargetFilter{
		Ecosystems: ecosystems, Limit: 5000, TargetLimit: 5000, VersionSpecific: true,
		ExpandPackageManagerAliases: true, IncludeSBTOwnedTargets: true,
	})
	if err != nil {
		t.Fatalf("reader: %v", err)
	}
	readerHash := make(map[string]uint32, len(page.Targets))
	for _, target := range page.Targets {
		readerHash[identityKey(target.Ecosystem, target.PackageName, target.Version)] = target.KeyHashLow24
	}
	for _, absent := range []string{"stale-only", "inactive-scope", "tombstoned"} {
		for key := range readerHash {
			if strings.Contains(key, absent) {
				t.Fatalf("fixture error: reader returned %q", key)
			}
		}
	}

	planned := planSlotQueries(t, page, ecosystems)
	for _, must := range []string{
		identityKey("npm", "@Scope/Pkg", "1.0.0"), identityKey("pypi", "Foo_Bar", "1.0.0"),
		identityKey("pypi", "foo.bar-baz", "2.0.0"), identityKey("maven", "org.gradle:Lib", "1.0"),
		identityKey("maven", "com.x:plain", "1.0"), identityKey("maven", "com.x:lib_2.13", "2.0"),
		identityKey("go", "github.com/Foo/Bar", "v1.2.3"), identityKey("go", "github.com/foo/bar", "v1.2.3"),
		identityKey("nuget", "Newtonsoft.Json", "13.0.1"),
		identityKey("swift", "github.com/apple/swift-nio", "2.0.0"),
		identityKey("swift", "github.com/pytorch/executorch", "0.1.0"),
		identityKey("swift", "github.com/vapor/vapor", "4.0.0"),
		identityKey("swift", "github.com/grpc/grpc-swift", "1.0.0"),
		identityKey("swift", "github.com/upper/Case", "1.0.0"),
		identityKey("pypi", "a.b_c-d.e_f-g", "1.0.0"), identityKey("pypi", "foo__bar", "1.0.1"),
	} {
		if !slices.ContainsFunc(planned, func(q plannedSlotQuery) bool { return identityKey(q.Ecosystem, q.PackageName, q.Version) == must }) {
			t.Fatalf("planner did not pin %q; the differential would be vacuous. planned=%v", must, planned)
		}
	}

	var all []OwnedPackageNameLookup
	var union []string
	for _, query := range planned {
		feed := osvSpelling(query.Ecosystem, query.PackageName)
		if feed == "" {
			t.Fatalf("no feed spelling for planned %s %q", query.Ecosystem, query.PackageName)
		}
		for _, spelling := range slices.Compact([]string{query.PackageName, feed}) {
			lookup := OwnedPackageNameLookup{Ecosystem: query.Ecosystem, Name: spelling}
			all = append(all, lookup)
			resolution, err := store.ResolveOwnedPackageNames(ctx, []OwnedPackageNameLookup{lookup})
			if err != nil {
				t.Fatalf("resolve %+v: %v", lookup, err)
			}
			union = append(union, resolutionMembers(resolution)...)
			found := false
			for _, target := range resolution.Targets {
				want, ok := readerHash[identityKey(target.Ecosystem, target.EntityName, target.Version)]
				if !ok {
					t.Errorf("resolve %+v returned %s|%s|%s, which the reader does not return", lookup, target.Ecosystem, target.EntityName, target.Version)
					continue
				}
				if target.KeyHashLow24 != want {
					t.Errorf("resolve %+v: %s|%s|%s key_hash_low24 = %d, reader has %d", lookup, target.Ecosystem, target.EntityName, target.Version, target.KeyHashLow24, want)
				}
				if target.Lookup != lookup {
					t.Errorf("resolve %+v: target %s|%s|%s echoes lookup %+v, want the lookup it was sent as", lookup, target.Ecosystem, target.EntityName, target.Version, target.Lookup)
				}
				if target.VirtualSlot != target.KeyHashLow24%1024 {
					t.Errorf("resolve %+v: virtual slot %d is not key_hash_low24 %d mod 1024", lookup, target.VirtualSlot, target.KeyHashLow24)
				}
				if target.Version == query.Version && target.VirtualSlot == query.slot {
					found = true
				}
			}
			if !found {
				t.Errorf("resolve %+v found no target for version %q in planner slot %d; got %+v", lookup, query.Version, query.slot, resolution.Targets)
			}
		}
	}

	batch, err := store.ResolveOwnedPackageNames(ctx, all)
	if err != nil {
		t.Fatalf("batch resolve: %v", err)
	}
	assertResolutionMembers(t, "batch", batch, union)
	for _, target := range batch.Targets {
		if !slices.Contains(all, target.Lookup) {
			t.Errorf("batch target %s|%s|%s echoes lookup %+v, which no sent lookup canonicalizes to", target.Ecosystem, target.EntityName, target.Version, target.Lookup)
		}
	}

	// A caller keyed on a reader alias or a padded name finds its targets under
	// the canonical lookup the resolver documents: "gomod" comes back as "go".
	aliased, err := store.ResolveOwnedPackageNames(ctx, []OwnedPackageNameLookup{
		{Ecosystem: "gomod", Name: "  github.com/foo/bar "},
	})
	if err != nil {
		t.Fatalf("resolve gomod alias: %v", err)
	}
	if len(aliased.Targets) == 0 {
		t.Fatal("gomod alias resolved no targets; the canonical-lookup check would be vacuous")
	}
	for _, target := range aliased.Targets {
		if want := (OwnedPackageNameLookup{Ecosystem: "go", Name: "github.com/foo/bar"}); target.Lookup != want {
			t.Errorf("gomod alias target %s|%s echoes lookup %+v, want %+v", target.EntityName, target.Version, target.Lookup, want)
		}
	}

	// Rows the reader does not read must not resolve.
	none, err := store.ResolveOwnedPackageNames(ctx, []OwnedPackageNameLookup{
		{Ecosystem: "npm", Name: "stale-only"},
		{Ecosystem: "npm", Name: "inactive-scope"},
		{Ecosystem: "npm", Name: "tombstoned"},
		{Ecosystem: "npm", Name: "does-not-exist"},
	})
	if err != nil || len(none.Targets) != 0 {
		t.Fatalf("stale, inactive, tombstoned and unknown names resolved: %+v err=%v", none.Targets, err)
	}

	// Two repos declaring one identity resolve to one target per version.
	leftPad, err := store.ResolveOwnedPackageNames(ctx, []OwnedPackageNameLookup{{Ecosystem: "npm", Name: "left-pad"}})
	if err != nil || len(leftPad.Targets) != 2 {
		t.Fatalf("left-pad targets = %+v err=%v, want exactly versions 1.2.0 and 1.3.0", leftPad.Targets, err)
	}

	// The by-name read and the binary-artifact read both reach this sbt row, and
	// it still resolves to one target.
	sbt, err := store.ResolveOwnedPackageNames(ctx, []OwnedPackageNameLookup{
		{Ecosystem: "maven", Name: "com.x:lib"}, {Ecosystem: "maven", Name: "com.x:lib_2.13"},
	})
	if err != nil || len(sbt.Targets) != 1 {
		t.Fatalf("sbt row read twice = %+v err=%v, want one target", sbt.Targets, err)
	}
}

// TestResolveOwnedPackageNamesMatchesTheFeedLive pins the spellings a seek on
// the stored name cannot reach: PyPI separator runs and long mixed names fold
// on both sides, swift names compare on one repository key, and a bare swift
// feed name matches the repository's final path segment and is reported
// partial. The swift rows come from the production parser.
func TestResolveOwnedPackageNamesMatchesTheFeedLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "owned_resolver_feed_9777", "ESHU_POSTGRES_DSN is not set")
	if _, err := db.ExecContext(ctx, resolverFixtureSQL(t)); err != nil {
		t.Fatalf("seed resolver fixture: %v", err)
	}
	store := NewFactStore(SQLDB{DB: db})
	allFooBar := []string{"Foo_Bar@1.0.0", "foo--bar@1.0.2", "foo__bar@1.0.1"}
	for _, tc := range []struct {
		ecosystem, name string
		want            []string
		partial         bool
	}{
		{"pypi", "foo-bar", allFooBar, false},
		{"pypi", "foo__bar", allFooBar, false},
		{"pypi", "foo.bar", allFooBar, false},
		{"pypi", "a-b-c-d-e-f-g", []string{"a.b_c-d.e_f-g@1.0.0"}, false},
		{"pypi", "zope-interface", []string{"Zope..Interface@5.0.0"}, false},
		{"pypi", "does-not-exist-a-b-c-d-e-f", nil, false},
		{"swift", "github.com/apple/swift-nio", []string{"github.com/apple/swift-nio@2.0.0"}, false},
		{"swift", "GitHub.com/Apple/Swift-NIO", []string{"github.com/apple/swift-nio@2.0.0"}, false},
		{"swift", "https://github.com/apple/swift-nio", []string{"github.com/apple/swift-nio@2.0.0"}, false},
		{"swift", "github.com/vapor/vapor", []string{"github.com/vapor/vapor@4.0.0"}, false},
		{"swift", "github.com/grpc/grpc-swift", []string{"github.com/grpc/grpc-swift@1.0.0"}, false},
		{"swift", "github.com/marmelroy/Zip", []string{"github.com/marmelroy/zip@2.1.0"}, false},
		{"swift", "github.com/upper/Case", []string{"github.com/upper/case.git@1.0.0"}, false},
		{"swift", "github.com/upper/Mixed", []string{"github.com/upper/mixed.git@1.0.0"}, false},
		{"swift", "swift-crypto", []string{"github.com/apple/swift-crypto@3.0.0"}, true},
		{"swift", "CocoaMQTT", []string{"github.com/emqx/cocoamqtt@2.1.0"}, true},
		{"swift", "Case", []string{"github.com/upper/case.git@1.0.0"}, true}, // the segment comes from the key, not the name
		{"swift", "not-owned-anywhere", nil, true},
		{"swift", "local-only", nil, true},
		{"swift", "no-location", nil, true},
		{"swift", "github.com/nobody/nothing", nil, false},
	} {
		resolution, err := store.ResolveOwnedPackageNames(ctx, []OwnedPackageNameLookup{{Ecosystem: tc.ecosystem, Name: tc.name}})
		if err != nil {
			t.Errorf("resolve %s %q: %v", tc.ecosystem, tc.name, err)
			continue
		}
		if got := resolvedEntities(resolution); !slices.Equal(got, tc.want) {
			t.Errorf("resolve %s %q = %v, want %v", tc.ecosystem, tc.name, got, tc.want)
		}
		if partial := len(resolution.Partial) == 1; partial != tc.partial || len(resolution.Partial) > 1 {
			t.Errorf("resolve %s %q partial = %v, want %v", tc.ecosystem, tc.name, resolution.Partial, tc.partial)
		}
	}
}

// TestResolveOwnedPackageNamesFindsEverySwiftURLNameLive looks up all 33 names of
// the OSV SwiftURL export against rows the production parser wrote. Every
// host/path name finds its repository. Every bare name finds the repositories
// with that final path segment and is partial, including a fork that shares it.
func TestResolveOwnedPackageNamesFindsEverySwiftURLNameLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "owned_resolver_swift_9777", "ESHU_POSTGRES_DSN is not set")
	rows := resolverParsedSwiftRows(t, "scope-res-a", "gen-res-a2", resolverSwiftFeedPins())
	if _, err := db.ExecContext(ctx, resolverSeedSQL(rows)); err != nil {
		t.Fatalf("seed swift rows: %v", err)
	}
	store := NewFactStore(SQLDB{DB: db})
	bare := map[string][]string{
		"CocoaMQTT":       {"github.com/emqx/cocoamqtt@1.0.0"},
		"swift-crypto":    {"github.com/apple/swift-crypto@1.0.0", "github.com/forker/swift-crypto@1.0.0"},
		"swift-nio-http2": {"github.com/apple/swift-nio-http2@1.0.0"},
	}
	if len(resolverSwiftURLFeedNames) != 33 {
		t.Fatalf("feed names = %d, want the export's 33", len(resolverSwiftURLFeedNames))
	}
	for _, name := range resolverSwiftURLFeedNames {
		want, isBare := bare[name]
		if !isBare {
			want = []string{strings.ToLower(name) + "@1.0.0"}
		}
		resolution, err := store.ResolveOwnedPackageNames(ctx, []OwnedPackageNameLookup{{Ecosystem: "swift", Name: name}})
		if err != nil {
			t.Fatalf("resolve %q: %v", name, err)
		}
		if got := resolvedEntities(resolution); !slices.Equal(got, want) {
			t.Errorf("resolve %q = %v, want %v", name, got, want)
		}
		if isPartial := len(resolution.Partial) == 1; isPartial != isBare {
			t.Errorf("resolve %q partial = %v, want bare=%v", name, resolution.Partial, isBare)
		}
	}
}

// TestResolveOwnedPackageNamesFollowsThePlannerRulesLive pins that a stored row
// the vulnerability derivation would never query resolves to nothing, even
// though the name matches: a non-exact version, an unexpanded variable, an sbt
// row with no consumption key. An ecosystem outside the owned set is an error,
// never an empty answer.
func TestResolveOwnedPackageNamesFollowsThePlannerRulesLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "owned_resolver_rules_9777", "ESHU_POSTGRES_DSN is not set")
	if _, err := db.ExecContext(ctx, resolverFixtureSQL(t)); err != nil {
		t.Fatalf("seed resolver fixture: %v", err)
	}
	store := NewFactStore(SQLDB{DB: db})
	for _, lookup := range []OwnedPackageNameLookup{
		{Ecosystem: "npm", Name: "range-only"},
		{Ecosystem: "npm", Name: "latest-only"},
		{Ecosystem: "go", Name: "github.com/unprefixed/mod"},
		{Ecosystem: "maven", Name: "com.x:ranged"},
		{Ecosystem: "npm", Name: "lib_${var}"},
		{Ecosystem: "maven", Name: "com.x:unknown"},
	} {
		resolution, err := store.ResolveOwnedPackageNames(ctx, []OwnedPackageNameLookup{lookup})
		if err != nil || len(resolution.Targets) != 0 {
			t.Errorf("resolve %+v = %+v err=%v, want no targets", lookup, resolution.Targets, err)
		}
	}
	mixed, err := store.ResolveOwnedPackageNames(ctx, []OwnedPackageNameLookup{{Ecosystem: "npm", Name: "mixed-versions"}})
	if err != nil || !slices.Equal(resolvedEntities(mixed), []string{"mixed-versions@1.0.0"}) {
		t.Errorf("mixed-versions = %v err=%v, want only the exact version", resolvedEntities(mixed), err)
	}
	for _, name := range []string{"localhost/a/b", "github.com/x/y"} { // rows the planner never queries
		resolution, err := store.ResolveOwnedPackageNames(ctx, []OwnedPackageNameLookup{{Ecosystem: "swift", Name: name}})
		if err != nil || len(resolution.Targets) != 0 {
			t.Errorf("resolve swift %q = %+v err=%v, want no targets", name, resolution.Targets, err)
		}
	}
	for _, ecosystem := range []string{"crates.io", "Packagist", "SwiftURL", "mystery", "sbt"} {
		if _, err := store.ResolveOwnedPackageNames(ctx, []OwnedPackageNameLookup{{Ecosystem: ecosystem, Name: "serde"}}); err == nil {
			t.Errorf("ecosystem %q resolved without an error", ecosystem)
		}
	}
	for _, ecosystem := range []string{"cargo", "composer", "go", "gomod", "gradle", "hex", "maven", "npm", "nuget", "pub", "pypi", "rubygems", "swift"} {
		if _, err := store.ResolveOwnedPackageNames(ctx, []OwnedPackageNameLookup{{Ecosystem: ecosystem, Name: "serde"}}); err != nil {
			t.Errorf("owned ecosystem %q: %v", ecosystem, err)
		}
	}
}

// TestResolveOwnedPackageNamesChunksLongBatchesLive pins chunking: a batch past
// the name chunk resolves the same set as its single calls, with a by-name hit
// in the first chunk and an sbt binary-artifact hit in the last.
func TestResolveOwnedPackageNamesChunksLongBatchesLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "owned_resolver_chunk_9777", "ESHU_POSTGRES_DSN is not set")
	if _, err := db.ExecContext(ctx, resolverFixtureSQL(t)); err != nil {
		t.Fatalf("seed resolver fixture: %v", err)
	}
	store := NewFactStore(SQLDB{DB: db})
	hits := []OwnedPackageNameLookup{
		{Ecosystem: "maven", Name: "a.first:hit"},
		{Ecosystem: "maven", Name: "com.x:lib_2.13"},
		{Ecosystem: "maven", Name: "z.last:lib_2.13"},
		{Ecosystem: "npm", Name: "left-pad"},
	}
	lookups := slices.Clone(hits)
	for i := range 2 * ownedResolverNameChunk {
		lookups = append(lookups,
			OwnedPackageNameLookup{Ecosystem: "maven", Name: fmt.Sprintf("n.filler:lib%03d", i)},
			OwnedPackageNameLookup{Ecosystem: "npm", Name: fmt.Sprintf("a-filler-%03d", i)})
	}
	var union []string
	for _, lookup := range hits {
		single, err := store.ResolveOwnedPackageNames(ctx, []OwnedPackageNameLookup{lookup})
		if err != nil {
			t.Fatalf("resolve %+v: %v", lookup, err)
		}
		if len(single.Targets) == 0 {
			t.Fatalf("resolve %+v found nothing; the chunk proof would be vacuous", lookup)
		}
		union = append(union, resolutionMembers(single)...)
	}
	batch, err := store.ResolveOwnedPackageNames(ctx, lookups)
	if err != nil {
		t.Fatalf("batch resolve: %v", err)
	}
	assertResolutionMembers(t, "chunked batch", batch, union)
	if got := resolvedEntities(batch); !slices.Contains(got, "z.last:lib@1.0") || !slices.Contains(got, "a.first:hit@1.0") {
		t.Fatalf("chunked batch = %v, want the first-chunk and last-chunk hits", got)
	}
}

// TestResolveOwnedPackageNamesSpanLive proves an operator can see a resolve
// pass: one span with counts, including the partial count, and no package name
// on it or on the error a mis-wired call records.
func TestResolveOwnedPackageNamesSpanLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "owned_resolver_span_9777", "ESHU_POSTGRES_DSN is not set")
	if _, err := db.ExecContext(ctx, resolverFixtureSQL(t)); err != nil {
		t.Fatalf("seed resolver fixture: %v", err)
	}
	recorder := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	store := NewFactStore(SQLDB{DB: db})
	resolution, err := store.ResolveOwnedPackageNames(ctx, []OwnedPackageNameLookup{
		{Ecosystem: "npm", Name: "left-pad"},
		{Ecosystem: "pypi", Name: "foo-bar"},
		{Ecosystem: "maven", Name: "com.x:lib_2.13"},
		{Ecosystem: "swift", Name: "swift-crypto"},
	})
	if err != nil || len(resolution.Targets) != 7 {
		t.Fatalf("resolve = %+v err=%v, want left-pad x2, three foo-bar spellings, com.x:lib and swift-crypto", resolution.Targets, err)
	}
	var found bool
	for _, span := range recorder.Ended() {
		if span.Name() != contract.SpanOwnedPackageResolve {
			continue
		}
		found = true
		got := map[string]int64{}
		for _, attr := range span.Attributes() {
			if strings.HasPrefix(string(attr.Key), "owned_package_resolver.") {
				got[string(attr.Key)] = attr.Value.AsInt64()
			}
			if strings.Contains(attr.Value.String(), "left-pad") {
				t.Errorf("span attribute %s carries a package name", attr.Key)
			}
		}
		want := map[string]int64{
			"owned_package_resolver.lookups": 4, "owned_package_resolver.ecosystems": 4, "owned_package_resolver.targets": 7,
			"owned_package_resolver.partial": 1,
		}
		for key, value := range want {
			if got[key] != value {
				t.Errorf("%s = %d, want %d (all: %v)", key, got[key], value, got)
			}
		}
		// npm: 1 by-name; pypi: 1 prefix read; maven: by-name + binary-artifact;
		// swift: 1 prefix read.
		if got["owned_package_resolver.statements"] != 5 || got["owned_package_resolver.rows_read"] < 7 {
			t.Errorf("statements/rows_read = %d/%d, want 5 statements and at least 7 rows", got["owned_package_resolver.statements"], got["owned_package_resolver.rows_read"])
		}
	}
	if !found {
		t.Fatalf("no %s span recorded", contract.SpanOwnedPackageResolve)
	}

	if _, err := store.ResolveOwnedPackageNames(ctx, []OwnedPackageNameLookup{{Ecosystem: "mystery", Name: "left-pad"}}); err == nil {
		t.Fatal("an ecosystem outside the owned set resolved without an error")
	}
	var exceptions int
	for _, span := range recorder.Ended() {
		for _, event := range span.Events() {
			exceptions++
			for _, attr := range event.Attributes {
				if strings.Contains(attr.Value.String(), "left-pad") {
					t.Errorf("span %s event %s carries a package name: %s", span.Name(), event.Name, attr.Value.String())
				}
			}
		}
	}
	if exceptions == 0 {
		t.Fatal("the failed call recorded no span event; the name check would be vacuous")
	}
}
