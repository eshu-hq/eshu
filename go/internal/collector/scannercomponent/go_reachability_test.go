// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannercomponent

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
)

const (
	testRepositoryID = "repository:r_1a2b3c4d"
	testCommit       = "0123456789abcdef0123456789abcdef01234567"
	testCloneURL     = "https://github.com/eshu-hq/widgets"
)

// TestGoReachabilityGrantsCoverEveryEmittedKind pins the four fact kinds the
// analyzer may emit, each at schema 1.0.0, so a degraded-path warning is never
// rejected at admission.
func TestGoReachabilityGrantsCoverEveryEmittedKind(t *testing.T) {
	t.Parallel()

	grants, err := RequiredGrantsFor(AnalyzerGoReachability)
	if err != nil {
		t.Fatalf("RequiredGrantsFor() error = %v", err)
	}
	want := []string{
		"vulnerability.go_module_evidence",
		"vulnerability.go_call_reachability",
		"vulnerability.warning",
		"scanner_worker.warning",
	}
	if len(grants) != len(want) {
		t.Fatalf("grants = %d, want %d", len(grants), len(want))
	}
	for i, grant := range grants {
		if grant.Kind != want[i] {
			t.Errorf("grant[%d].Kind = %q, want %q", i, grant.Kind, want[i])
		}
		if len(grant.SchemaVersions) != 1 || grant.SchemaVersions[0] != "1.0.0" {
			t.Errorf("grant[%d].SchemaVersions = %v, want [1.0.0]", i, grant.SchemaVersions)
		}
		if grant.ProducerID != IDGoReachability || grant.Scope != "scanner_worker" || grant.Version != Version {
			t.Errorf("grant[%d] = %+v, want the go-reachability producer in the scanner_worker scope", i, grant)
		}
	}
}

// TestRequiredGrantsForRefusesReducerLane proves the unpinned grant path keeps
// the lane check ManifestFor applies.
func TestRequiredGrantsForRefusesReducerLane(t *testing.T) {
	t.Parallel()

	if _, err := RequiredGrantsFor(AnalyzerSecurityPriority); err == nil {
		t.Fatal("RequiredGrantsFor(reducer lane) error = nil, want rejection")
	}
}

// TestGoReachabilityManifestFailsClosedUntilPinned proves no unpinned artifact
// is ever minted: the image does not exist until the satellite publishes it,
// so both the manifest and seeded-artifact admission refuse the component.
func TestGoReachabilityManifestFailsClosedUntilPinned(t *testing.T) {
	t.Parallel()

	if _, err := ManifestFor(AnalyzerGoReachability); err == nil ||
		!strings.Contains(err.Error(), "no published image digest pinned") {
		t.Fatalf("ManifestFor(go_reachability) error = %v, want the fail-closed digest error", err)
	}
	got, err := AnalyzerFor(IDGoReachability)
	if err != nil || got != AnalyzerGoReachability {
		t.Fatalf("AnalyzerFor() = %q, %v, want the analyzer", got, err)
	}
	seeded := component.Manifest{Metadata: component.Metadata{ID: IDGoReachability}}
	if err := VerifySeededArtifact(seeded); err == nil {
		t.Fatal("VerifySeededArtifact(unpinned go reachability) = nil, want refusal")
	}
}

// TestGoReachabilityMetricsPrefixIsAValidMetricName proves the hyphen in the
// component id never reaches a Prometheus metric name.
func TestGoReachabilityMetricsPrefixIsAValidMetricName(t *testing.T) {
	t.Parallel()

	manifest, err := unpinnedManifest(AnalyzerGoReachability)
	if err != nil {
		t.Fatalf("unpinnedManifest() error = %v", err)
	}
	if got, want := manifest.Spec.Telemetry.MetricsPrefix, "eshu_component_scanner_go_reachability"; got != want {
		t.Fatalf("MetricsPrefix = %q, want %q", got, want)
	}
}

