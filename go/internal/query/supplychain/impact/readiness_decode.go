// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

const (
	sourceSnapshotFamilyMarker    = "vulnerability.source_snapshot"
	sourceStateFamilyMarker       = "vulnerability.source_state"
	unsupportedTargetFamilyMarker = "vulnerability.unsupported_target"
)

func decodeSourceSnapshots(raw sql.NullString) ([]SourceSnapshot, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var snapshots []SourceSnapshot
	if err := json.Unmarshal([]byte(raw.String), &snapshots); err != nil {
		return nil, fmt.Errorf("decode vulnerability source snapshot metadata: %w", err)
	}
	return snapshots, nil
}

func decodeSourceStates(raw sql.NullString) ([]SourceState, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var states []SourceState
	if err := json.Unmarshal([]byte(raw.String), &states); err != nil {
		return nil, fmt.Errorf("decode vulnerability source state metadata: %w", err)
	}
	return states, nil
}

func decodeUnsupportedTargets(raw sql.NullString) ([]UnsupportedTarget, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var targets []UnsupportedTarget
	if err := json.Unmarshal([]byte(raw.String), &targets); err != nil {
		return nil, fmt.Errorf("decode vulnerability unsupported target metadata: %w", err)
	}
	return targets, nil
}
