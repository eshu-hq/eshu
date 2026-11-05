// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfstate

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Thin exports for the parent-package delta-overlay entry points
// (drift_overlay_tfstate.go, issue #10006). The parent package imports
// this one, so the overlay orchestration lives there; these wrappers
// expose the static query texts above and the pure decode/build pieces
// without duplicating any rule. Wrappers that take the unexported
// modulePrefixMap, moduleResolutionConfidenceMap, or unresolvedRecorder
// types follow the UnresolvedRecorder precedent: callers pass values
// obtained opaquely from other wrappers without naming their types.

// TerraformBackendFactContext is the decoded backend/variable/local
// context of one repo generation's file facts. See
// terraformBackendFactContext.
type TerraformBackendFactContext = terraformBackendFactContext

// DecodeTerraformBackendFactContext decodes one backend-context payload
// (the object shape the overlay queries select, or the bare array shape
// the row scanner tolerates). See decodeTerraformBackendFactContext.
func DecodeTerraformBackendFactContext(rawContext []byte) (TerraformBackendFactContext, error) {
	return decodeTerraformBackendFactContext(rawContext)
}

// MergeTerraformBackendFactContext appends src's entries onto dst. See
// mergeTerraformBackendFactContext.
func MergeTerraformBackendFactContext(dst, src TerraformBackendFactContext) TerraformBackendFactContext {
	return mergeTerraformBackendFactContext(dst, src)
}

// TerraformBackendCandidatesFromContext derives discovery candidates for
// one repo generation. See terraformBackendCandidatesFromContext.
func TerraformBackendCandidatesFromContext(
	repoID string,
	repoLocalPath string,
	contextValue TerraformBackendFactContext,
) []terraformstate.DiscoveryCandidate {
	return terraformBackendCandidatesFromContext(repoID, repoLocalPath, contextValue)
}

// MatchingBackendRowsFromCandidates converts one repo-generation context
// into canonical backend rows for the composite key. See
// matchingBackendRowsFromCandidates.
func MatchingBackendRowsFromCandidates(
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	candidates []terraformstate.DiscoveryCandidate,
	backendKind string,
	locatorHash string,
) []tfstatebackend.TerraformBackendRow {
	return matchingBackendRowsFromCandidates(repoID, scopeID, generationID, observedAt, candidates, backendKind, locatorHash)
}

// TerraformBackendReadCanonical names the canonical ownership read label
// for eshu_dp_terraform_backend_canonical_read_duration_seconds. See
// terraformBackendReadCanonical.
const TerraformBackendReadCanonical = terraformBackendReadCanonical

// RecordBackendRead records one sample of the backend ownership read
// duration sensor. See recordBackendRead.
func RecordBackendRead(
	ctx context.Context,
	inst *telemetry.Instruments,
	read string,
	started time.Time,
	failed bool,
) {
	recordBackendRead(ctx, inst, read, started, failed)
}

// DecodeConfigResourceEntries decodes one terraform_resources payload
// array. See decodeJSONArray.
func DecodeConfigResourceEntries(raw []byte) ([]map[string]any, error) {
	return decodeJSONArray(raw, "terraform_resources")
}

// DecodeModuleCallEntries decodes one terraform_modules payload array.
// See decodeJSONArray.
func DecodeModuleCallEntries(raw []byte) ([]map[string]any, error) {
	return decodeJSONArray(raw, "terraform_modules")
}

// EmitConfigRowsForEntry projects one parser-emitted terraform_resources
// entry into the address map. See emitConfigRowsForEntry.
func EmitConfigRowsForEntry(
	entry map[string]any,
	prefixMap modulePrefixMap,
	confidenceMap moduleResolutionConfidenceMap,
	out map[string]*tfconfigstate.ResourceRow,
) {
	emitConfigRowsForEntry(entry, prefixMap, confidenceMap, out)
}

// ModulePrefixMapFromPayloads builds the module-prefix and confidence
// maps from already-merged module payloads. See
// buildModulePrefixMapFromEntries.
func ModulePrefixMapFromPayloads(
	ctx context.Context,
	payloads [][]byte,
	recorder unresolvedRecorder,
) (modulePrefixMap, moduleResolutionConfidenceMap, error) {
	var entries []moduleCallEntry
	for _, raw := range payloads {
		decoded, err := decodeJSONArray(raw, "terraform_modules")
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, moduleCallEntriesFromDecoded(decoded)...)
	}
	return buildModulePrefixMapFromEntries(ctx, entries, recorder)
}

// OverlaidConfig carries a delta-overlaid current-config read back into
// the drift join: the decoded address map plus the overlaid
// module-prefix maps the prior-config rename detection needs. Built by
// NewOverlaidConfig; fields stay unexported because only this package
// reads them.
type OverlaidConfig struct {
	byAddress     map[string]*tfconfigstate.ResourceRow
	prefixMap     modulePrefixMap
	confidenceMap moduleResolutionConfidenceMap
}

// ConfigByAddress exposes the overlay-merged config rows by address for
// consumers that join config without the tfstate loader's drift pipeline
// (AWS/multicloud runtime drift). The map is the seam's own storage;
// callers must not mutate it.
func (c OverlaidConfig) ConfigByAddress() map[string]*tfconfigstate.ResourceRow {
	return c.byAddress
}

// NewOverlaidConfig bundles overlay-merged config with its prefix maps.
// Callers pass prefix values obtained opaquely from
// ModulePrefixMapFromPayloads without naming their types.
func NewOverlaidConfig(
	byAddress map[string]*tfconfigstate.ResourceRow,
	prefixMap modulePrefixMap,
	confidenceMap moduleResolutionConfidenceMap,
) OverlaidConfig {
	return OverlaidConfig{byAddress: byAddress, prefixMap: prefixMap, confidenceMap: confidenceMap}
}

