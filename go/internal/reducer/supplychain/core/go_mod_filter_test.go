// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// Regression scope for eshu-hq/eshu#10032 shape (A): a git-repo scope whose
// only manifest evidence is go.mod content entities must still seed the
// round-1 evidence filter with the canonical go package identity, so the
// affected_package key probes load the co-loaded advisory rows Go findings
// require.
const (
	goModFilterTestRepository = "repository:r_10032a1b"
	goModFilterTestModule     = "golang.org/x/text"
	goModFilterTestAdvisory   = "GO-2026-10032"
	goModFilterTestPackageID  = "gomod://proxy.golang.org/golang.org/x/text"
	goModFilterTestPURL       = "pkg:golang/golang.org/x/text"
)

// TestSupplyChainImpactFilterSeedsGoModPackageIDsFromContentEntities pins the
// shape (A) contract: one go.mod require row seeds the canonical PackageID
// and PURL arms, alongside the repository arm the content-entity case already
// fed.
func TestSupplyChainImpactFilterSeedsGoModPackageIDsFromContentEntities(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	envelopes := []facts.Envelope{
		packageManifestDependencyFactWithMetadata(
			goModFilterTestRepository,
			"go-service",
			"go.mod",
			goModFilterTestModule,
			"go",
			"v0.3.7",
			observedAt,
			map[string]any{
				"section":           "require",
				"direct_dependency": true,
				"dependency_path":   []any{goModFilterTestModule},
				"dependency_depth":  1,
			},
		),
	}

	filter := supplyChainImpactFilter(envelopes)

	if got, want := filter.PackageIDs, []string{goModFilterTestPackageID}; !slices.Equal(got, want) {
		t.Fatalf("PackageIDs = %v, want %v (canonical gomod identity of the go.mod require)", got, want)
	}
	if got, want := filter.PURLs, []string{goModFilterTestPURL}; !slices.Equal(got, want) {
		t.Fatalf("PURLs = %v, want %v (versionless pkg:golang form the advisory rows carry)", got, want)
	}
	if !slices.Contains(filter.RepositoryIDs, goModFilterTestRepository) {
		t.Fatalf("RepositoryIDs = %v, want the repository arm kept", filter.RepositoryIDs)
	}
}

// TestSupplyChainImpactFilterSkipsNonGoManifestPackageIDs pins the negative
// half of the shape (A) contract: a non-go manifest row and a go.sum
// checksum-only row seed no package identity, and go-ecosystem rows with a
// blank or unparseable name seed nothing (fail closed). The npm row keeps
// its repository arm; the checksum row is not dependency evidence at all.
func TestSupplyChainImpactFilterSkipsNonGoManifestPackageIDs(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	envelopes := []facts.Envelope{
		packageManifestDependencyFactWithMetadata(
			goModFilterTestRepository,
			"go-service",
			"go.mod",
			"   ",
			"go",
			"v0.3.7",
			observedAt,
			map[string]any{
				"section":           "require",
				"direct_dependency": true,
			},
		),
		packageManifestDependencyFactWithMetadata(
			goModFilterTestRepository,
			"go-service",
			"go.mod",
			"!!!",
			"go",
			"v0.3.7",
			observedAt,
			map[string]any{
				"section":           "require",
				"direct_dependency": true,
			},
		),
		packageManifestDependencyFactWithMetadata(
			goModFilterTestRepository,
			"go-service",
			"package-lock.json",
			"undici",
			"npm",
			"6.23.0",
			observedAt,
			map[string]any{
				"section":  "package-lock",
				"lockfile": true,
			},
		),
		{
			FactID:      "checksum-only-go-sum",
			FactKind:    factload.FactKindContentEntity,
			ObservedAt:  observedAt,
			IsTombstone: false,
			Payload: map[string]any{
				"repo_id":       goModFilterTestRepository,
				"repo_name":     "go-service",
				"relative_path": "go.sum",
				"entity_type":   "Variable",
				"entity_name":   goModFilterTestModule,
				"entity_metadata": map[string]any{
					"config_kind":     "dependency_checksum",
					"package_manager": "go",
					"value":           "v0.3.7",
					"section":         "go-sum",
					"lockfile":        true,
					"ambiguous":       true,
				},
			},
		},
	}

	filter := supplyChainImpactFilter(envelopes)

	if len(filter.PackageIDs) != 0 {
		t.Fatalf("PackageIDs = %v, want empty (no go dependency evidence)", filter.PackageIDs)
	}
	if len(filter.PURLs) != 0 {
		t.Fatalf("PURLs = %v, want empty (no go dependency evidence)", filter.PURLs)
	}
	if !slices.Contains(filter.RepositoryIDs, goModFilterTestRepository) {
		t.Fatalf("RepositoryIDs = %v, want the npm repository arm kept", filter.RepositoryIDs)
	}
}

