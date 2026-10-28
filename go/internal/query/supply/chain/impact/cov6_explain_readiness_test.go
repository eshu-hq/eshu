// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
)

func TestCov6BuildNoEvidenceExplanation(t *testing.T) {
	t.Parallel()
	filter := ExplanationFilter{CVEID: "CVE-1", PackageID: "pkg:npm/x"}
	readiness := ReadinessEnvelope{State: ReadinessStateReadyWithFindings, MissingEvidence: []string{"sbom"}}
	got := BuildNoEvidenceExplanation(filter, readiness)
	if got.Outcome != "no_finding" {
		t.Fatalf("outcome = %q, want no_finding", got.Outcome)
	}
	if got.Advisory.CVEID != "CVE-1" || got.Component.PackageID != "pkg:npm/x" {
		t.Fatalf("advisory/component = %+v/%+v, want filter echo", got.Advisory, got.Component)
	}
	if got.Version.VersionEvidence != "missing" {
		t.Fatalf("version evidence = %q, want missing", got.Version.VersionEvidence)
	}
	if len(got.MissingEvidence) != 2 {
		t.Fatalf("missing = %v, want impact_finding plus sbom", got.MissingEvidence)
	}
	if !strings.HasPrefix(got.EvidencePacketHandle, "supply-chain-impact-explanation:scope:") {
		t.Fatalf("handle = %q, want scope handle", got.EvidencePacketHandle)
	}
}

func TestCov6BuildAmbiguousExplanation(t *testing.T) {
	t.Parallel()
	filter := ExplanationFilter{PackageID: "pkg:npm/x"}
	readiness := ReadinessEnvelope{State: ReadinessStateReadyWithFindings, Counts: ReadinessCounts{FindingsReturned: 1}}
	got := BuildAmbiguousExplanation(filter, readiness, 3)
	if got.Outcome != "ambiguous_scope" {
		t.Fatalf("outcome = %q, want ambiguous_scope", got.Outcome)
	}
	if got.Readiness.State != ReadinessStateAmbiguousScope {
		t.Fatalf("readiness state = %q, want ambiguous_scope", got.Readiness.State)
	}
	if got.Readiness.Counts.FindingsReturned != 3 {
		t.Fatalf("candidate count = %d, want 3", got.Readiness.Counts.FindingsReturned)
	}
	// A single candidate still floors to two.
	solo := BuildAmbiguousExplanation(filter, readiness, 1)
	if solo.Readiness.Counts.FindingsReturned != 2 {
		t.Fatalf("solo count = %d, want 2", solo.Readiness.Counts.FindingsReturned)
	}
	// An already-unavailable envelope keeps its state.
	dark := ReadinessEnvelope{State: ReadinessStateReadinessUnavailable}
	kept := supplyChainImpactAmbiguousReadiness(dark, 2)
	if kept.State != ReadinessStateReadinessUnavailable {
		t.Fatalf("unavailable state = %q, want preserved", kept.State)
	}
}

