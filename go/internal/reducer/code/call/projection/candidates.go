// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// PartitionCandidateReader reads pending code-call rows that
// hash into one leased partition. It is a selection optimization only; selected
// work is still loaded through the acceptance-unit partition reader before
// graph writes.
type PartitionCandidateReader interface {
	ListPendingDomainPartitionIntents(
		ctx context.Context,
		domain string,
		partitionID int,
		partitionCount int,
		limit int,
	) ([]sharedintent.Row, error)
}

// UnhashedCandidateReader reads pending legacy rows that were
// inserted before partition_hash was available.
type UnhashedCandidateReader interface {
	ListPendingDomainUnhashedIntents(
		ctx context.Context,
		domain string,
		limit int,
	) ([]sharedintent.Row, error)
}
