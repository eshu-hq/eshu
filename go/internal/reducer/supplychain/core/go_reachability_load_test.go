// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

func BenchmarkGoReachabilityStageNoLoader(b *testing.B) {
	handler := SupplyChainImpactHandler{FencingTokenIssuer: newTestImpactFencingTokenIssuer(), FactLoader: &manifestBackedSupplyChainImpactLoader{}}
	envelopes := goCrossScopeHolderSeed()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := handler.loadGoReachabilityEvidenceFacts(context.Background(), envelopes); err != nil {
			b.Fatalf("loadGoReachabilityEvidenceFacts() error = %v", err)
		}
	}
}

// Canonical repository id for the #10095 cross-scope loader tests. It matches
// scannercomponent's canonical pattern (repository:r_ + 8 hex) without
// colliding with the #10055 fence seed.
const goCrossScopeTestRepository = "repository:r_10095a01"

const (
	goCrossScopeTestAdvisory = "GO-2026-10095"
	goCrossScopeTestCVEAlias = "CVE-2026-10095"
	goCrossScopeTestModule   = "example.com/crossscope"
	goCrossScopeTestPackage  = "gomod://proxy.golang.org/example.com/crossscope"
	goCrossScopeTestScope    = "go-reachability://repository:r_10095a01"
)

// goCrossScopeBackedLoader is the family-local fake for the #10095 loader:
// holder-scope facts come from ListFactsByKind, Go evidence from
// ListActiveGoReachabilityFacts. Every other stage sees a loader that does
// not implement its interface and no-ops, exactly like production before the
// storage reader lands.
type goCrossScopeBackedLoader struct {
	scopeFacts []facts.Envelope
	goFacts    []facts.Envelope
	goCalls    int
	goFilters  []GoReachabilityFactFilter
}

func (s *goCrossScopeBackedLoader) ListFacts(
	_ context.Context,
	_, _ string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *goCrossScopeBackedLoader) ListFactsByKind(
	_ context.Context,
	_, _ string,
	kinds []string,
) ([]facts.Envelope, error) {
	want := make(map[string]struct{}, len(kinds))
	for _, kind := range kinds {
		want[kind] = struct{}{}
	}
	var out []facts.Envelope
	for _, envelope := range s.scopeFacts {
		if _, ok := want[envelope.FactKind]; ok {
			out = append(out, envelope)
		}
	}
	return out, nil
}

func (s *goCrossScopeBackedLoader) ListActiveSupplyChainImpactFacts(
	_ context.Context,
	_ SupplyChainImpactFactFilter,
) ([]facts.Envelope, bool, error) {
	return nil, false, nil
}

func (s *goCrossScopeBackedLoader) ListActiveGoReachabilityFacts(
	_ context.Context,
	filter GoReachabilityFactFilter,
) ([]facts.Envelope, error) {
	s.goCalls++
	s.goFilters = append(s.goFilters, filter)
	return append([]facts.Envelope(nil), s.goFacts...), nil
}

// ListActivePackageManifestDependencyFactsWithRefusals keeps the #10095 stub
// on the refusing seam (#10109): it holds no manifest rows, so it reports no
// refusal, matching the pre-#10109 skip for these tests.
func (s *goCrossScopeBackedLoader) ListActivePackageManifestDependencyFactsWithRefusals(
	context.Context,
	[]string,
	[]string,
) ([]facts.Envelope, bool, error) {
	return nil, false, nil
}

// goCrossScopeHolderSeed mirrors seedGoFenceChain (#10055 acceptance seed):
// a Go advisory holder with its CVE, gomod affected_package, and consumption
// facts. The Go evidence itself lives in goFacts, standing in for the active
// generation of the go-reachability scope.
func goCrossScopeHolderSeed() []facts.Envelope {
	return []facts.Envelope{
		vulnerabilityCVEFact("fact:10095:cve", goCrossScopeTestAdvisory, 7.5),
		vulnerabilityAffectedPackageFact(
			"fact:10095:affected",
			goCrossScopeTestAdvisory,
			goCrossScopeTestPackage,
			"gomod",
			goCrossScopeTestModule,
			"0",
			"1.2.0",
		),
		packageConsumptionFactWithRange(
			"fact:10095:consumption",
			goCrossScopeTestPackage,
			goCrossScopeTestRepository,
			"v1.1.0",
		),
	}
}

