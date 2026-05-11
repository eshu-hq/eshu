package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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
// Attribute drift is active end-to-end as of PR #167. The HCL parser
// recursively walks resource blocks and emits a flat dot-path attributes
// map plus an unknown_attributes list (parser/hcl/terraform_resource_attributes.go).
// configRowFromParserEntry (tfstate_drift_evidence_config_row.go) decodes
// both into ResourceRow.Attributes and ResourceRow.UnknownAttributes.
// flattenStateAttributes (tfstate_drift_evidence_state_row.go) produces the
// matching dot-path keys from Terraform-state's nested-array-wrapped repeated
// blocks via singleton-array unwrap. The two sides must stay byte-identical
// in their leaf-value encoding — see the coerceJSONString doc comment for the
// per-type contract and the float64 scientific-notation hazard.
//
// removed_from_config detection is also dormant in v1: the loader cannot
// cheaply prove that a state-only address was once declared in a prior
// config snapshot (requires walking prior repo generations for the joined
// repo). Per the design, the safer fallback is to leave
// ResourceRow.PreviouslyDeclaredInConfig false; the classifier then emits
// added_in_state for every state-only address, including genuine
// removed_from_config cases. Operators still see the drift via
// added_in_state; misclassification of operator-imported resources as
// removed_from_config is avoided. A follow-up parser+loader pass will
// activate removed_from_config without changing the classifier.
type PostgresDriftEvidenceLoader struct {
	DB Queryer
	// Tracer wraps LoadDriftEvidence in a single span so operators can
	// answer "is the loader slow because of the config query, the state
	// query, or the prior-state query?" — the InstrumentedDB child spans
	// appear under it. Optional; nil disables span emission.
	Tracer trace.Tracer
	// Logger receives WARN logs when a payload row fails to decode. A
	// decode failure indicates real corruption or a payload schema break
	// upstream; the loader skips the row to keep the join bounded, but the
	// log line is the operator-visible signal. Optional; nil drops logs.
	Logger *slog.Logger
	// PriorConfigDepth bounds the prior-generation walk that activates
	// removed_from_config (added in Task 3 of this plan). Zero means "use
	// the package default". Set from ESHU_DRIFT_PRIOR_CONFIG_DEPTH at
	// construction time (cmd/reducer/main.go). Tests set this field directly.
	PriorConfigDepth int
}

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

	if l.Tracer != nil {
		var span trace.Span
		ctx, span = l.Tracer.Start(ctx, telemetry.SpanReducerDriftEvidenceLoad)
		defer span.End()
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
			l.logDecodeFailure(ctx, scopeID, generationID, address)
			continue
		}
		out[row.Address] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate state terraform_state_resource: %w", err)
	}
	return out, nil
}

// logDecodeFailure surfaces a corrupt state-resource payload as an
// operator-actionable WARN log. A decode failure indicates real corruption
// or a payload schema break in the collector pipeline upstream; the loader
// skips the row to keep the join bounded, but the row is real drift evidence
// that has gone dark. Without the log, the corruption is invisible.
//
// The loader deliberately continues past a decode failure rather than
// propagating it: drift detection is best-effort observability over
// committed facts, not a transactional invariant. One corrupt payload in a
// scope of thousands must not disable drift classification for every other
// address; the structured log gives operators the signal to remediate the
// corrupt fact while the rest of the join still runs. Reducer handlers that
// own transactional truth (e.g. canonical materialization) propagate
// upstream errors instead — drift does not, by design.
func (l PostgresDriftEvidenceLoader) logDecodeFailure(ctx context.Context, scopeID, generationID, address string) {
	if l.Logger == nil {
		return
	}
	l.Logger.LogAttrs(ctx, slog.LevelWarn, "drift evidence loader skipped state resource",
		slog.String(telemetry.LogKeyScopeID, scopeID),
		slog.String(telemetry.LogKeyGenerationID, generationID),
		slog.String("state.address", address),
		slog.String(telemetry.LogKeyFailureClass, "state_resource_payload_decode"),
	)
}

// mergeDriftRows unions the address keyspaces of config, state, and prior and
// emits one AddressedRow per address that appears in any source. Aligned
// addresses (config and state both present with identical Attributes, no
// prior signal) pass through; classify.go returns the empty string for them
// and tfconfigstate.BuildCandidates drops them before they reach the engine.
//
// PreviouslyDeclaredInConfig is intentionally LEFT FALSE in v1. The classifier
// uses that flag to distinguish removed_from_config from added_in_state for
// state-only addresses. Setting it from prior-state existence (the available
// proxy) would misclassify operator-imported resources — which exist in
// state across many generations without ever being declared in config — as
// removed_from_config. The safer fallback is to let the classifier emit
// added_in_state for every state-only address; removed_from_config remains
// dormant until a future loader pass can walk prior-config-snapshot evidence
// to prove the address was once declared. Operators still see the drift,
// just under a more conservative label.
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

// coerceJSONString formats one leaf JSON value into the canonical drift-
// comparison string. flattenStateAttributes owns recursion into nested maps
// and singleton-array-wrapped repeated blocks (see
// tfstate_drift_evidence_state_row.go); coerceJSONString operates on the
// leaves it produces and must stay byte-identical to the parser-side
// ctyValueToDriftString output
// (go/internal/parser/hcl/terraform_resource_attributes.go) so the classifier's
// value-equality check at
// go/internal/correlation/drift/tfconfigstate/classify.go:171 fires
// deterministically across both sides.
//
// Encoding contract per leaf type:
//   - nil          → ""
//   - string       → identity
//   - bool         → "true" / "false" (NEVER "cty.True" or other Go-formatted forms)
//   - default      → fmt.Sprint(value)
//
// The default branch covers numeric leaves decoded from JSON as float64.
// fmt.Sprint(float64) renders integers as their decimal form for values below
// ~1e6 but switches to scientific notation ("1e+06", "1e+07", …) at or above
// 1e6. The parser side encodes integers with strconv.FormatInt, which is
// always decimal. The encodings diverge above the 1e6 threshold.
//
// Safe for all v1 allowlist numeric attributes (aws_lambda_function.memory_size
// up to 10240, aws_lambda_function.timeout up to 900, aws_db_instance numeric
// versions). Any new numeric allowlist entry whose real-world values could
// reach 1e6 (e.g. aws_cloudwatch_log_group.retention_in_days at multi-year
// retention, large disk sizes in bytes) needs a regression test AND a
// coordinated change here — switching the default branch to a numeric-aware
// formatter that produces decimal output.
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
