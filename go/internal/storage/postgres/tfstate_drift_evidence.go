package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// PostgresDriftEvidenceLoader builds the per-address join the drift handler
// classifier consumes. It pulls four logical inputs from durable facts:
//
//  1. Config-side parsed-HCL terraform_resources from the resolved
//     anchor.ScopeID + anchor.CommitID.
//  2. Active terraform_state_resource rows for the state-snapshot scope.
//  3. Prior-generation terraform_state_resource rows when the current
//     snapshot has serial > 0; the prior generation enables
//     removed_from_state classification.
//  4. Prior-config-snapshot addresses (the union of resource addresses
//     declared in the most recent PriorConfigDepth prior repo-snapshot
//     generations) that activates removed_from_config classification.
//
// The loader emits one AddressedRow per address present in any of the four
// inputs. Per the AddressedRow contract the loader MAY omit aligned
// addresses, but for now it emits the union; the classifier already filters
// non-drifted candidates. Trading the union for a pre-filter would duplicate
// classify.go's dispatch order — not worth the bug surface.
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
// removed_from_config is active as of issue #168. loadPriorConfigAddresses
// (tfstate_drift_evidence_prior_config.go) walks the most recent
// PriorConfigDepth (default 10, configurable via
// ESHU_DRIFT_PRIOR_CONFIG_DEPTH) repo-snapshot generations and unions every
// declared address. mergeDriftRows promotes state-only addresses present in
// that set to PreviouslyDeclaredInConfig=true; the classifier emits
// removed_from_config for them. Operator-imported addresses (never declared
// in any prior generation within the depth window) stay outside the set and
// surface as added_in_state — the conservative outside-window fallback.
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
	// removed_from_config (see tfstate_drift_evidence_prior_config.go).
	// Non-positive values (zero or negative) mean "use the package default"
	// (defaultPriorConfigDepth = 10). Set from ESHU_DRIFT_PRIOR_CONFIG_DEPTH
	// at construction time (cmd/reducer/main.go); parsePriorConfigDepth maps
	// invalid env input to 0, which the loader then resolves to the default.
	// Tests set this field directly.
	PriorConfigDepth int
	// Instruments threads the data-plane telemetry contract through the
	// loader so module-aware joining (issue #169) can report unresolved
	// module calls on eshu_dp_drift_unresolved_module_calls_total. Optional;
	// nil routes recording through a no-op recorder so tests and early
	// bootstrap paths (no telemetry wired) remain operable.
	Instruments *telemetry.Instruments
}

// LoadDriftEvidence implements reducer.DriftEvidenceLoader. The method
// returns one AddressedRow per address present in any of the four inputs;
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

	recorder := l.unresolvedRecorder()
	prefixMap, err := l.buildModulePrefixMap(ctx, configScopeID, configGenerationID, recorder)
	if err != nil {
		return nil, err
	}

	configByAddress, err := l.loadConfigByAddress(ctx, configScopeID, configGenerationID, prefixMap)
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

	var priorConfigAddresses map[string]struct{}
	if hasStateOnlyAddress(configByAddress, stateByAddress) {
		priorConfigAddresses, err = l.loadPriorConfigAddresses(ctx, configScopeID, configGenerationID, prefixMap)
		if err != nil {
			return nil, err
		}
	}

	merged := mergeDriftRows(configByAddress, stateByAddress, priorByAddress, priorConfigAddresses)
	l.logPriorConfigWalk(ctx, configScopeID, configGenerationID, priorConfigAddresses, merged)
	return merged, nil
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
	prefixMap modulePrefixMap,
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
			emitConfigRowsForEntry(entry, prefixMap, out)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate config terraform_resources: %w", err)
	}
	return out, nil
}

