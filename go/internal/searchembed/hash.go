package searchembed

import (
	"context"
	"errors"
	"hash/fnv"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
)

// DefaultDimensions is the local hash embedder's default vector width.
const DefaultDimensions = 256

// MaxUniqueTerms bounds one embedding request after token normalization.
const MaxUniqueTerms = 4096

// HashEmbedder maps normalized search terms into a fixed-width feature vector.
type HashEmbedder struct {
	dimensions int
}

var _ searchhybrid.Embedder = (*HashEmbedder)(nil)

// NewHashEmbedder constructs a deterministic local embedder with dimensions.
func NewHashEmbedder(dimensions int) (*HashEmbedder, error) {
	if dimensions <= 0 {
		return nil, errors.New("hash embedder dimensions must be positive")
	}
	return &HashEmbedder{dimensions: dimensions}, nil
}

// Dimensions returns the fixed vector width produced by Embed.
func (embedder *HashEmbedder) Dimensions() int {
	if embedder == nil {
		return 0
	}
	return embedder.dimensions
}

// Embed returns a deterministic feature-hash embedding for text.
func (embedder *HashEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	if embedder == nil || embedder.dimensions <= 0 {
		return nil, errors.New("hash embedder is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	vector := make([]float64, embedder.dimensions)
	terms := searchhybrid.QueryTerms(text)
	keys := make([]string, 0, len(terms))
	for term := range terms {
		keys = append(keys, term)
	}
	sort.Strings(keys)
	if len(keys) > MaxUniqueTerms {
		keys = keys[:MaxUniqueTerms]
	}
	for _, term := range keys {
		index, sign := feature(term, embedder.dimensions)
		vector[index] += sign * float64(terms[term])
	}
	return vector, nil
}

func feature(term string, dimensions int) (int, float64) {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(term))
	sum := hash.Sum64()
	sign := 1.0
	if sum&1 == 1 {
		sign = -1.0
	}
	return int((sum >> 1) % uint64(dimensions)), sign
}
