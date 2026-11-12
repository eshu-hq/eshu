// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
)

// GHSAWindowCursorReader loads the stored GHSA rolling cursors of one
// collector instance, keyed by advisory type: the committed advance-to each
// advisory type's next window plans from, plus whether any window ever
// committed. It backs plan-from-cursor for the rolling GHSA derivation
// (eshu-hq/eshu#10517). A read failure is returned, never answered with
// unset: planning from zero on an error would re-collect the backlog over
// committed windows (and write-once would then refuse those re-commits
// forever).
type GHSAWindowCursorReader interface {
	ReadGHSAWindowCursors(ctx context.Context, collectorInstanceID string) (map[string]vulnerability.GHSAWindowCursor, error)
}

// vulnerabilityGHSAWindowCursors is the one rolling-cursor read for the GHSA
// window derivation (eshu-hq/eshu#10517). A disabled derivation reads
// nothing. An enabled derivation without a reader fails loudly instead of
// silently planning every window from zero.
func (s Service) vulnerabilityGHSAWindowCursors(
	ctx context.Context,
	derivation vulnerability.GHSAWindowDerivation,
	instanceID string,
) (map[string]vulnerability.GHSAWindowCursor, error) {
	if !derivation.Enabled {
		return nil, nil
	}
	if s.GHSAWindowCursorReader == nil {
		return nil, fmt.Errorf("ghsa window cursor reader is required for rolling GHSA windows")
	}
	cursors, err := s.GHSAWindowCursorReader.ReadGHSAWindowCursors(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("read ghsa window cursors: %w", err)
	}
	if cursors == nil {
		return map[string]vulnerability.GHSAWindowCursor{}, nil
	}
	return cursors, nil
}
