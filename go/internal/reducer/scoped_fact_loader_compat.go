// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// This file is the transitional compatibility surface for the scoped fact
// loader that moved to [factload] (issue #6061). Reducer-root call sites and the
// external packages naming FactLoader keep their current spelling; each entry is
// deleted once its last caller has moved into a family subpackage.

// FactLoader loads fact envelopes for one scope generation.
type FactLoader = factload.FactLoader

// Fact-kind names the scoped loader filters on.
const (
	factKindContentEntity       = factload.FactKindContentEntity
	factKindFile                = factload.FactKindFile
	factKindParsedFile          = factload.FactKindParsedFile
	factKindRepository          = factload.FactKindRepository
	factKindCodeownersOwnership = factload.FactKindCodeownersOwnership
	factKindSubmodulePin        = factload.FactKindSubmodulePin
)

// loadFactsForKinds forwards to [factload.LoadFactsForKinds].
func loadFactsForKinds(
	ctx context.Context,
	loader FactLoader,
	scopeID string,
	generationID string,
	factKinds []string,
) ([]facts.Envelope, error) {
	return factload.LoadFactsForKinds(ctx, loader, scopeID, generationID, factKinds)
}

// loadFactsForKindAndPayloadValue forwards to
// [factload.LoadFactsForKindAndPayloadValue].
func loadFactsForKindAndPayloadValue(
	ctx context.Context,
	loader FactLoader,
	scopeID string,
	generationID string,
	factKind string,
	payloadKey string,
	payloadValues []string,
) ([]facts.Envelope, error) {
	return factload.LoadFactsForKindAndPayloadValue(
		ctx, loader, scopeID, generationID, factKind, payloadKey, payloadValues)
}

// classifyFactLoadError forwards to [factload.ClassifyFactLoadError].
func classifyFactLoadError(err error) error {
	return factload.ClassifyFactLoadError(err)
}

// cleanFactFilterValues forwards to [payloadcore.CleanFactFilterValues].
func cleanFactFilterValues(values []string) []string {
	return payloadcore.CleanFactFilterValues(values)
}
