// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/filters"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/tfstate"
)

// scanDriftPayloadRow scans the eight overlay payload columns (config
// resources and module calls texts) in order. The is_tombstone column
// is consumed to keep the scan positional; the texts filter tombstones.
func scanDriftPayloadRow(rows db.Rows) (driftOverlayRow, error) {
	var row driftOverlayRow
	var tombstone bool
	if err := rows.Scan(
		&row.factID, &row.scopeID, &row.generationID, &row.stableKey,
		&tombstone, &row.relativePath, &row.payload, &row.observedAt,
	); err != nil {
		return driftOverlayRow{}, err
	}
	return row, nil
}

// scanDriftFactsRow scans ActiveTerraformBackendFactsQuery's nine
// columns in order.
func scanDriftFactsRow(rows db.Rows) (driftOverlayRow, error) {
	var row driftOverlayRow
	var tombstone bool
	if err := rows.Scan(
		&row.repoID, &row.contextPayload,
		&row.factID, &row.scopeID, &row.generationID, &row.stableKey,
		&tombstone, &row.relativePath, &row.observedAt,
	); err != nil {
		return driftOverlayRow{}, err
	}
	return row, nil
}

// TerraformStateCandidates answers terraformstate.BackendFactReader
// over the delta overlay: the backend-facts leg merges base-full files
// back in, while the terragrunt, local-candidate, and filtered legs run
// unchanged on the store handle. It satisfies the discovery query port;
// wire the overlay value itself wherever the active-only
// tfstate.TerraformStateBackendFactReader is constructed today.
func (o TerraformDriftOverlay) TerraformStateCandidates(
	ctx context.Context,
	query terraformstate.DiscoveryQuery,
) ([]terraformstate.DiscoveryCandidate, error) {
	if o.Store == nil {
		return nil, fmt.Errorf("terraform drift overlay store is required")
	}
	repoIDs := filters.CleanFactKinds(query.RepoIDs)
	backendFilters := tfstate.CleanTerraformStateBackendFilters(query.BackendFilters)
	if len(repoIDs) == 0 && len(backendFilters) == 0 {
		return nil, nil
	}
	legs := tfstate.TerraformStateBackendFactReader{DB: o.Store.database, Instruments: o.Store.Instruments()}
	if len(repoIDs) == 0 {
		return legs.FilteredTerraformStateCandidates(ctx, backendFilters, nil)
	}
	sink := o.sink(terraformDriftReadBackendFacts)
	rows, _, _, err := o.overlaidRows(ctx, sink, tfstate.ActiveTerraformBackendFactsQuery, []any{repoIDs}, scanDriftFactsRow)
	if err != nil {
		return nil, err
	}
	rowCandidates, err := driftFactsCandidates(rows)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	candidates := tfstate.AppendTerraformStateCandidatesWithFilterEnrichment(nil, seen, rowCandidates, backendFilters)
	terragruntCandidates, err := legs.TerragruntRemoteStateCandidates(ctx, repoIDs, seen, backendFilters)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, terragruntCandidates...)
	localCandidates, err := legs.LocalStateCandidates(ctx, query, seen)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, localCandidates...)
	filteredCandidates, err := legs.FilteredTerraformStateCandidates(ctx, backendFilters, seen)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, filteredCandidates...)
	return candidates, nil
}

// driftFactsCandidates decodes surviving fact rows into discovery
// candidates, merging each requested repo's file contexts first — the
// overlaid equivalent of scanTerraformBackendCandidateRows, which the
// active-only path runs. Groups emit in requested-repo order for a
// deterministic candidate order.
func driftFactsCandidates(rows []driftOverlayRow) ([]terraformstate.DiscoveryCandidate, error) {
	contexts := map[string]tfstate.TerraformBackendFactContext{}
	for _, row := range rows {
		repoID := strings.TrimSpace(row.repoID)
		if repoID == "" {
			continue
		}
		contextValue, err := tfstate.DecodeTerraformBackendFactContext(row.contextPayload)
		if err != nil {
			return nil, err
		}
		contexts[repoID] = tfstate.MergeTerraformBackendFactContext(contexts[repoID], contextValue)
	}
	repos := make([]string, 0, len(contexts))
	for repoID := range contexts {
		repos = append(repos, repoID)
	}
	sort.Strings(repos)
	var candidates []terraformstate.DiscoveryCandidate
	for _, repoID := range repos {
		// The backend-facts leg never resolved a checkout root (the
		// active-only scan carries no local path); blank keeps that
		// behavior: S3-shaped candidates resolve, local ones do not.
		candidates = append(candidates, tfstate.TerraformBackendCandidatesFromContext(repoID, "", contexts[repoID])...)
	}
	return candidates, nil
}

// LoadDriftConfig loads one scope's current config over the delta
// overlay: module calls merge first (the prefix maps must see the same
// live tree the resources do), then resources decode against those maps.
// It satisfies the OverlaidConfig seam. The requested generation must
// match the rows the active generation actually carries; otherwise the
// anchor raced activation and the read fails closed instead of joining
// config from the wrong generation.
func (o TerraformDriftOverlay) LoadDriftConfig(
	ctx context.Context,
	scopeID string,
	generationID string,
) (tfstate.OverlaidConfig, error) {
	if o.Store == nil {
		return tfstate.OverlaidConfig{}, fmt.Errorf("terraform drift overlay store is required")
	}
	sink := o.sink(terraformDriftReadConfig)
	moduleRows, _, _, err := o.overlaidRows(ctx, sink, tfstate.ActiveModuleCallsForOverlayQuery, []any{scopeID}, scanDriftPayloadRow)
	if err != nil {
		return tfstate.OverlaidConfig{}, err
	}
	configRows, _, targets, err := o.overlaidRows(ctx, sink, tfstate.ActiveConfigResourcesForOverlayQuery, []any{scopeID}, scanDriftPayloadRow)
	if err != nil {
		return tfstate.OverlaidConfig{}, err
	}
	// Bind the check to the delta targets the snapshot actually
	// reached, not to the config rows: a delta that touches no
	// resources leaves activeRows empty, which made the old loop
	// vacuous and let a superseded anchor read the new
	// generation's config. The reach-the-target check below fails
	// closed even when the anchor generation carries zero rows.
	for _, tgt := range targets {
		if tgt.scopeID != scopeID {
			continue
		}
		if tgt.generationID != generationID {
			return tfstate.OverlaidConfig{}, fmt.Errorf(
				"terraform drift overlay config generation mismatch: anchor %q is not the scope's active generation %q",
				generationID, tgt.generationID,
			)
		}
	}
	recorder := tfstate.PostgresDriftEvidenceLoader{
		Instruments: o.Store.Instruments(),
		Logger:      o.Store.Logger(),
	}.UnresolvedRecorder()
	var modulePayloads [][]byte
	for _, row := range moduleRows {
		modulePayloads = append(modulePayloads, row.payload)
	}
	prefixMap, confidenceMap, err := tfstate.ModulePrefixMapFromPayloads(ctx, modulePayloads, recorder)
	if err != nil {
		return tfstate.OverlaidConfig{}, err
	}
	out := map[string]*tfconfigstate.ResourceRow{}
	for _, row := range configRows {
		entries, err := tfstate.DecodeConfigResourceEntries(row.payload)
		if err != nil {
			return tfstate.OverlaidConfig{}, err
		}
		for _, entry := range entries {
			tfstate.EmitConfigRowsForEntry(entry, prefixMap, confidenceMap, out)
		}
	}
	return tfstate.NewOverlaidConfig(out, prefixMap, confidenceMap), nil
}
