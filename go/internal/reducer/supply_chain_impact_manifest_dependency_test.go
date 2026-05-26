package reducer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type manifestBackedSupplyChainImpactLoader struct {
	scopeFacts        []facts.Envelope
	activeFacts       []facts.Envelope
	manifestFacts     []facts.Envelope
	manifestCalls     int
	manifestEcosystem []string
	manifestNames     []string
	kindCalls         [][]string
	filters           []SupplyChainImpactFactFilter
}

func (s *manifestBackedSupplyChainImpactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *manifestBackedSupplyChainImpactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	kinds []string,
) ([]facts.Envelope, error) {
	s.kindCalls = append(s.kindCalls, append([]string(nil), kinds...))
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *manifestBackedSupplyChainImpactLoader) ListActiveSupplyChainImpactFacts(
	_ context.Context,
	filter SupplyChainImpactFactFilter,
) ([]facts.Envelope, error) {
	s.filters = append(s.filters, filter)
	return append([]facts.Envelope(nil), s.activeFacts...), nil
}

func (s *manifestBackedSupplyChainImpactLoader) ListActivePackageManifestDependencyFacts(
	_ context.Context,
	ecosystems []string,
	packageNames []string,
) ([]facts.Envelope, error) {
	s.manifestCalls++
	s.manifestEcosystem = append([]string(nil), ecosystems...)
	s.manifestNames = append([]string(nil), packageNames...)
	return append([]facts.Envelope(nil), s.manifestFacts...), nil
}

func TestSupplyChainImpactHandlerUsesManifestDependencyBeforeRegistryCorrelation(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 26, 8, 0, 0, 0, time.UTC)
	loader := &manifestBackedSupplyChainImpactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-undici", "CVE-2026-0001", 7.5),
			vulnerabilityAffectedPackageFact(
				"affected-undici",
				"CVE-2026-0001",
				"npm://registry.npmjs.org/undici",
				"npm",
				"undici",
				"6.23.0",
				"6.23.1",
			),
		},
		manifestFacts: []facts.Envelope{
			packageManifestDependencyFactWithMetadata(
				testImpactRepositoryID,
				"api",
				"package-lock.json",
				"undici",
				"npm",
				"6.23.0",
				observedAt,
				map[string]any{
					"section":           "package-lock",
					"lockfile":          true,
					"dependency_path":   []any{"fetch-client", "undici"},
					"dependency_depth":  2,
					"direct_dependency": false,
				},
			),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-impact-undici",
		ScopeID:      "vuln-intel://osv/npm/undici@6.23.0",
		GenerationID: "generation-impact-undici",
		SourceSystem: "vulnerability_intelligence",
		Domain:       DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if loader.manifestCalls != 1 {
		t.Fatalf("ListActivePackageManifestDependencyFacts() calls = %d, want 1", loader.manifestCalls)
	}
	if got, want := strings.Join(loader.manifestEcosystem, ","), "npm"; got != want {
		t.Fatalf("manifest ecosystems = %q, want %q", got, want)
	}
	if got, want := strings.Join(loader.manifestNames, ","), "undici"; got != want {
		t.Fatalf("manifest names = %q, want %q", got, want)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if got, want := len(writer.write.Findings), 1; got != want {
		t.Fatalf("len(Findings) = %d, want %d", got, want)
	}
	finding := writer.write.Findings[0]
	assertSupplyChainImpactStatus(t, finding, SupplyChainImpactAffectedExact)
	if finding.RepositoryID != testImpactRepositoryID {
		t.Fatalf("RepositoryID = %q, want %q", finding.RepositoryID, testImpactRepositoryID)
	}
	if finding.ObservedVersion != "6.23.0" {
		t.Fatalf("ObservedVersion = %q, want lockfile version 6.23.0", finding.ObservedVersion)
	}
	if !strings.Contains(strings.Join(finding.EvidencePath, " -> "), factKindContentEntity) {
		t.Fatalf("EvidencePath = %#v, want content_entity source dependency evidence", finding.EvidencePath)
	}
}