// emitConfigRowsForEntry projects one parser-emitted terraform_resources entry
// into one or more ResourceRow values keyed by canonical address. The entry's
// `path` field is matched against the module-prefix map (longest-prefix
// wins). Outcomes:
//
//   - Zero matching prefixes: emit one row with the root-module address
//     `<type>.<name>` exactly as v0 behavior.
//   - One matching prefix: emit one row with the prefixed address
//     `module.<name>...<type>.<name>`.
//   - N matching prefixes (1→N projection — same callee directory referenced
//     by N distinct `module {}` blocks): emit N rows, one per prefix. This is
//     the load-bearing fan-out the ADR commits to and that
//     TestLoadConfigByAddressExpandsSameCalleeForMultipleCallers proves.
//
// The fan-out lives here, in the loader emission loop, deliberately NOT in
// configRowFromParserEntry — that helper stays strictly 1:1 so future
// readers cannot mistake it for the projection seam (binding constraint D).
//
// First-write-wins on address conflicts mirrors v0 behavior — a duplicate
// address in the same generation is a parser bug, not a join-shape signal.
func emitConfigRowsForEntry(
	entry map[string]any,
	prefixMap modulePrefixMap,
	out map[string]*tfconfigstate.ResourceRow,
) {
	entryPath := strings.TrimSpace(coerceJSONString(entry["path"]))
	prefixes := prefixMap.modulePrefixForPath(entryPath)
	if len(prefixes) == 0 {
		row, ok := configRowFromParserEntry(entry, "")
		if !ok {
			return
		}
		if _, exists := out[row.Address]; !exists {
			out[row.Address] = row
		}
		return
	}
	for _, prefix := range prefixes {
		row, ok := configRowFromParserEntry(entry, prefix)
		if !ok {
			return
		}
		if _, exists := out[row.Address]; !exists {
			out[row.Address] = row
		}
	}
}

// unresolvedRecorder returns the recorder the loader uses for module-call
// fallbacks. When Instruments is wired, the recorder both increments
// DriftUnresolvedModuleCalls and emits a structured log per call so
// operators have a per-intent signal outside metrics. When Instruments is
// nil (early bootstrap, fixtures without telemetry), the recorder is a
// no-op so the loader stays operable.
func (l PostgresDriftEvidenceLoader) unresolvedRecorder() unresolvedRecorder {
	if l.Instruments == nil || l.Instruments.DriftUnresolvedModuleCalls == nil {
		return nopUnresolvedRecorder{}
	}
	return loggingUnresolvedRecorder{
		counter: l.Instruments.DriftUnresolvedModuleCalls,
		logger:  l.Logger,
	}
}

// loggingUnresolvedRecorder forwards every record call to the OTEL counter
// and (optionally) to the structured logger. Wraps the counter in a typed
// adapter so test paths can substitute a stub recorder without faking the
// OTEL surface.
type loggingUnresolvedRecorder struct {
	counter metric.Int64Counter
	logger  *slog.Logger
}

func (r loggingUnresolvedRecorder) record(ctx context.Context, reason string) {
	r.counter.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionDriftUnresolvedModuleReason, reason),
	))
	if r.logger != nil {
		r.logger.LogAttrs(ctx, slog.LevelInfo, "drift evidence loader skipped unresolvable module call",
			slog.String(telemetry.LogKeyFailureClass, reason),
		)
	}
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
		row, ok := stateRowFromCollectorPayload(ctx, l.Logger, address, payload, lineageRotation)
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

// logPriorConfigWalk emits one INFO log per drift intent summarizing the
// prior-config walk that powers removed_from_config detection. Cardinality is
// per-intent (low); address-level detail stays out of metric labels per
// CLAUDE.md observability rules. Optional; nil logger drops the line.
func (l PostgresDriftEvidenceLoader) logPriorConfigWalk(
	ctx context.Context,
	scopeID string,
	generationID string,
	priorConfig map[string]struct{},
	merged []tfconfigstate.AddressedRow,
) {
	if l.Logger == nil {
		return
	}
	stateOnly := 0
	promoted := 0
	for _, row := range merged {
		if row.Config == nil && row.State != nil {
			stateOnly++
			if row.State.PreviouslyDeclaredInConfig {
				promoted++
			}
		}
	}
	l.Logger.LogAttrs(ctx, slog.LevelInfo, "drift evidence loader walked prior config generations",
		slog.String(telemetry.LogKeyScopeID, scopeID),
		slog.String(telemetry.LogKeyGenerationID, generationID),
		slog.Int(telemetry.LogKeyDriftPriorConfigDepth, l.effectivePriorConfigDepth()),
		slog.Int(telemetry.LogKeyDriftPriorConfigAddresses, len(priorConfig)),
		slog.Int(telemetry.LogKeyDriftStateOnlyAddresses, stateOnly),
		slog.Int(telemetry.LogKeyDriftAddressesPromoted, promoted),
	)
}

// hasStateOnlyAddress, mergeDriftRows, decodeJSONArray, and coerceJSONString
// were hoisted to tfstate_drift_evidence_helpers.go (issue #169) to keep this
// loader file under the CLAUDE.md 500-line cap after the module-aware-join
// extension.