func TestCov6ExplanationScopesAndHandles(t *testing.T) {
	t.Parallel()
	if !(ExplanationFilter{FindingID: "f1"}).HasBoundedScope() {
		t.Fatal("finding_id must bound the scope")
	}
	if !(ExplanationFilter{CVEID: "CVE-1", PackageID: "p"}).HasBoundedScope() {
		t.Fatal("cve plus package must bound the scope")
	}
	if (ExplanationFilter{CVEID: "CVE-1"}).HasBoundedScope() {
		t.Fatal("bare cve must not bound the scope")
	}
	if (ExplanationFilter{AdvisoryID: "a"}).HasBoundedScope() {
		t.Fatal("bare advisory must not bound the scope")
	}
	if !(ExplanationFilter{AdvisoryID: "a", ServiceID: "s"}).HasBoundedScope() {
		t.Fatal("advisory plus service must bound the scope")
	}
	if !(ExplanationFilter{}).hasTargetScope() == false {
		t.Fatal("empty filter must have no target scope")
	}
	// Finding-anchored handles name the finding; scope handles hash the scope.
	if got := supplyChainImpactEvidencePacketHandle(ExplanationFilter{}, " f1 "); got != "supply-chain-impact-explanation:finding:f1" {
		t.Fatalf("finding handle = %q", got)
	}
	a := supplyChainImpactEvidencePacketHandle(ExplanationFilter{PackageID: "p"}, "")
	b := supplyChainImpactEvidencePacketHandle(ExplanationFilter{PackageID: "q"}, "")
	if !strings.HasPrefix(a, "supply-chain-impact-explanation:scope:") || a == b {
		t.Fatalf("scope handles = (%q,%q), want distinct scope hashes", a, b)
	}
	// Readiness scope fills blanks from the row.
	scope := FindingReadinessScope(FindingRow{CVEID: "CVE-9"}, ExplanationFilter{PackageID: "p"})
	if scope.CVEID != "CVE-9" || scope.PackageID != "p" {
		t.Fatalf("readiness scope = %+v, want row cve plus filter package", scope)
	}
	if err := newSupplyChainImpactExplanationAmbiguousError(1); ExplanationAmbiguousCandidateCount(err) != 2 {
		t.Fatalf("floored error counts %d, want 2", ExplanationAmbiguousCandidateCount(err))
	}
	if ExplanationAmbiguousCandidateCount(errors.New("nope")) != 0 {
		t.Fatal("foreign error must count 0 candidates")
	}
	if !errors.Is(newSupplyChainImpactExplanationAmbiguousError(3), ErrExplanationAmbiguous) {
		t.Fatal("ambiguous error must match the sentinel")
	}
}

func TestCov6BuildExplanationMinimalRow(t *testing.T) {
	t.Parallel()
	filter := ExplanationFilter{FindingID: "f1"}
	row := ExplanationRow{Finding: FindingRow{FindingID: "f1", CVEID: "CVE-1", PackageID: "pkg:npm/x"}}
	got := BuildExplanation(filter, row, ReadinessEnvelope{State: ReadinessStateReadyWithFindings})
	if got.Outcome != "finding_explained" {
		t.Fatalf("outcome = %q, want finding_explained", got.Outcome)
	}
	if got.Finding == nil {
		t.Fatal("explained finding must be present")
	}
	if got.EvidencePacketHandle != "supply-chain-impact-explanation:finding:f1" {
		t.Fatalf("handle = %q, want finding handle", got.EvidencePacketHandle)
	}
}

func TestCov6BuildReadinessUnavailable(t *testing.T) {
	t.Parallel()
	scope := TargetScope{PackageID: "pkg:npm/x"}
	findings := []FindingResult{{Status: "affected"}, {Status: "affected"}, {Status: ""}}
	got := BuildReadinessUnavailable(scope, findings, true)
	if got.State != ReadinessStateReadinessUnavailable {
		t.Fatalf("state = %q, want unavailable", got.State)
	}
	if got.Counts.FindingsReturned != 3 || !got.Counts.FindingsTruncated {
		t.Fatalf("counts = %+v, want 3 returned truncated", got.Counts)
	}
	if got.Counts.FindingsByStatus["affected"] != 2 {
		t.Fatalf("by-status = %v, want affected:2", got.Counts.FindingsByStatus)
	}
	if len(got.MissingEvidence) != 1 {
		t.Fatalf("missing = %v, want the unavailable marker", got.MissingEvidence)
	}
	if countFindingsByStatus(nil) != nil || countFindingsByStatus([]FindingResult{{}}) != nil {
		t.Fatal("empty inputs must count nil")
	}
}

func TestCov6DecodeReadinessMetadata(t *testing.T) {
	t.Parallel()
	if got, err := decodeSourceSnapshots(sql.NullString{}); got != nil || err != nil {
		t.Fatalf("null snapshots = (%v,%v), want (nil,nil)", got, err)
	}
	if _, err := decodeSourceSnapshots(sql.NullString{Valid: true, String: "bogus"}); err == nil {
		t.Fatal("bogus snapshots must fail")
	}
	if got, err := decodeSourceSnapshots(sql.NullString{Valid: true, String: "[]"}); err != nil || len(got) != 0 {
		t.Fatalf("empty snapshots = (%v,%v)", got, err)
	}
	if got, err := decodeSourceStates(sql.NullString{}); got != nil || err != nil {
		t.Fatalf("null states = (%v,%v), want (nil,nil)", got, err)
	}
	if _, err := decodeSourceStates(sql.NullString{Valid: true, String: "bogus"}); err == nil {
		t.Fatal("bogus states must fail")
	}
	if got, err := decodeUnsupportedTargets(sql.NullString{}); got != nil || err != nil {
		t.Fatalf("null targets = (%v,%v), want (nil,nil)", got, err)
	}
	if _, err := decodeUnsupportedTargets(sql.NullString{Valid: true, String: "bogus"}); err == nil {
		t.Fatal("bogus targets must fail")
	}
}

