// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factload

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// FactLoader loads fact envelopes for one scope generation.
type FactLoader interface {
	ListFacts(ctx context.Context, scopeID, generationID string) ([]facts.Envelope, error)
}
