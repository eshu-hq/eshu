// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator //nolint:dirgate // Root owns scheduling, policy, audit, and durable admission.

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/vulnsource"
)

// GHSAWindowCursorReader loads the stored GHSA rolling cursor of one
// collector instance's advisory-type stream: the committed advance-to the
// next window plans from, plus whether any window ever committed. It
// backs plan-from-cursor for the rolling GHSA derivation
// (eshu-hq/eshu#10037) and matches the cursor store's read signature, so
// the store satisfies it directly. A read failure is returned, never
// answered with unset: planning from zero on an error would re-collect
// the backlog over committed windows.
type GHSAWindowCursorReader interface {
	ReadGHSAWindowCursor(ctx context.Context, collectorInstanceID string, advisoryType string) (cursorUnixMicro int64, cursorSet bool, err error)
}

// vulnerabilityGHSAWindowCursors is the one rolling-cursor read for the
// GHSA window derivation: one stored cursor per advisory type. A disabled
// derivation reads nothing. An enabled derivation without a reader fails
// loudly instead of silently planning every window from zero.
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
	cursors := make(map[string]vulnerability.GHSAWindowCursor, 2)
	for _, advisoryType := range []string{vulnsource.GHSAAdvisoryTypeReviewed, vulnsource.GHSAAdvisoryTypeMalware} {
		cursor, set, err := s.GHSAWindowCursorReader.ReadGHSAWindowCursor(ctx, instanceID, advisoryType)
		if err != nil {
			return nil, fmt.Errorf("read ghsa window cursor: %w", err)
		}
		cursors[advisoryType] = vulnerability.GHSAWindowCursor{UnixMicro: cursor, Set: set}
	}
	return cursors, nil
}
