// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factload

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// Fact-kind names the scoped loader filters on.
const (
	FactKindContentEntity       = "content_entity"
	FactKindFile                = "file"
	FactKindParsedFile          = "parsed_file_data"
	FactKindRepository          = "repository"
	FactKindCodeownersOwnership = "codeowners.ownership"
	FactKindSubmodulePin        = "submodule.pin"
)

// FactKindLoader is an optional fast path for handlers that need only a small
// subset of a generation's facts. Loaders that do not implement it keep the
// existing full-generation behavior.
type FactKindLoader interface {
	ListFactsByKind(
		ctx context.Context,
		scopeID string,
		generationID string,
		factKinds []string,
	) ([]facts.Envelope, error)
}

// FactPayloadValueLoader narrows large fact-kind scans with a top-level
// payload allowlist. Reducer domains use it only when the payload predicate is
// part of their correctness contract, such as content entity type.
type FactPayloadValueLoader interface {
	ListFactsByKindAndPayloadValue(
		ctx context.Context,
		scopeID string,
		generationID string,
		factKind string,
		payloadKey string,
		payloadValues []string,
	) ([]facts.Envelope, error)
}

// LoadFactsForKinds uses a bounded fact-kind query when the backing store
// supports it, falling back to the full FactLoader contract for test doubles
// and older loader implementations.
func LoadFactsForKinds(
	ctx context.Context,
	loader FactLoader,
	scopeID string,
	generationID string,
	factKinds []string,
) ([]facts.Envelope, error) {
	if typed, ok := loader.(FactKindLoader); ok {
		envelopes, err := typed.ListFactsByKind(ctx, scopeID, generationID, factKinds)
		if err != nil {
			return nil, ClassifyFactLoadError(err)
		}
		return envelopes, nil
	}
	envelopes, err := loader.ListFacts(ctx, scopeID, generationID)
	if err != nil {
		return nil, ClassifyFactLoadError(err)
	}
	return envelopes, nil
}

// LoadFactsForKindAndPayloadValue uses a bounded payload-value query when the
// backing store implements [FactPayloadValueLoader], and falls back to a
// kind-filtered [LoadFactsForKinds] read otherwise. A blank fact kind or
// payload key, or a value set that cleans to empty, short-circuits to
// (nil, nil).
func LoadFactsForKindAndPayloadValue(
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
	payloadValues = payloadcore.CleanFactFilterValues(payloadValues)
	if factKind == "" || payloadKey == "" || len(payloadValues) == 0 {
		return nil, nil
	}

	if typed, ok := loader.(FactPayloadValueLoader); ok {
		envelopes, err := typed.ListFactsByKindAndPayloadValue(
			ctx,
			scopeID,
			generationID,
			factKind,
			payloadKey,
			payloadValues,
		)
		if err != nil {
			return nil, ClassifyFactLoadError(err)
		}
		return envelopes, nil
	}
	return LoadFactsForKinds(ctx, loader, scopeID, generationID, []string{factKind})
}

// ClassifyFactLoadError preserves semantic errors while marking transient
// database stream interruptions retryable for the durable reducer queue.
func ClassifyFactLoadError(err error) error {
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