// goModFilterBackedLoader models the round-1 storage key probes for the
// shape (A) handler test: advisory rows come back only when the filter
// carries the derived canonical package key, exactly like the
// affected_package_package_id probe the branch catalog runs when its bound
// list is non-empty and skips when it is empty. Repository-only filters load
// nothing: repositoryGrowthKinds holds no advisory kind, so RepositoryIDs
// alone never admit advisory rows in production either.
type goModFilterBackedLoader struct {
	scopeFacts      []facts.Envelope
	advisoryFacts   []facts.Envelope
	repositoryFacts []facts.Envelope
	filters         []SupplyChainImpactFactFilter
}

func (s *goModFilterBackedLoader) ListFacts(
	_ context.Context,
	_, _ string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *goModFilterBackedLoader) ListFactsByKind(
	_ context.Context,
	_, _ string,
	_ []string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *goModFilterBackedLoader) ListActiveSupplyChainImpactFacts(
	_ context.Context,
	filter SupplyChainImpactFactFilter,
) ([]facts.Envelope, bool, error) {
	s.filters = append(s.filters, filter)
	if !slices.Contains(filter.PackageIDs, goModFilterTestPackageID) {
		return nil, false, nil
	}
	return append([]facts.Envelope(nil), s.advisoryFacts...), false, nil
}

func (s *goModFilterBackedLoader) ListActiveRepositoryFacts(
	context.Context,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.repositoryFacts...), nil
}

func (s *goModFilterBackedLoader) ListActivePackageManifestDependencyFactsWithRefusals(
	context.Context,
	[]string,
	[]string,
) ([]facts.Envelope, bool, error) {
	return nil, false, nil
}

// TestSupplyChainImpactHandlerLoadsAdvisoriesFromGoModSeededFilter is the
// end-to-end half of the shape (A) regression: a git-repo scope holding only
// gomod content entities plus go module evidence drives a round-1 filter
// that loads the advisory rows, and the Go finding goes from 0 (no advisory
// rows without the seed) to 1.
func TestSupplyChainImpactHandlerLoadsAdvisoriesFromGoModSeededFilter(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	loader := &goModFilterBackedLoader{
		scopeFacts: []facts.Envelope{
			packageManifestDependencyFactWithMetadata(
				goModFilterTestRepository,
				"go-service",
				"go.mod",
				goModFilterTestModule,
				"go",
				"v0.3.7",
				observedAt,
				map[string]any{
					"section":           "require",
					"direct_dependency": true,
					"dependency_path":   []any{goModFilterTestModule},
					"dependency_depth":  1,
				},
			),
			goModuleEvidenceEnvelope(t, goModFilterTestRepository, "go.mod", goModFilterTestModule, "v0.3.7", false, "", ""),
		},
		advisoryFacts: []facts.Envelope{
			vulnerabilityCVEFact("go-filter-cve", goModFilterTestAdvisory, 7.5),
			vulnerabilityAffectedPackageGoRangeFact(
				"go-filter-affected",
				goModFilterTestAdvisory,
				goModFilterTestPackageID,
				goModFilterTestModule,
				"0.3.8",
			),
		},
		repositoryFacts: []facts.Envelope{
			packageSourceRepositoryFact(
				goModFilterTestRepository,
				"go-service",
				"https://github.com/acme/go-service",
				false,
				observedAt,
			),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FencingTokenIssuer: newTestImpactFencingTokenIssuer(), FactLoader: loader, Writer: writer}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-gomod-filter-10032",
		ScopeID:      goModFilterTestRepository,
		GenerationID: "generation-gomod-filter-10032",
		SourceSystem: "git",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "go.mod dependency evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if len(loader.filters) == 0 {
		t.Fatal("no active-evidence reads issued, want the round-1 filter to run the probes")
	}
	roundOne := loader.filters[0]
	if !slices.Contains(roundOne.PackageIDs, goModFilterTestPackageID) {
		t.Fatalf("round-1 PackageIDs = %v, want it to contain %v (the go.mod seed the advisory probes bind)", roundOne.PackageIDs, goModFilterTestPackageID)
	}
	if got, want := roundOne.PURLs, []string{goModFilterTestPURL}; !slices.Equal(got, want) {
		t.Fatalf("round-1 PURLs = %v, want %v (the versionless advisory purl form)", got, want)
	}
	findings := supplyChainImpactFindingsByCVE(writer.write.Findings)[goModFilterTestAdvisory]
	if findings.CVEID == "" {
		t.Fatalf("no finding for %s in %d findings, want the co-loaded advisory rows to prove impact",
			goModFilterTestAdvisory, len(writer.write.Findings))
	}
	assertSupplyChainImpactStatus(t, findings, SupplyChainImpactAffectedExact)
	if findings.ObservedVersion != "v0.3.7" || findings.RequestedRange != "v0.3.7" {
		t.Fatalf("version readback = observed %q requested %q, want go.mod require version on both",
			findings.ObservedVersion, findings.RequestedRange)
	}
	for _, want := range []string{"go-filter-cve", "go-filter-affected"} {
		if !slices.Contains(findings.EvidenceFactIDs, want) {
			t.Fatalf("EvidenceFactIDs = %#v, want co-loaded advisory fact %s", findings.EvidenceFactIDs, want)
		}
	}
}