func TestCov6NormalizeUnsupportedTargets(t *testing.T) {
	t.Parallel()
	if normalizeUnsupportedTargets(nil) != nil {
		t.Fatal("empty input must normalize nil")
	}
	got := normalizeUnsupportedTargets([]UnsupportedTarget{
		{TargetKind: " " + UnsupportedTargetKindEcosystem + " ", Reason: " r ", Count: 2, Ecosystem: " npm "},
		{TargetKind: UnsupportedTargetKindEcosystem, Reason: "r", Count: 3, Ecosystem: "npm"},
		{TargetKind: "bogus", Reason: "r"},
		{TargetKind: UnsupportedTargetKindEcosystem, Reason: ""},
		{TargetKind: "", Reason: "r"},
		{TargetKind: UnsupportedTargetKindImageTarget, Reason: "r2", Count: 0},
	})
	if len(got) != 2 {
		t.Fatalf("normalized = %+v, want 2 survivors", got)
	}
	if got[0].Count != 5 {
		t.Fatalf("merged count = %d, want 5", got[0].Count)
	}
	if got[0].TargetKind != UnsupportedTargetKindEcosystem || got[0].Ecosystem != "npm" {
		t.Fatalf("merged entry = %+v, want trimmed ecosystem row", got[0])
	}
	if got[1].Count != 1 {
		t.Fatalf("zero count normalizes to %d, want 1", got[1].Count)
	}
}

func TestCov6ResolveReadinessTargetPackage(t *testing.T) {
	t.Parallel()
	pkg, err := resolveReadinessTargetPackage("", "pkg:npm/lodash@4.17.21", "", "", nil)
	if err != nil {
		t.Fatalf("purl target rejected: %v", err)
	}
	if pkg.packageID == "" || len(pkg.keys) == 0 {
		t.Fatalf("purl target = %+v, want identity with keys", pkg)
	}
	if _, err := resolveReadinessTargetPackage("pkg:npm/other", "pkg:npm/lodash@4.17.21", "", "", nil); err == nil {
		t.Fatal("disagreeing package id must fail")
	}
	// cocoapods carries no manifest-consumption identity, so the target fails closed.
	if _, err := resolveReadinessTargetPackage("", "pkg:cocoapods/AFNetworking@4.0", "", "", nil); err == nil {
		t.Fatal("identity-less purl must fail")
	}
	named, err := resolveReadinessTargetPackage("", "", "npm", "lodash", nil)
	if err != nil {
		t.Fatalf("ecosystem+name target rejected: %v", err)
	}
	if named.packageID == "" || len(named.keys) == 0 {
		t.Fatalf("named target = %+v, want identity with keys", named)
	}
	if _, err := resolveReadinessTargetPackage("", "", "bogus-eco", "x", nil); err == nil {
		t.Fatal("unknown ecosystem must fail")
	}
	if _, err := resolveReadinessTargetPackage("", "", "", "", nil); err == nil {
		t.Fatal("empty target must fail")
	}
	// Ownership validation short-circuits without keys; empty package ids fail.
	if err := validateReadinessTargetKeyOwners(t.Context(), nil, readinessTarget{}); err != nil {
		t.Fatalf("keyless ownership validation = %v, want nil", err)
	}
	if _, err := resolveReadinessPackageID(t.Context(), nil, "  "); err == nil {
		t.Fatal("blank package id must fail before any store read")
	}
	if _, err := readinessTargetPackageForPackageID("  ", nil); err == nil {
		t.Fatal("blank package id must fail")
	}
}