// goCrossScopeEvidenceSeed mirrors the go-reachability generation of
// seedGoFenceChain: module evidence plus a not_called govulncheck verdict.
func goCrossScopeEvidenceSeed() []facts.Envelope {
	return []facts.Envelope{
		{
			ScopeID:      goCrossScopeTestScope,
			GenerationID: "g1",
			FactID:       "fact:10095:module",
			FactKind:     chain.VulnerabilityGoModuleEvidenceFactKind,
			Payload: map[string]any{
				"repository_id": goCrossScopeTestRepository, "relative_path": "go.mod",
				"module_path": goCrossScopeTestModule, "required_version": "v1.1.0",
				"indirect": false, "replacement_path": "", "replacement_version": "",
				"ecosystem": "go-module", "package_id": "go-module/proxy.golang.org/" + goCrossScopeTestModule,
				"package_name": goCrossScopeTestModule, "line_number": 5,
			},
		},
		{
			ScopeID:      goCrossScopeTestScope,
			GenerationID: "g1",
			FactID:       "fact:10095:reach",
			FactKind:     chain.VulnerabilityGoCallReachabilityFactKind,
			Payload: map[string]any{
				"repository_id": goCrossScopeTestRepository, "osv_id": goCrossScopeTestAdvisory,
				"deepest_module": goCrossScopeTestModule, "deepest_package": "",
				"deepest_symbol": "", "reachability_level": "not_called",
				"trace": []any{map[string]any{"module": goCrossScopeTestModule, "version": "v1.1.0"}},
			},
		},
	}
}

func goCrossScopeTestIntent() reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:     "intent-10095",
		ScopeID:      "scope:10095:osv",
		GenerationID: "g1",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	}
}

func findGoCrossScopeFinding(t *testing.T, findings []SupplyChainImpactFinding) SupplyChainImpactFinding {
	t.Helper()
	for _, finding := range findings {
		if normalizedSupplyChainVersionEcosystem(finding.Ecosystem) == "gomod" {
			return finding
		}
	}
	t.Fatalf("no gomod finding in %d findings", len(findings))
	return SupplyChainImpactFinding{}
}

// TestGoCrossScopeEvidenceClassifiesNotCalled pins the classifier half of
// #10095: with the Go evidence in hand, the holder's copy reads not_called.
// This holds today and guards the seed the loader test builds on.
func TestGoCrossScopeEvidenceClassifiesNotCalled(t *testing.T) {
	t.Parallel()

	combined := append(goCrossScopeHolderSeed(), goCrossScopeEvidenceSeed()...)
	findings := BuildSupplyChainImpactFindings(combined)
	got := findGoCrossScopeFinding(t, findings)
	if got.RuntimeReachability != string(GoVulnReachabilityNotCalled) {
		t.Fatalf("RuntimeReachability = %q, want not_called", got.RuntimeReachability)
	}
}

