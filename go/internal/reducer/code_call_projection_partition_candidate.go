// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer //nolint:dirgate // code-call projection runner stays in root (#6609): it needs the root lease and shared-projection machinery that sharedintent/doc.go pins here

import "context"

// CodeCallProjectionPartitionCandidateReader reads pending code-call rows that
// hash into one leased partition. It is a selection optimization only; selected
// work is still loaded through the acceptance-unit partition reader before
// graph writes.
type CodeCallProjectionPartitionCandidateReader interface {
	ListPendingDomainPartitionIntents(
		ctx context.Context,
		domain string,
		partitionID int,
		partitionCount int,
		limit int,
	) ([]SharedProjectionIntentRow, error)
}

// CodeCallProjectionUnhashedCandidateReader reads pending legacy rows that were
// inserted before partition_hash was available.
type CodeCallProjectionUnhashedCandidateReader interface {
	ListPendingDomainUnhashedIntents(
		ctx context.Context,
		domain string,
		limit int,
	) ([]SharedProjectionIntentRow, error)
}
