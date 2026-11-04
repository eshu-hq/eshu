// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"fmt"
	"net/url"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestDerivedOSVVirtualSlotCountMatchesWorkflow pins the literal bound to the
// coordinator's modulus. The literal keeps this package from importing
// workflow and the reducer tree behind it; a change on either side fails here
// instead of silently planning scopes the parser reads as foreign (#9432).
func TestDerivedOSVVirtualSlotCountMatchesWorkflow(t *testing.T) {
	t.Parallel()
	if derivedOSVVirtualSlotCount != workflow.OwnedPackageVirtualSlotCount {
		t.Fatalf("derivedOSVVirtualSlotCount = %d, workflow.OwnedPackageVirtualSlotCount = %d, want one value",
			derivedOSVVirtualSlotCount, workflow.OwnedPackageVirtualSlotCount)
	}
}

func TestParseDerivedOSVSlotScopeAcceptsPinnedShape(t *testing.T) {
	t.Parallel()

	ecosystem, vslot, chunk, ok := ParseDerivedOSVSlotScope("vuln-intel://osv/npm/_slot/7/0")
	if !ok {
		t.Fatalf("ParseDerivedOSVSlotScope rejected the pinned slot shape")
	}
	if ecosystem != "npm" || vslot != 7 || chunk != 0 {
		t.Fatalf("parsed = (%q, %d, %d), want (npm, 7, 0)", ecosystem, vslot, chunk)
	}
	// The top virtual slot must stay accepted: the bound duplicates the
	// coordinator's V, and shrinking it orphans every scope above it.
	if _, vslot, _, ok := ParseDerivedOSVSlotScope("vuln-intel://osv/pypi/_slot/1023/12"); !ok || vslot != 1023 {
		t.Fatalf("ParseDerivedOSVSlotScope rejected vslot 1023, the top of V=1024")
	}
}

func TestParseDerivedOSVSlotScopeRejectsNonSlotShapes(t *testing.T) {
	t.Parallel()

	for _, scopeID := range []string{
		"",
		"vuln-intel://osv/npm/_batch?query=left-pad%401.3.0",
		"vuln-intel://osv/npm/lodash?version=4.17.20",
		"vuln-intel://osv/npm/_slot/7",
		"vuln-intel://osv/npm/_slot/7/0/extra",
		"vuln-intel://osv/npm/_slot/not-a-number/0",
		"vuln-intel://osv/npm/_slot/7/not-a-number",
		"vuln-intel://osv/npm/_slot/1024/0",
		"vuln-intel://osv/npm/_slot/7/0?query=left-pad%401.3.0",
		"https://osv.dev/npm/_slot/7/0",
		"vuln-intel://osv//_slot/7/0",
	} {
		if _, _, _, ok := ParseDerivedOSVSlotScope(scopeID); ok {
			t.Errorf("ParseDerivedOSVSlotScope(%q) accepted, want reject", scopeID)
		}
	}
}

// TestDerivedScopePackageKeysRejectsSlotScopeWithoutTransport pins fail-closed
// key derivation: a bare slot scope id carries no queries, so scope-id-only
// resolution must refuse it rather than record no package keys and read every
// package as pending.
func TestDerivedScopePackageKeysRejectsSlotScopeWithoutTransport(t *testing.T) {
	t.Parallel()

	scopeID := "vuln-intel://osv/npm/_slot/7/0"
	if _, ok := DerivedScopePackageKeys(scopeID); ok {
		t.Fatalf("DerivedScopePackageKeys(%q) resolved without claim transport, want reject", scopeID)
	}
}

// TestSlotScopePackageKeysMatchLegacyBatchKeys pins the mint-to-claim-resolve
// half of the round-trip contract: keys derived from a slot claim's resolved
// queries equal the keys the same queries parse to from a legacy _batch scope.
func TestSlotScopePackageKeysMatchLegacyBatchKeys(t *testing.T) {
	t.Parallel()

	batchScopeID := "vuln-intel://osv/npm/_batch?query=left-pad%401.3.0&query=is-odd%403.0.1"
	legacyKeys, ok := DerivedScopePackageKeys(batchScopeID)
	if !ok {
		t.Fatalf("rejected legacy batch scope %q", batchScopeID)
	}
	slotKeys, ok := SlotScopePackageKeys("npm", []string{"left-pad", "is-odd"})
	if !ok {
		t.Fatalf("SlotScopePackageKeys rejected npm names")
	}
	if !slices.Equal(slotKeys, legacyKeys) {
		t.Fatalf("slot keys = %+v, legacy batch keys = %+v, want equal", slotKeys, legacyKeys)
	}
	if _, ok := SlotScopePackageKeys("cobol", []string{"payroll"}); ok {
		t.Fatalf("SlotScopePackageKeys accepted an ecosystem that does not normalize, want fail closed")
	}
}