func TestGoReachabilityScopeIDRoundTrip(t *testing.T) {
	t.Parallel()

	scopeID, err := GoReachabilityScopeID(testRepositoryID)
	if err != nil {
		t.Fatalf("GoReachabilityScopeID() error = %v", err)
	}
	if want := "go-reachability://repository:r_1a2b3c4d"; scopeID != want {
		t.Fatalf("scope id = %q, want %q", scopeID, want)
	}
	repositoryID, ok := ParseGoReachabilityScopeID(scopeID)
	if !ok || repositoryID != testRepositoryID {
		t.Fatalf("Parse = %q, %v, want the repository id", repositoryID, ok)
	}
}

// TestGoReachabilityScopeIDRefusesAnythingButACanonicalID proves no URL, path,
// or other repository spelling can reach a scope id.
func TestGoReachabilityScopeIDRefusesAnythingButACanonicalID(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{
		"", "repository:r_", "repository:r_1A2B3C4D", "repository:r_1a2b3c4", "repository:r_1a2b3c4d5",
		"https://github.com/eshu-hq/widgets", "repository:r_1a2b3c4d/x", " repository:r_1a2b3c4d",
	} {
		if got, err := GoReachabilityScopeID(bad); err == nil {
			t.Errorf("GoReachabilityScopeID(%q) = %q, want refusal", bad, got)
		}
		if _, ok := ParseGoReachabilityScopeID(GoReachabilityScopePrefix + bad); ok {
			t.Errorf("ParseGoReachabilityScopeID(prefix+%q) ok = true, want false", bad)
		}
	}
	if _, ok := ParseGoReachabilityScopeID("vuln-intel://cisa/kev"); ok {
		t.Error("Parse(other scope) ok = true, want false")
	}
}

func testTarget() GoReachabilityTarget {
	return GoReachabilityTarget{
		ScopeID:          GoReachabilityScopePrefix + testRepositoryID,
		TargetKind:       string(TargetRepository),
		RepositoryID:     testRepositoryID,
		SourceCommitSHA:  testCommit,
		CloneURL:         testCloneURL,
		AcceptanceUnitID: GoReachabilityScopePrefix + testRepositoryID,
	}
}

// TestGoReachabilityTargetValidate covers every shape a stale or hand-edited
// run row can carry, and proves the error names the defect and never the value.
func TestGoReachabilityTargetValidate(t *testing.T) {
	t.Parallel()

	if err := testTarget().Validate(); err != nil {
		t.Fatalf("Validate(valid) error = %v", err)
	}
	const sentinel = "s3cr3t"
	for name, mutate := range map[string]func(*GoReachabilityTarget){
		"scope mismatch":     func(g *GoReachabilityTarget) { g.ScopeID = GoReachabilityScopePrefix + "repository:r_ffffffff" },
		"blank commit":       func(g *GoReachabilityTarget) { g.SourceCommitSHA = "" },
		"short commit":       func(g *GoReachabilityTarget) { g.SourceCommitSHA = "abc123" },
		"branch as commit":   func(g *GoReachabilityTarget) { g.SourceCommitSHA = "main" },
		"http":               func(g *GoReachabilityTarget) { g.CloneURL = "http://github.com/eshu-hq/widgets" },
		"userinfo":           func(g *GoReachabilityTarget) { g.CloneURL = "https://" + sentinel + "@github.com/eshu-hq/widgets" },
		"query":              func(g *GoReachabilityTarget) { g.CloneURL = testCloneURL + "?token=" + sentinel },
		"fragment":           func(g *GoReachabilityTarget) { g.CloneURL = testCloneURL + "#" + sentinel },
		"local path":         func(g *GoReachabilityTarget) { g.CloneURL = "/srv/" + sentinel },
		"file url":           func(g *GoReachabilityTarget) { g.CloneURL = "file:///srv/" + sentinel },
		"owner only":         func(g *GoReachabilityTarget) { g.CloneURL = "https://github.com/" + sentinel },
		"traversal":          func(g *GoReachabilityTarget) { g.CloneURL = "https://github.com/../" + sentinel },
		"non-default port":   func(g *GoReachabilityTarget) { g.CloneURL = "https://github.com:8443/eshu-hq/" + sentinel },
		"uppercase host":     func(g *GoReachabilityTarget) { g.CloneURL = "https://GitHub.com/eshu-hq/" + sentinel },
		"acceptance differs": func(g *GoReachabilityTarget) { g.AcceptanceUnitID = "other" },
	} {
		target := testTarget()
		mutate(&target)
		err := target.Validate()
		if err == nil {
			t.Errorf("%s: Validate() = nil, want refusal", name)
			continue
		}
		if strings.Contains(err.Error(), sentinel) {
			t.Errorf("%s: error %q echoes the value", name, err)
		}
	}
}

