package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// PostgresDriftEvidenceLoader builds the per-address join the drift handler
// classifier consumes. It pulls three logical inputs from durable facts:
//
//  1. Config-side parsed-HCL terraform_resources from the resolved
//     anchor.ScopeID + anchor.CommitID.
//  2. Active terraform_state_resource rows for the state-snapshot scope.
//  3. Prior-generation terraform_state_resource rows when the current
//     snapshot has serial > 0; the prior generation enables
//     removed_from_state classification.
//
// The loader emits one AddressedRow per address present in any of the three
// inputs. Per the AddressedRow contract the loader MAY omit aligned
// addresses, but for v1 it emits the union; the classifier already filters
// non-drifted candidates. Trading the union for a pre-filter would duplicate
// classify.go's dispatch order — not worth the bug surface for v1.
//
// Attribute drift requires both sides to carry per-attribute values.
// The state collector emits attributes (resources.go:173-181); the HCL parser
// does NOT emit attributes on terraform_resources rows today
// (parser.go:130-154). Until the parser is enhanced, attribute_drift cannot
// fire in production from this loader — the dispatcher returns "" because the
// config-side Attributes map is always empty. See the package AGENTS.md for
// the follow-up tracking item.
type PostgresDriftEvidenceLoader struct {
	DB Queryer
}

// listConfigResourcesForCommitQuery returns the terraform_resources arrays
// emitted by the parser for one (scope_id, generation_id) — i.e. one sealed
// commit anchor — restricted to git-source file facts that actually carry the
// bucket. The jsonb_typeof predicate keeps the query bounded to .tf-like
// inputs even though the parser emits empty arrays in its base payload.
const listConfigResourcesForCommitQuery = `
SELECT
    fact.payload->'parsed_file_data'->'terraform_resources' AS terraform_resources
FROM fact_records AS fact
WHERE fact.scope_id = $1
  AND fact.generation_id = $2
  AND fact.fact_kind = 'file'
  AND fact.source_system = 'git'
  AND jsonb_typeof(fact.payload->'parsed_file_data'->'terraform_resources') = 'array'
ORDER BY fact.fact_id ASC
`

// activeStateSnapshotMetadataQuery returns the lineage and serial of the
// active terraform_state_snapshot fact for one state-snapshot scope, plus the
// generation_id (used to fetch the matching state-resource rows). The scope
// must have at most one active snapshot at any time; the LIMIT 1 protects
// against stray duplicates without hiding a real bug.
const activeStateSnapshotMetadataQuery = `
SELECT
    fact.payload->>'lineage'                AS lineage,
    (fact.payload->>'serial')::bigint        AS serial,
    fact.generation_id                       AS generation_id
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
WHERE fact.scope_id = $1
  AND fact.fact_kind = 'terraform_state_snapshot'
LIMIT 1
`

// listStateResourcesForGenerationQuery returns the terraform_state_resource
// rows for one (scope_id, generation_id) pair. Used twice per call when a
// prior generation exists: once for the active generation and once for
// serial-1. Returns (address, payload_json) so the loader can decode
// attributes without joining additional fact records.
const listStateResourcesForGenerationQuery = `
SELECT
    fact.payload->>'address' AS address,
    fact.payload            AS payload
FROM fact_records AS fact
WHERE fact.scope_id = $1
  AND fact.generation_id = $2
  AND fact.fact_kind = 'terraform_state_resource'
ORDER BY fact.payload->>'address' ASC, fact.fact_id ASC
`

// priorStateSnapshotMetadataQuery returns the prior generation for one
// state-snapshot scope by matching serial = currentSerial - 1. The lineage
// is returned in addition to the generation_id so the loader can flag
// lineage rotations (different lineage than the current snapshot) and
// suppress removed_from_state per classify.go:73.
const priorStateSnapshotMetadataQuery = `
SELECT
    fact.payload->>'lineage'                AS lineage,
    (fact.payload->>'serial')::bigint        AS serial,
    fact.generation_id                       AS generation_id
FROM fact_records AS fact
WHERE fact.scope_id = $1
  AND fact.fact_kind = 'terraform_state_snapshot'
  AND (fact.payload->>'serial')::bigint = $2
ORDER BY fact.observed_at DESC, fact.fact_id DESC
LIMIT 1
`

