package reducer

import (
	"context"
	"errors"
	"io"
	"strings"

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

// factPayloadValueLoader narrows large fact-kind scans with a top-level
// payload allowlist. Reducer domains use it only when the payload predicate is
// part of their correctness contract, such as content entity type.
type factPayloadValueLoader interface {
	ListFactsByKindAndPayloadValue(
		ctx context.Context,
		scopeID string,
		generationID string,
		factKind string,
		payloadKey string,
		payloadValues []string,
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
		envelopes, err := typed.ListFactsByKind(ctx, scopeID, generationID, factKinds)
		if err != nil {
			return nil, classifyFactLoadError(err)
		}
		return envelopes, nil
	}
	envelopes, err := loader.ListFacts(ctx, scopeID, generationID)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return envelopes, nil
}

func loadFactsForKindAndPayloadValue(
	ctx context.Context,
	loader FactLoader,
	scopeID string,
	generationID string,
	factKind string,
	payloadKey string,
	payloadValues []string,
) ([]facts.Envelope, error) {
	factKind = strings.TrimSpace(factKind)
	payloadKey = strings.TrimSpace(payloadKey)
	payloadValues = cleanFactFilterValues(payloadValues)
	if factKind == "" || payloadKey == "" || len(payloadValues) == 0 {
		return nil, nil
	}

	if typed, ok := loader.(factPayloadValueLoader); ok {
		envelopes, err := typed.ListFactsByKindAndPayloadValue(
			ctx,
			scopeID,
			generationID,
			factKind,
			payloadKey,
			payloadValues,
		)
		if err != nil {
			return nil, classifyFactLoadError(err)
		}
		return envelopes, nil
	}
	return loadFactsForKinds(ctx, loader, scopeID, generationID, []string{factKind})
}

func cleanFactFilterValues(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}

// classifyFactLoadError preserves semantic errors while marking transient
// database stream interruptions retryable for the durable reducer queue.
func classifyFactLoadError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, io.ErrUnexpectedEOF) ||
		strings.Contains(strings.ToLower(err.Error()), "unexpected eof") {
		return retryableFactLoadError{err: err}
	}
	return err
}

type retryableFactLoadError struct {
	err error
}

func (e retryableFactLoadError) Error() string {
	return e.err.Error()
}

func (e retryableFactLoadError) Unwrap() error {
	return e.err
}

func (retryableFactLoadError) Retryable() bool {
	return true
}

func (retryableFactLoadError) FailureClass() string {
	return "fact_load_transient"
}