// TestSlotScopePackageKeysFailClosedOnNames pins the name-list bounds: an empty
// list, a blank name, or more names than one chunk report false.
func TestSlotScopePackageKeysFailClosedOnNames(t *testing.T) {
	t.Parallel()

	overCap := make([]string, 0, maxDerivedOSVTargetQueries+1)
	for index := range maxDerivedOSVTargetQueries + 1 {
		overCap = append(overCap, fmt.Sprintf("pkg-%03d", index))
	}
	for name, names := range map[string][]string{
		"nil":     nil,
		"empty":   {},
		"blank":   {"left-pad", "  "},
		"overcap": overCap,
	} {
		if keys, ok := SlotScopePackageKeys("npm", names); ok {
			t.Errorf("SlotScopePackageKeys(%s) = (%+v, true), want false", name, keys)
		}
	}
	if keys, ok := SlotScopePackageKeys("npm", overCap[:maxDerivedOSVTargetQueries]); !ok || len(keys) != maxDerivedOSVTargetQueries {
		t.Fatalf("SlotScopePackageKeys(at cap) = (%d keys, %t), want (%d, true)", len(keys), ok, maxDerivedOSVTargetQueries)
	}
}

func TestIsDerivedOSVSlotScopeMatchesParser(t *testing.T) {
	t.Parallel()

	for _, scopeID := range []string{
		"vuln-intel://osv/npm/_slot/7/0",
		"vuln-intel://osv/pypi/_slot/1023/12",
	} {
		if !IsDerivedOSVSlotScope(scopeID) {
			t.Errorf("IsDerivedOSVSlotScope(%q) = false, want true", scopeID)
		}
	}
	for _, scopeID := range []string{
		"vuln-intel://osv/npm/_batch?query=a%401",
		"vuln-intel://osv/npm/lodash?version=1",
		"vuln-intel://other/npm/_slot/7/0",
	} {
		if IsDerivedOSVSlotScope(scopeID) {
			t.Errorf("IsDerivedOSVSlotScope(%q) = true, want false", scopeID)
		}
	}
}

