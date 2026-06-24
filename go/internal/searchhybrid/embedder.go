// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchhybrid

import "context"

// Embedder produces a fixed-dimension embedding for a piece of text.
//
// Implementations MUST be deterministic for a given input. This package never
// calls hosted services directly; governed runtime adapters that implement this
// port live outside searchhybrid. The concrete model is supplied by a caller,
// and this package still serves BM25 retrieval with no embedder at all.
type Embedder interface {
	// Embed returns the embedding vector for text. The returned slice length
	// must equal Dimensions for every call.
	Embed(ctx context.Context, text string) ([]float64, error)
	// Dimensions is the fixed vector length produced by Embed.
	Dimensions() int
}
