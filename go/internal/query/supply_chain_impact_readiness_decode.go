package query

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

const sourceSnapshotFamilyMarker = "vulnerability.source_snapshot"
const sourceStateFamilyMarker = "vulnerability.source_state"

func decodeSourceSnapshots(raw sql.NullString) ([]SupplyChainImpactSourceSnapshot, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var snapshots []SupplyChainImpactSourceSnapshot
	if err := json.Unmarshal([]byte(raw.String), &snapshots); err != nil {
		return nil, fmt.Errorf("decode vulnerability source snapshot metadata: %w", err)
	}
	return snapshots, nil
}

func decodeSourceStates(raw sql.NullString) ([]SupplyChainImpactSourceState, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var states []SupplyChainImpactSourceState
	if err := json.Unmarshal([]byte(raw.String), &states); err != nil {
		return nil, fmt.Errorf("decode vulnerability source state metadata: %w", err)
	}
	return states, nil
}
