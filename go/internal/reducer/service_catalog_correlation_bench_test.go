package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildServiceCatalogCorrelationDecisionsHandlesHighCardinalityFanout(t *testing.T) {
	t.Parallel()

	envelopes := serviceCatalogHighCardinalityFanoutFacts(128)
	decisions := BuildServiceCatalogCorrelationDecisions(envelopes)
	if got, want := len(decisions), 1; got != want {
		t.Fatalf("decisions = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.Outcome, ServiceCatalogCorrelationAmbiguous; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if got, want := len(decision.CandidateRepositoryIDs), 128; got != want {
		t.Fatalf("CandidateRepositoryIDs = %d, want %d", got, want)
	}
	if got, want := decision.CandidateRepositoryIDs[0], "repo-fanout-0000"; got != want {
		t.Fatalf("first candidate = %q, want %q", got, want)
	}
}

func BenchmarkBuildServiceCatalogCorrelationDecisionsHighCardinalityFanout(b *testing.B) {
	envelopes := serviceCatalogHighCardinalityFanoutFacts(4096)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decisions := BuildServiceCatalogCorrelationDecisions(envelopes)
		if len(decisions) != 1 || decisions[0].Outcome != ServiceCatalogCorrelationAmbiguous {
			b.Fatalf("unexpected decisions = %#v", decisions)
		}
		if len(decisions[0].CandidateRepositoryIDs) != 4096 {
			b.Fatalf("candidate repositories = %d, want 4096", len(decisions[0].CandidateRepositoryIDs))
		}
	}
}

func serviceCatalogHighCardinalityFanoutFacts(repoCount int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, repoCount+2)
	envelopes = append(
		envelopes,
		serviceCatalogEntityFact("entity-fanout", "component:default/fanout", "Fanout"),
		serviceCatalogRepositoryLinkFact("repo-link-fanout", "component:default/fanout", "https://github.com/acme/fanout.git"),
	)
	for i := 0; i < repoCount; i++ {
		envelopes = append(envelopes, repositoryFact(
			fmt.Sprintf("repo-fanout-%04d", i),
			fmt.Sprintf("fanout-%04d", i),
			"https://github.com/acme/fanout.git",
			false,
		))
	}
	return envelopes
}