// LoadDriftEvidence implements reducer.DriftEvidenceLoader. The method
// returns one AddressedRow per address present in any of the three inputs;
// aligned addresses pass through and are filtered out downstream by the
// classifier rather than re-doing the classifier's dispatch order here.
//
// Failure modes:
//
//   - Missing terraform_state_snapshot fact (e.g. partial bootstrap) returns
//     ([], nil); the reducer handler logs the operator-actionable rejection.
//   - Active snapshot with serial == 0 short-circuits prior-generation
//     lookups. removed_from_state cannot fire on the first apply by
//     definition; saving the round-trip is correct, not an optimization.
//   - A prior snapshot whose lineage differs from the current generation's
//     marks every prior ResourceRow with LineageRotation = true so the
//     classifier suppresses removed_from_state (rotated state file, not a
//     real removal).
func (l PostgresDriftEvidenceLoader) LoadDriftEvidence(
	ctx context.Context,
	stateScopeID string,
	anchor tfstatebackend.CommitAnchor,
) ([]tfconfigstate.AddressedRow, error) {
	if l.DB == nil {
		return nil, fmt.Errorf("drift evidence database is required")
	}
	stateScopeID = strings.TrimSpace(stateScopeID)
	if stateScopeID == "" {
		return nil, fmt.Errorf("state scope ID must not be blank")
	}
	configScopeID := strings.TrimSpace(anchor.ScopeID)
	configGenerationID := strings.TrimSpace(anchor.CommitID)
	if configScopeID == "" || configGenerationID == "" {
		return nil, fmt.Errorf("commit anchor must carry scope and commit id")
	}

	configByAddress, err := l.loadConfigByAddress(ctx, configScopeID, configGenerationID)
	if err != nil {
		return nil, err
	}

	currentSnapshot, ok, err := l.loadActiveStateSnapshot(ctx, stateScopeID)
	if err != nil {
		return nil, err
	}
	if !ok {
		// No active state-snapshot fact yet; the join cannot run.
		return nil, nil
	}

	stateByAddress, err := l.loadStateResources(ctx, stateScopeID, currentSnapshot.generationID, false)
	if err != nil {
		return nil, err
	}

	var priorByAddress map[string]*tfconfigstate.ResourceRow
	if currentSnapshot.serial > 0 {
		priorSnapshot, priorOK, err := l.loadPriorStateSnapshot(ctx, stateScopeID, currentSnapshot.serial-1)
		if err != nil {
			return nil, err
		}
		if priorOK {
			lineageRotation := priorSnapshot.lineage != currentSnapshot.lineage
			priorByAddress, err = l.loadStateResources(ctx, stateScopeID, priorSnapshot.generationID, lineageRotation)
			if err != nil {
				return nil, err
			}
		}
	}

	return mergeDriftRows(configByAddress, stateByAddress, priorByAddress), nil
}

// snapshotMetadata captures the lineage/serial/generation_id of one
// terraform_state_snapshot fact. The loader uses two: one for the active
// generation, optionally one for the prior generation.
type snapshotMetadata struct {
	lineage      string
	serial       int64
	generationID string
}

func (l PostgresDriftEvidenceLoader) loadConfigByAddress(
	ctx context.Context,
	scopeID string,
	generationID string,
) (map[string]*tfconfigstate.ResourceRow, error) {
	rows, err := l.DB.QueryContext(ctx, listConfigResourcesForCommitQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("list config terraform_resources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]*tfconfigstate.ResourceRow{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan config terraform_resources: %w", err)
		}
		entries, err := decodeJSONArray(raw, "terraform_resources")
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			row, ok := configRowFromParserEntry(entry)
			if !ok {
				continue
			}
			if _, exists := out[row.Address]; exists {
				continue
			}
			out[row.Address] = row
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate config terraform_resources: %w", err)
	}
	return out, nil
}

