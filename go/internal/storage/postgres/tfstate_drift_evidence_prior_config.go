package postgres

import (
	"context"
	"fmt"
)

// defaultPriorConfigDepth bounds how many prior repo-snapshot generations the
// drift loader walks to detect removed_from_config. Ten is a conservative seed
// — most operators apply many times per day; ten generations cover roughly a
// trading-day window in production. Override per-deployment via
// ESHU_DRIFT_PRIOR_CONFIG_DEPTH.
//
//nolint:unused // wired into LoadDriftEvidence in tfstate_drift_evidence.go (Task 4)
const defaultPriorConfigDepth = 10

// effectivePriorConfigDepth returns the configured depth or the package
// default when the field is zero. The split keeps tests that construct the
// loader inline ergonomic — they specify the depth they want or rely on the
// default behavior with no init step.
//
//nolint:unused // wired into LoadDriftEvidence in tfstate_drift_evidence.go (Task 4)
func (l PostgresDriftEvidenceLoader) effectivePriorConfigDepth() int {
	if l.PriorConfigDepth <= 0 {
		return defaultPriorConfigDepth
	}
	return l.PriorConfigDepth
}

// loadPriorConfigAddresses returns the set of canonical Terraform resource
// addresses declared in any of the most recent N prior repo-snapshot
// generations for `scopeID`, excluding the current generation
// `currentGenerationID`. Powers removed_from_config detection: state-only
// addresses present in this set get PreviouslyDeclaredInConfig=true so the
// classifier emits removed_from_config; operator-imported addresses (never in
// any prior config) stay outside the set and surface as added_in_state.
//
// The walk is bounded by listPriorConfigAddressesQuery's LIMIT. Addresses
// declared more than N generations ago are intentionally invisible — the
// conservative outside-window policy keeps the cost bounded and avoids
// promoting truly-ancient resources whose removal was already actioned.
//
// Returns an empty set when:
//   - the scope has no prior generations matching the status filter, OR
//   - prior generations exist but none declare any resources.
//
// Returns a DB error to the caller; the reducer handler's retry path covers
// transient failures.
//
//nolint:unused // wired into LoadDriftEvidence in tfstate_drift_evidence.go (Task 4)
func (l PostgresDriftEvidenceLoader) loadPriorConfigAddresses(
	ctx context.Context,
	scopeID string,
	currentGenerationID string,
) (map[string]struct{}, error) {
	depth := l.effectivePriorConfigDepth()
	rows, err := l.DB.QueryContext(ctx, listPriorConfigAddressesQuery, scopeID, currentGenerationID, depth)
	if err != nil {
		return nil, fmt.Errorf("list prior config terraform_resources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]struct{}{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan prior config terraform_resources: %w", err)
		}
		entries, err := decodeJSONArray(raw, "prior_terraform_resources")
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			row, ok := configRowFromParserEntry(entry)
			if !ok {
				continue
			}
			out[row.Address] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate prior config terraform_resources: %w", err)
	}
	return out, nil
}