// TestSupplyChainImpactHandlerLoadsGoReachabilityEvidenceCrossScope is the
// #10095 acceptance test at the handler level: a holder pass with no Go
// facts of its own reads not_called once the cross-scope stage loads the
// go-reachability scope's active generation. Dropping the stage (or the
// loader method) must fail this test.
func TestSupplyChainImpactHandlerLoadsGoReachabilityEvidenceCrossScope(t *testing.T) {
	t.Parallel()

	loader := &goCrossScopeBackedLoader{
		scopeFacts: goCrossScopeHolderSeed(),
		goFacts:    goCrossScopeEvidenceSeed(),
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FencingTokenIssuer: newTestImpactFencingTokenIssuer(), FactLoader: loader, Writer: writer}

	_, err := handler.Handle(context.Background(), goCrossScopeTestIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if loader.goCalls != 1 {
		t.Fatalf("ListActiveGoReachabilityFacts() calls = %d, want 1", loader.goCalls)
	}
	if len(loader.goFilters) != 1 {
		t.Fatalf("Go reachability filters = %d, want 1", len(loader.goFilters))
	}
	repositoryIDs := loader.goFilters[0].RepositoryIDs
	if len(repositoryIDs) != 1 || repositoryIDs[0] != goCrossScopeTestRepository {
		t.Fatalf("Go reachability repository filter = %v, want [%s]", repositoryIDs, goCrossScopeTestRepository)
	}
	got := findGoCrossScopeFinding(t, writer.write.Findings)
	if got.RuntimeReachability != string(GoVulnReachabilityNotCalled) {
		t.Fatalf("RuntimeReachability = %q, want not_called", got.RuntimeReachability)
	}
}

// TestSupplyChainImpactHandlerWithoutGoLoaderReadsUnknown pins the mutant:
// a loader with no Go method leaves the holder's copy at unknown (no Go
// module or govulncheck evidence joined). It passes before and after #10095
// and fails if the handler ever requires the loader method.
func TestSupplyChainImpactHandlerWithoutGoLoaderReadsUnknown(t *testing.T) {
	t.Parallel()

	loader := &manifestBackedSupplyChainImpactLoader{scopeFacts: goCrossScopeHolderSeed()}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FencingTokenIssuer: newTestImpactFencingTokenIssuer(), FactLoader: loader, Writer: writer}

	_, err := handler.Handle(context.Background(), goCrossScopeTestIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	got := findGoCrossScopeFinding(t, writer.write.Findings)
	if got.RuntimeReachability != string(GoVulnReachabilityUnknown) {
		t.Fatalf("RuntimeReachability = %q, want unknown without the Go loader", got.RuntimeReachability)
	}
}

// TestSupplyChainImpactHandlerLoadsGoReachabilityEvidenceForCVEAlias covers
// the alias case the fence seed does not: the advisory joins on the GO id
// while the finding is keyed by its CVE alias. The alias copy must still
// read not_called.
func TestSupplyChainImpactHandlerLoadsGoReachabilityEvidenceForCVEAlias(t *testing.T) {
	t.Parallel()

	loader := &goCrossScopeBackedLoader{
		scopeFacts: []facts.Envelope{
			{
				FactID:   "fact:10095:alias-cve",
				FactKind: chain.VulnerabilityCVEFactKind,
				Payload: map[string]any{
					"cve_id": goCrossScopeTestCVEAlias, "advisory_id": goCrossScopeTestAdvisory,
					"cvss_score": 7.5, "aliases": []any{goCrossScopeTestCVEAlias, goCrossScopeTestAdvisory},
				},
			},
			{
				FactID:   "fact:10095:alias-affected",
				FactKind: chain.VulnerabilityAffectedPackageFactKind,
				Payload: map[string]any{
					"cve_id": goCrossScopeTestCVEAlias, "advisory_id": goCrossScopeTestAdvisory,
					"package_id": goCrossScopeTestPackage, "ecosystem": "gomod",
					"package_name": goCrossScopeTestModule, "purl": "pkg:golang/" + goCrossScopeTestModule,
					"affected_versions": []any{"0"}, "fixed_versions": []any{"1.2.0"},
				},
			},
			packageConsumptionFactWithRange(
				"fact:10095:alias-consumption",
				goCrossScopeTestPackage,
				goCrossScopeTestRepository,
				"v1.1.0",
			),
		},
		goFacts: goCrossScopeEvidenceSeed(),
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FencingTokenIssuer: newTestImpactFencingTokenIssuer(), FactLoader: loader, Writer: writer}

	_, err := handler.Handle(context.Background(), goCrossScopeTestIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	var got *SupplyChainImpactFinding
	for i, finding := range writer.write.Findings {
		if finding.CVEID == goCrossScopeTestCVEAlias && normalizedSupplyChainVersionEcosystem(finding.Ecosystem) == "gomod" {
			got = &writer.write.Findings[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("no gomod finding for %s in %d findings", goCrossScopeTestCVEAlias, len(writer.write.Findings))
	}
	if got.RuntimeReachability != string(GoVulnReachabilityNotCalled) {
		t.Fatalf("RuntimeReachability = %q, want not_called for the CVE alias copy", got.RuntimeReachability)
	}
}