func (l PostgresDriftEvidenceLoader) loadActiveStateSnapshot(
	ctx context.Context,
	scopeID string,
) (snapshotMetadata, bool, error) {
	rows, err := l.DB.QueryContext(ctx, activeStateSnapshotMetadataQuery, scopeID)
	if err != nil {
		return snapshotMetadata{}, false, fmt.Errorf("read active state snapshot: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return snapshotMetadata{}, false, nil
	}
	var meta snapshotMetadata
	if err := rows.Scan(&meta.lineage, &meta.serial, &meta.generationID); err != nil {
		return snapshotMetadata{}, false, fmt.Errorf("scan active state snapshot: %w", err)
	}
	if err := rows.Err(); err != nil {
		return snapshotMetadata{}, false, fmt.Errorf("iterate active state snapshot: %w", err)
	}
	return meta, true, nil
}

func (l PostgresDriftEvidenceLoader) loadPriorStateSnapshot(
	ctx context.Context,
	scopeID string,
	priorSerial int64,
) (snapshotMetadata, bool, error) {
	rows, err := l.DB.QueryContext(ctx, priorStateSnapshotMetadataQuery, scopeID, priorSerial)
	if err != nil {
		return snapshotMetadata{}, false, fmt.Errorf("read prior state snapshot: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return snapshotMetadata{}, false, nil
	}
	var meta snapshotMetadata
	if err := rows.Scan(&meta.lineage, &meta.serial, &meta.generationID); err != nil {
		return snapshotMetadata{}, false, fmt.Errorf("scan prior state snapshot: %w", err)
	}
	if err := rows.Err(); err != nil {
		return snapshotMetadata{}, false, fmt.Errorf("iterate prior state snapshot: %w", err)
	}
	return meta, true, nil
}

func (l PostgresDriftEvidenceLoader) loadStateResources(
	ctx context.Context,
	scopeID string,
	generationID string,
	lineageRotation bool,
) (map[string]*tfconfigstate.ResourceRow, error) {
	rows, err := l.DB.QueryContext(ctx, listStateResourcesForGenerationQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("list state terraform_state_resource: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]*tfconfigstate.ResourceRow{}
	for rows.Next() {
		var address string
		var payload []byte
		if err := rows.Scan(&address, &payload); err != nil {
			return nil, fmt.Errorf("scan state terraform_state_resource: %w", err)
		}
		row, ok := stateRowFromCollectorPayload(address, payload, lineageRotation)
		if !ok {
			continue
		}
		out[row.Address] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate state terraform_state_resource: %w", err)
	}
	return out, nil
}

// configRowFromParserEntry maps one parsed_file_data.terraform_resources entry
// to a ResourceRow. The parser emits resource_type + resource_name plus an
// optional module path; the canonical address is rebuilt here so the join key
// matches the state-side address shape. Module-nested resources are skipped:
// the parser sees the calling repo's module call, not the callee's resources,
// so the join would never match anyway.
func configRowFromParserEntry(entry map[string]any) (*tfconfigstate.ResourceRow, bool) {
	resourceType := strings.TrimSpace(coerceJSONString(entry["resource_type"]))
	resourceName := strings.TrimSpace(coerceJSONString(entry["resource_name"]))
	if resourceType == "" || resourceName == "" {
		return nil, false
	}
	address := resourceType + "." + resourceName
	// The parser does not emit per-attribute values today; reserve the
	// Attributes map for forward-compat when an attribute extractor lands.
	return &tfconfigstate.ResourceRow{
		Address:      address,
		ResourceType: resourceType,
	}, true
}

// stateRowFromCollectorPayload decodes one terraform_state_resource fact
// payload into a ResourceRow. The collector emits classified attributes as a
// map[string]any (resources.go:173-181); we flatten the top-level keys into a
// map[string]string so the classifier's attribute-drift dispatch can compare
// them against allowlisted attribute paths. Nested structure is intentionally
// dropped — the allowlist is path-keyed, but until the config side carries
// attributes there is no value to compare against, so the lossy flattening
// does not change runtime behavior.
func stateRowFromCollectorPayload(address string, payload []byte, lineageRotation bool) (*tfconfigstate.ResourceRow, bool) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, false
	}
	var decoded struct {
		Address      string                 `json:"address"`
		Type         string                 `json:"type"`
		Attributes   map[string]any         `json:"attributes"`
		ExtraIgnored map[string]any         `json:"-"`
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return nil, false
		}
	}
	row := &tfconfigstate.ResourceRow{
		Address:         address,
		ResourceType:    strings.TrimSpace(decoded.Type),
		LineageRotation: lineageRotation,
	}
	if len(decoded.Attributes) > 0 {
		flat := make(map[string]string, len(decoded.Attributes))
		for key, value := range decoded.Attributes {
			flat[key] = coerceJSONString(value)
		}
		row.Attributes = flat
	}
	return row, true
}

// mergeDriftRows unions the address keyspaces of config, state, and prior and
// emits one AddressedRow per address that appears in any source. Aligned
// addresses (config and state both present with identical Attributes, no
// prior signal) pass through; classify.go returns the empty string for them
// and tfconfigstate.BuildCandidates drops them before they reach the engine.
//
// PreviouslyDeclaredInConfig is the conservative v1 signal documented in the
// plan: set true on a state-only address when the prior generation also has
// the address. The classifier uses this to distinguish removed_from_config
// from added_in_state. When the proxy is wrong (an operator-imported
// resource that also existed in prior state without ever being declared) the
// classifier falls back to added_in_state — still operator-actionable.
func mergeDriftRows(
	config, state, prior map[string]*tfconfigstate.ResourceRow,
) []tfconfigstate.AddressedRow {
	addresses := map[string]struct{}{}
	for address := range config {
		addresses[address] = struct{}{}
	}
	for address := range state {
		addresses[address] = struct{}{}
	}
	for address := range prior {
		addresses[address] = struct{}{}
	}
	if len(addresses) == 0 {
		return nil
	}
	out := make([]tfconfigstate.AddressedRow, 0, len(addresses))
	for address := range addresses {
		cfg := config[address]
		st := state[address]
		pr := prior[address]
		if st != nil && cfg == nil && pr != nil {
			st.PreviouslyDeclaredInConfig = true
		}
		resourceType := ""
		switch {
		case cfg != nil:
			resourceType = cfg.ResourceType
		case st != nil:
			resourceType = st.ResourceType
		case pr != nil:
			resourceType = pr.ResourceType
		}
		out = append(out, tfconfigstate.AddressedRow{
			Address:      address,
			ResourceType: resourceType,
			Config:       cfg,
			State:        st,
			Prior:        pr,
		})
	}
	return out
}

// decodeJSONArray decodes a Postgres JSON array column into a slice of
// generic maps. The label is included in the error string so callers can tell
// which logical bucket failed (terraform_resources vs others) without
// inspecting the raw bytes.
func decodeJSONArray(raw []byte, label string) ([]map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("decode %s array: %w", label, err)
	}
	return entries, nil
}

// coerceJSONString coerces a JSON value into a flat string. Numeric, bool,
// and null values produce their fmt.Sprint form so the classifier's
// attribute-drift comparison stays type-stable across config and state
// sources. Nested structures collapse to fmt.Sprint output, which is lossy
// for nested attribute drift; this matches the v1 contract (the attribute
// allowlist is path-keyed and operates on top-level keys only).
func coerceJSONString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(value)
	}
}
