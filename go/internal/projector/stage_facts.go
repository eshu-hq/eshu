package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Fact kind constants matching the Go git collector output. The collector emits
// lowercase fact kinds ("repository", "file", "content_entity"). NormalizeFactKind
// strips the legacy "Fact" suffix (e.g. "repositoryFact" → "repository").
const (
	FactKindFileObserved         = "file"
	FactKindParsedEntityObserved = "content_entity"
	FactKindRepositoryObserved   = "repository"
)

// NormalizeFactKind returns a stable fact kind by stripping the legacy "Fact"
// suffix used during the transition period.
func NormalizeFactKind(kind string) string {
	return strings.TrimSuffix(kind, "Fact")
}

// FilterFileFacts returns file observation facts in stable insertion order
// with deduplication by FactID.
func FilterFileFacts(envelopes []facts.Envelope) []facts.Envelope {
	return filterFactsByKind(envelopes, FactKindFileObserved)
}

// FilterEntityFacts returns parsed-entity facts in stable insertion order
// with deduplication by FactID.
func FilterEntityFacts(envelopes []facts.Envelope) []facts.Envelope {
	return filterFactsByKind(envelopes, FactKindParsedEntityObserved)
}

// FilterRepositoryFacts returns repository observation facts in stable
// insertion order with deduplication by FactID.
func FilterRepositoryFacts(envelopes []facts.Envelope) []facts.Envelope {
	return filterFactsByKind(envelopes, FactKindRepositoryObserved)
}

func filterFactsByKind(envelopes []facts.Envelope, kind string) []facts.Envelope {
	seen := make(map[string]struct{}, len(envelopes))
	var result []facts.Envelope
	for i := range envelopes {
		if NormalizeFactKind(envelopes[i].FactKind) != kind {
			continue
		}
		if _, ok := seen[envelopes[i].FactID]; ok {
			continue
		}
		seen[envelopes[i].FactID] = struct{}{}
		result = append(result, envelopes[i])
	}
	return result
}
