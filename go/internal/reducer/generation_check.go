package reducer

import "context"

// GenerationFreshnessCheck reports whether the given generation is still
// the active generation for the scope. Returns (true, nil) if current,
// (false, nil) if superseded, or (false, err) on lookup failure.
type GenerationFreshnessCheck func(ctx context.Context, scopeID, generationID string) (bool, error)

// PriorGenerationCheck reports whether the scope has any generation before the
// given generation. Retract paths use it to skip no-op cleanup on first writes
// while preserving cleanup on refreshes and retries.
type PriorGenerationCheck func(ctx context.Context, scopeID, generationID string) (bool, error)
