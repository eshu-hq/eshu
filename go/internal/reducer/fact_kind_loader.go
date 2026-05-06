package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	factKindContentEntity = "content_entity"
	factKindFile          = "file"
	factKindParsedFile    = "parsed_file_data"
	factKindRepository    = "repository"
)

// factKindLoader is an optional fast path for handlers that need only a small
// subset of a generation's facts. Loaders that do not implement it keep the
// existing full-generation behavior.
type factKindLoader interface {
	ListFactsByKind(
		ctx context.Context,
		scopeID string,
		generationID string,
		factKinds []string,
	) ([]facts.Envelope, error)
}

// loadFactsForKinds uses a bounded fact-kind query when the backing store
// supports it, falling back to the full FactLoader contract for test doubles
// and older loader implementations.
func loadFactsForKinds(
	ctx context.Context,
	loader FactLoader,
	scopeID string,
	generationID string,
	factKinds []string,
) ([]facts.Envelope, error) {
	if typed, ok := loader.(factKindLoader); ok {
		return typed.ListFactsByKind(ctx, scopeID, generationID, factKinds)
	}
	return loader.ListFacts(ctx, scopeID, generationID)
}