// TestGoReachabilityInstallationKeyBindsHostAndOwner proves the key names the
// host as well as the owner, so a look-alike host never selects an
// installation mapped for github.com.
func TestGoReachabilityInstallationKeyBindsHostAndOwner(t *testing.T) {
	t.Parallel()

	target := testTarget()
	if got, want := target.InstallationKey(), "github-owner:github.com/eshu-hq"; got != want {
		t.Fatalf("InstallationKey() = %q, want %q", got, want)
	}
	target.CloneURL = "https://evil.example/eshu-hq/widgets"
	if got, want := target.InstallationKey(), "github-owner:evil.example/eshu-hq"; got != want {
		t.Fatalf("InstallationKey() = %q, want %q", got, want)
	}
}

func TestResolveGoReachabilityTarget(t *testing.T) {
	t.Parallel()

	raw := `{"collector_instance_id":"i","analyzer":"go_reachability","targets":[` +
		`{"scope_id":"go-reachability://repository:r_00000000","target_kind":"repository","repository_id":"repository:r_00000000",` +
		`"source_commit_sha":"` + testCommit + `","clone_url":"https://github.com/eshu-hq/other","acceptance_unit_id":"go-reachability://repository:r_00000000"},` +
		`{"scope_id":"go-reachability://repository:r_1a2b3c4d","target_kind":"repository","repository_id":"repository:r_1a2b3c4d",` +
		`"source_commit_sha":"` + testCommit + `","clone_url":"` + testCloneURL + `","acceptance_unit_id":"go-reachability://repository:r_1a2b3c4d"}]}`

	got, err := ResolveGoReachabilityTarget(raw, GoReachabilityScopePrefix+testRepositoryID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != testTarget() {
		t.Fatalf("Resolve() = %+v, want %+v", got, testTarget())
	}
	for name, input := range map[string]string{
		"empty":      "",
		"not json":   "{",
		"no targets": `{"targets":[]}`,
		"other only": `{"targets":[{"scope_id":"go-reachability://repository:r_00000000"}]}`,
		"ambiguous": `{"targets":[{"scope_id":"go-reachability://repository:r_1a2b3c4d"},` +
			`{"scope_id":"go-reachability://repository:r_1a2b3c4d"}]}`,
		"incomplete": `{"targets":[{"scope_id":"go-reachability://repository:r_1a2b3c4d"}]}`,
	} {
		if _, err := ResolveGoReachabilityTarget(input, GoReachabilityScopePrefix+testRepositoryID); err == nil {
			t.Errorf("%s: Resolve() error = nil, want refusal", name)
		}
	}
}

// TestGoModulePageWindow pins slot arithmetic: consecutive buckets take
// consecutive pages and wrap, and an empty fleet has one empty slot.
func TestGoModulePageWindow(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		bucket              int64
		total, size         int
		wantSlot, wantSlots int
	}{
		{bucket: 0, total: 0, size: 256, wantSlot: 0, wantSlots: 1},
		{bucket: 7, total: 256, size: 256, wantSlot: 0, wantSlots: 1},
		{bucket: 7, total: 257, size: 256, wantSlot: 1, wantSlots: 2},
		{bucket: 8, total: 257, size: 256, wantSlot: 0, wantSlots: 2},
		{bucket: 5, total: 1000, size: 256, wantSlot: 1, wantSlots: 4},
		{bucket: -3, total: 1000, size: 256, wantSlot: 0, wantSlots: 4},
	} {
		slot, slots := GoModulePageWindow(tc.bucket, tc.total, tc.size)
		if slot != tc.wantSlot || slots != tc.wantSlots {
			t.Errorf("GoModulePageWindow(%d,%d,%d) = %d,%d, want %d,%d",
				tc.bucket, tc.total, tc.size, slot, slots, tc.wantSlot, tc.wantSlots)
		}
	}
}