func TestCov6ReadinessTargetArguments(t *testing.T) {
	t.Parallel()
	pkg, err := resolveReadinessTargetPackage("", "pkg:npm/lodash@4.17.21", "", "", nil)
	if err != nil {
		t.Fatalf("purl target rejected: %v", err)
	}
	target := readinessTarget{PackageIDs: []string{pkg.packageID}, Keys: pkg.keys}
	ecosystems, names, ids, _ := readinessTargetArguments(target, ReadinessQuery{PackageID: pkg.packageID})
	if len(ecosystems) != len(pkg.keys) || len(names) != len(pkg.keys) || len(ids) != 1 {
		t.Fatalf("target arguments lengths = (%d,%d,%d)", len(ecosystems), len(names), len(ids))
	}
	merged := mergeReadinessTargetPackage(readinessTargetPackage{}, pkg)
	if len(merged.keys) != len(pkg.keys) {
		t.Fatal("merge into empty must keep every key")
	}
	pkgIDs, ecos, names, trustedIDs, trustedEcos, trustedNames := readinessTargetOwnerArguments(target)
	if len(pkgIDs) != len(pkg.keys) || len(ecos) != len(pkg.keys) || len(names) != len(pkg.keys) {
		t.Fatalf("owner arguments lengths = (%d,%d,%d)", len(pkgIDs), len(ecos), len(names))
	}
	if len(trustedIDs) != len(pkg.keys) || len(trustedEcos) != len(pkg.keys) || len(trustedNames) != len(pkg.keys) {
		t.Fatal("purl-derived keys must all be parser-trusted")
	}
	intersected := intersectReadinessTargetPackages([]map[string]readinessTargetPackage{{pkg.packageID: pkg}})
	if len(intersected.PackageIDs) != 1 {
		t.Fatalf("intersection = %+v, want the shared package", intersected)
	}
	if got := intersectReadinessTargetPackages(nil); len(got.PackageIDs) != 0 {
		t.Fatalf("empty intersection = %+v, want empty", got)
	}
}

func TestCov6ImpactSmallHelpers(t *testing.T) {
	t.Parallel()
	if !WinnersReadEnabled("true") || WinnersReadEnabled("nope") || WinnersReadEnabled("") {
		t.Fatal("winners flag parsing mismatch")
	}
	payload := map[string]any{"entity_keys": []any{"deployment:abc", "service:x"}}
	if !payloadEntityKeyHasPrefix(payload, "deployment:") {
		t.Fatal("deployment prefix must match")
	}
	if payloadEntityKeyHasPrefix(payload, "workload:") || payloadEntityKeyHasPrefix(map[string]any{}, "deployment:") {
		t.Fatal("absent prefix must not match")
	}
	if got := decodeSupplyChainImpactPriorityContributions("nope"); got != nil {
		t.Fatalf("non-list contributions = %v, want nil", got)
	}
	contribs := decodeSupplyChainImpactPriorityContributions([]any{
		map[string]any{"reason_code": "rc1"},
		"junk",
		map[string]any{"other": 1},
	})
	if len(contribs) != 1 {
		t.Fatalf("contributions = %+v, want the single reasoned row", contribs)
	}
	unknown := decodeSupplyChainComponentEvidence(EvidenceFact{FactKind: "bogus.kind"})
	if unknown.Matched {
		t.Fatal("unknown fact kind must not match component evidence")
	}
	broken := decodeSupplyChainComponentEvidence(EvidenceFact{FactKind: factschema.FactKindSBOMDocument, Payload: map[string]any{}})
	if !broken.Matched || broken.Err == nil {
		t.Fatalf("malformed document = %+v, want matched failure", broken)
	}
	var anchors ExplanationAnchors
	appendDeploymentAnchors(&anchors, map[string]any{"deployment_id": "d1", "entity_keys": []any{"deployment:d2", "service:x"}})
	if len(anchors.Deployments) != 2 {
		t.Fatalf("deployments = %v, want id plus entity key", anchors.Deployments)
	}
	appendLockfilePathAnchors(&anchors, "  ")
	if len(anchors.LockfilePaths) != 0 {
		t.Fatal("blank lockfile path must anchor nothing")
	}
	appendLockfilePathAnchors(&anchors, "go.mod")
	if len(anchors.LockfilePaths) != 1 || len(anchors.ManifestPaths) != 1 {
		t.Fatalf("lockfile anchors = %+v, want both paths", anchors)
	}
}