// ActiveStateSnapshot returns the active snapshot's lineage, serial, and
// generation id. See loadActiveStateSnapshot.
func (l PostgresDriftEvidenceLoader) ActiveStateSnapshot(
	ctx context.Context,
	scopeID string,
) (lineage string, serial int64, generationID string, ok bool, err error) {
	meta, ok, err := l.loadActiveStateSnapshot(ctx, scopeID)
	if err != nil {
		return "", 0, "", false, fmt.Errorf("read active state snapshot: %w", err)
	}
	if !ok {
		return "", 0, "", false, nil
	}
	return meta.lineage, meta.serial, meta.generationID, true, nil
}

// PriorStateSnapshot returns the serial-1 snapshot. See
// loadPriorStateSnapshot.
func (l PostgresDriftEvidenceLoader) PriorStateSnapshot(
	ctx context.Context,
	scopeID string,
	priorSerial int64,
) (lineage string, serial int64, generationID string, ok bool, err error) {
	meta, ok, err := l.loadPriorStateSnapshot(ctx, scopeID, priorSerial)
	if err != nil {
		return "", 0, "", false, fmt.Errorf("read prior state snapshot: %w", err)
	}
	if !ok {
		return "", 0, "", false, nil
	}
	return meta.lineage, meta.serial, meta.generationID, true, nil
}

// StateResources returns the state resource rows for one generation. See
// loadStateResources.
func (l PostgresDriftEvidenceLoader) StateResources(
	ctx context.Context,
	scopeID string,
	generationID string,
	lineageRotation bool,
) (map[string]*tfconfigstate.ResourceRow, error) {
	return l.loadStateResources(ctx, scopeID, generationID, lineageRotation)
}

// PriorConfigAddresses returns the prior-generation address set. See
// loadPriorConfigAddresses.
func (l PostgresDriftEvidenceLoader) PriorConfigAddresses(
	ctx context.Context,
	scopeID string,
	currentGenerationID string,
	currentPrefixMap modulePrefixMap,
	recorder unresolvedRecorder,
) (map[string]string, error) {
	return l.loadPriorConfigAddresses(ctx, scopeID, currentGenerationID, currentPrefixMap, recorder)
}

// MergeDriftRows unions the config, state, prior, and prior-config
// address spaces. See mergeDriftRows.
func MergeDriftRows(
	config, state, prior map[string]*tfconfigstate.ResourceRow,
	priorConfigAddresses map[string]string,
) []tfconfigstate.AddressedRow {
	return mergeDriftRows(config, state, prior, priorConfigAddresses)
}

// HasStateOnlyAddress reports whether any state address lacks a config
// row. See hasStateOnlyAddress.
func HasStateOnlyAddress(
	config, state map[string]*tfconfigstate.ResourceRow,
) bool {
	return hasStateOnlyAddress(config, state)
}

// LogPriorConfigWalk emits the per-intent prior-config walk summary. See
// logPriorConfigWalk.
func (l PostgresDriftEvidenceLoader) LogPriorConfigWalk(
	ctx context.Context,
	scopeID string,
	generationID string,
	priorConfig map[string]string,
	merged []tfconfigstate.AddressedRow,
) {
	l.logPriorConfigWalk(ctx, scopeID, generationID, priorConfig, merged)
}

// TerragruntRemoteStateCandidates queries parsed Terragrunt remote_state
// rows. See terragruntRemoteStateCandidates.
func (r TerraformStateBackendFactReader) TerragruntRemoteStateCandidates(
	ctx context.Context,
	repoIDs []string,
	seen map[string]struct{},
	filters []terraformstate.DiscoveryBackendFilter,
) ([]terraformstate.DiscoveryCandidate, error) {
	return r.terragruntRemoteStateCandidates(ctx, repoIDs, seen, filters)
}

// LocalStateCandidates queries approved local-state candidates. See
// localStateCandidates.
func (r TerraformStateBackendFactReader) LocalStateCandidates(
	ctx context.Context,
	query terraformstate.DiscoveryQuery,
	seen map[string]struct{},
) ([]terraformstate.DiscoveryCandidate, error) {
	return r.localStateCandidates(ctx, query, seen)
}

// FilteredTerraformStateCandidates queries filter-matched backend facts.
// See filteredTerraformStateCandidates.
func (r TerraformStateBackendFactReader) FilteredTerraformStateCandidates(
	ctx context.Context,
	filters []terraformstate.DiscoveryBackendFilter,
	seen map[string]struct{},
) ([]terraformstate.DiscoveryCandidate, error) {
	return r.filteredTerraformStateCandidates(ctx, filters, seen)
}

// CleanTerraformStateBackendFilters normalizes backend filters. See
// cleanTerraformStateBackendFilters.
func CleanTerraformStateBackendFilters(
	filters []terraformstate.DiscoveryBackendFilter,
) []terraformstate.DiscoveryBackendFilter {
	return cleanTerraformStateBackendFilters(filters)
}

// AppendTerraformStateCandidatesWithFilterEnrichment appends unseen
// candidates with filter enrichment. See
// appendTerraformStateCandidatesWithFilterEnrichment.
func AppendTerraformStateCandidatesWithFilterEnrichment(
	out []terraformstate.DiscoveryCandidate,
	seen map[string]struct{},
	candidates []terraformstate.DiscoveryCandidate,
	filters []terraformstate.DiscoveryBackendFilter,
) []terraformstate.DiscoveryCandidate {
	return appendTerraformStateCandidatesWithFilterEnrichment(out, seen, candidates, filters)
}