// TestDerivedTargetPackageKeysUseCanonicalConsumptionKeys pins the #9227 F7
// key derivation: the canonical packageidentity ecosystem plus every
// consumption-key candidate, so the readiness join matches the request side.
func TestDerivedTargetPackageKeysUseCanonicalConsumptionKeys(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		ecosystem string
		packages  []string
		want      []PackageKey
	}{
		{"npm", "npm", []string{"lodash"}, []PackageKey{{Ecosystem: "npm", PackageName: "lodash"}}},
		{"npm scoped", "npm", []string{"@Example/Name"}, []PackageKey{{Ecosystem: "npm", PackageName: "@example/name"}}},
		{"maven", "maven", []string{"org.apache.logging.log4j:log4j-core"}, []PackageKey{
			{Ecosystem: "maven", PackageName: "org.apache.logging.log4j/log4j-core"},
			{Ecosystem: "maven", PackageName: "org.apache.logging.log4j:log4j-core"},
		}},
		{"pypi", "pypi", []string{"Django_REST"}, []PackageKey{
			{Ecosystem: "pypi", PackageName: "django-rest"},
			{Ecosystem: "pypi", PackageName: "django_rest"},
		}},
		{"go spelled go", "go", []string{"golang.org/x/net"}, []PackageKey{{Ecosystem: "gomod", PackageName: "golang.org/x/net"}}},
		{"alma distro alias", "alma", []string{"curl"}, []PackageKey{{Ecosystem: "os", PackageName: "curl"}}},
		{"unknown ecosystem", "not-an-ecosystem", []string{"thing"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := derivedTargetPackageKeys(tc.ecosystem, tc.packages); !slices.Equal(got, tc.want) {
				t.Fatalf("keys = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestDerivedScopePackageKeysCoverBatchAndVersions(t *testing.T) {
	t.Parallel()

	batch := url.Values{}
	for index := range 100 {
		batch.Add("query", fmt.Sprintf("pkg-%03d@1.0.%d", index, index))
	}
	batchKeys, ok := DerivedScopePackageKeys("vuln-intel://osv/npm/_batch?" + batch.Encode())
	if !ok || len(batchKeys) < 100 {
		t.Fatalf("batch keys ok=%v len=%d, want >=100", ok, len(batchKeys))
	}

	versions, ok := DerivedScopePackageKeys("vuln-intel://osv/npm/lodash?version=4.17.20&version=4.17.21")
	want := []PackageKey{{Ecosystem: "npm", PackageName: "lodash"}}
	if !ok || !slices.Equal(versions, want) {
		t.Fatalf("two-version keys ok=%v keys=%+v, want %+v", ok, versions, want)
	}
}

// TestDerivedScopePackageKeysRejectForeignScopes pins that a scope the parser
// rejects (configured target, other source, malformed id) reports false
// (#9227 with #9420), and that an ecosystem the key normalizer does not know
// still parses with no keys, so the scope fails closed instead of being
// guessed.
func TestDerivedScopePackageKeysRejectForeignScopes(t *testing.T) {
	t.Parallel()
	for _, scopeID := range []string{
		"vuln-intel://osv/npm", "vuln-intel://glad/npm/lodash?version=1", "vuln-intel://cisa/kev",
		"vuln-intel://osv/npm/_batch?query=", "vuln-intel://osv/%zz/lodash?version=1", "not a url",
	} {
		if keys, ok := DerivedScopePackageKeys(scopeID); ok || len(keys) != 0 {
			t.Errorf("DerivedScopePackageKeys(%q) = (%+v, %t), want (none, false)", scopeID, keys, ok)
		}
	}
	if keys, ok := DerivedScopePackageKeys("vuln-intel://osv/cobol/payroll?version=1"); !ok || len(keys) != 0 {
		t.Errorf("unknown ecosystem keys = (%+v, %t), want (none, true)", keys, ok)
	}
}

// TestDerivedScopePackageKeysKeepSwiftOSVName pins #9804: a planner-minted
// swift scope records the host/path name OSV indexes, which is the name a
// swift manifest dependency carries for the readiness join.
func TestDerivedScopePackageKeysKeepSwiftOSVName(t *testing.T) {
	t.Parallel()
	const scopeID = "vuln-intel://osv/swift/github.com%2Fapple%2Fswift-nio?version=2.0.0"

	keys, ok := DerivedScopePackageKeys(scopeID)
	if !ok {
		t.Fatalf("DerivedScopePackageKeys(%q) rejected a planner-minted swift scope", scopeID)
	}
	if want := (PackageKey{Ecosystem: "swift", PackageName: "github.com/apple/swift-nio"}); !slices.Contains(keys, want) {
		t.Fatalf("keys = %#v, want to contain %#v", keys, want)
	}
}

// TestParseDerivedOSVScopeReadsEcosystemAndNames carries the scope shapes the
// claim-path tests exercise: single package, repeated versions, a query batch
// across packages, and every ecosystem the planner mints.
func TestParseDerivedOSVScopeReadsEcosystemAndNames(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		scopeID       string
		wantEcosystem string
		wantNames     []string
	}{
		{"single version", "vuln-intel://osv/npm/vite?version=5.4.21", "npm", []string{"vite"}},
		{"two versions", "vuln-intel://osv/npm/qs?version=6.5.2&version=6.5.3", "npm", []string{"qs", "qs"}},
		{"duplicate versions collapse", "vuln-intel://osv/npm/qs?version=6.5.2&version=6.5.2&version=", "npm", []string{"qs"}},
		{"query batch across packages", "vuln-intel://osv/npm/_batch?query=qs%406.5.2&query=%40scope%2Fwidget%401.2.3", "npm", []string{"qs", "@scope/widget"}},
		{"duplicate batch queries collapse", "vuln-intel://osv/npm/_batch?query=qs%401&query=qs%401&query=qs%402", "npm", []string{"qs", "qs"}},
		{"malformed batch queries skip", "vuln-intel://osv/npm/_batch?query=%401&query=qs%40&query=noversion&query=qs%401", "npm", []string{"qs"}},
		{"swift url name", "vuln-intel://osv/swift/https:%2F%2Fgithub.com%2Fapple%2Fswift-crypto.git?version=4.3.0", "swift", []string{"https://github.com/apple/swift-crypto.git"}},
		{"pub", "vuln-intel://osv/pub/http?version=1.2.2", "pub", []string{"http"}},
		{"pypi", "vuln-intel://osv/pypi/django?version=4.2.11", "pypi", []string{"django"}},
		{"go", "vuln-intel://osv/go/golang.org%2Fx%2Fnet?version=v0.17.0", "go", []string{"golang.org/x/net"}},
		{"maven", "vuln-intel://osv/maven/org.apache.logging.log4j:log4j-core?version=2.17.1", "maven", []string{"org.apache.logging.log4j:log4j-core"}},
		{"nuget", "vuln-intel://osv/nuget/newtonsoft.json?version=13.0.3", "nuget", []string{"newtonsoft.json"}},
		{"composer", "vuln-intel://osv/composer/symfony%2Fconsole?version=7.0.0", "composer", []string{"symfony/console"}},
		{"rubygems", "vuln-intel://osv/rubygems/rails?version=7.1.0", "rubygems", []string{"rails"}},
		{"cargo", "vuln-intel://osv/cargo/serde_json?version=1.0.116", "cargo", []string{"serde_json"}},
		{"hex", "vuln-intel://osv/hex/phoenix_html?version=4.2.1", "hex", []string{"phoenix_html"}},
		{"ascii case variant lowers", "vuln-intel://osv/PYPI/requests?version=2.0.0", "pypi", []string{"requests"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ecosystem, names, ok := parseDerivedOSVScope(tc.scopeID)
			if !ok {
				t.Fatalf("parseDerivedOSVScope(%q) rejected, want accept", tc.scopeID)
			}
			if ecosystem != tc.wantEcosystem {
				t.Fatalf("ecosystem = %q, want %q", ecosystem, tc.wantEcosystem)
			}
			if !slices.Equal(names, tc.wantNames) {
				t.Fatalf("names = %q, want %q", names, tc.wantNames)
			}
		})
	}
}

// TestParseDerivedOSVScopeFailsClosedOverTheQueryCap pins the one-chunk bound
// on both the version and the batch shape. The counts are literals on
// purpose: the coordinator mints at most 100 queries per target, so a cap that
// drifts from 100 in either direction must fail here instead of following the
// constant.
func TestParseDerivedOSVScopeFailsClosedOverTheQueryCap(t *testing.T) {
	t.Parallel()

	scopeIDs := func(count int) []string {
		versions := url.Values{}
		batch := url.Values{}
		for index := range count {
			versions.Add("version", fmt.Sprintf("1.0.%d", index))
			batch.Add("query", fmt.Sprintf("pkg-%03d@1.0.%d", index, index))
		}
		return []string{
			"vuln-intel://osv/npm/lodash?" + versions.Encode(),
			"vuln-intel://osv/npm/_batch?" + batch.Encode(),
		}
	}
	for _, scopeID := range scopeIDs(100) {
		if _, _, ok := parseDerivedOSVScope(scopeID); !ok {
			t.Errorf("parseDerivedOSVScope(%s...) rejected exactly 100 queries, want accept", scopeID[:min(len(scopeID), 40)])
		}
	}
	for _, scopeID := range scopeIDs(101) {
		if _, _, ok := parseDerivedOSVScope(scopeID); ok {
			t.Errorf("parseDerivedOSVScope(%s...) accepted 101 queries, want reject", scopeID[:min(len(scopeID), 40)])
		}
	}
}

// TestDerivedOSVEcosystemTokensKeepLookalikesApart pins #9584 for the scope
// grammar: scope-id ecosystem tokens fold ASCII letters only, so a lookalike
// token never becomes a key under the ASCII ecosystem it imitates.
func TestDerivedOSVEcosystemTokensKeepLookalikesApart(t *testing.T) {
	t.Parallel()

	if ecosystem, _, ok := parseDerivedOSVScope("vuln-intel://osv/pyp%C4%B0/requests?version=2.0.0"); ok && ecosystem == "pypi" {
		t.Fatalf("parseDerivedOSVScope folded a lookalike ecosystem to %q", ecosystem)
	}
	ecosystem, _, _, ok := ParseDerivedOSVSlotScope("vuln-intel://osv/pyp%C4%B0/_slot/7/0")
	if ok && ecosystem == "pypi" {
		t.Fatalf("ParseDerivedOSVSlotScope folded a lookalike ecosystem to %q", ecosystem)
	}
	keys, _ := SlotScopePackageKeys("PYP\u0130", []string{"requests"})
	for _, key := range keys {
		if key.Ecosystem == "pypi" {
			t.Fatalf("SlotScopePackageKeys keyed a lookalike ecosystem as %+v", key)
		}
	}
}
