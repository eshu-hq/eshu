package collector

import (
	"context"
	"fmt"
)

// PriorityRepositorySelector asks selectors in order and returns the first
// non-empty repository batch.
type PriorityRepositorySelector struct {
	Selectors []RepositorySelector
}

// SelectRepositories returns the first non-empty batch, or the last empty
// batch when every selector has no work.
func (s PriorityRepositorySelector) SelectRepositories(ctx context.Context) (SelectionBatch, error) {
	if len(s.Selectors) == 0 {
		return SelectionBatch{}, fmt.Errorf("priority repository selector requires at least one selector")
	}
	var lastEmpty SelectionBatch
	for _, selector := range s.Selectors {
		if selector == nil {
			continue
		}
		batch, err := selector.SelectRepositories(ctx)
		if err != nil {
			return SelectionBatch{}, err
		}
		if len(batch.Repositories) > 0 {
			return batch, nil
		}
		lastEmpty = batch
	}
	return lastEmpty, nil
}
