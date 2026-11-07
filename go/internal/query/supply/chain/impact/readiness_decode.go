// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

const (
	sourceSnapshotFamilyMarker    = "vulnerability.source_snapshot"
	sourceStateFamilyMarker       = "vulnerability.source_state"
	sourceStateOverflowMarker     = "vulnerability.source_state_overflow"
	sourceStateInvalidMarker      = "vulnerability.source_state_invalid"
	unsupportedTargetFamilyMarker = "vulnerability.unsupported_target"
)

func decodeSourceSnapshots(raw sql.NullString) ([]packet.SourceSnapshot, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var snapshots []packet.SourceSnapshot
	if err := json.Unmarshal([]byte(raw.String), &snapshots); err != nil {
		return nil, fmt.Errorf("decode vulnerability source snapshot metadata: %w", err)
	}
	return snapshots, nil
}

func decodeUnsupportedTargets(raw sql.NullString) ([]packet.UnsupportedTarget, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var targets []packet.UnsupportedTarget
	if err := json.Unmarshal([]byte(raw.String), &targets); err != nil {
		return nil, fmt.Errorf("decode vulnerability unsupported target metadata: %w", err)
	}
	return targets, nil
}
