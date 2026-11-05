// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
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

// checkDriftConfigAnchorOn fails closed when the requested anchor
// generation is not the scope's current active generation. The check
// binds the scope row in ingestion_scopes directly, on every branch:
// the delta-targets lookup only returns is_delta scopes (a targets-only
// guard never fires for a full-active scope) and the config rows can be
// legitimately empty (a row loop over them is vacuous there). An unknown
// scope has no anchor to validate, so it proceeds as before. Callers run
// it on the same snapshot as the legs it guards, so a racing activation
// cannot slip between the check and the reads.
func checkDriftConfigAnchorOn(
	ctx context.Context,
	database db.Queryer,
	scopeID string,
	generationID string,
) error {
	rows, err := database.QueryContext(ctx,
		`SELECT scope.active_generation_id FROM ingestion_scopes AS scope WHERE scope.scope_id = $1`,
		scopeID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return rows.Err()
	}
	var active sql.NullString
	if err := rows.Scan(&active); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !active.Valid || active.String != generationID {
		return fmt.Errorf(
			"terraform drift overlay config generation mismatch: anchor %q is not the scope's active generation %q",
			generationID, active.String,
		)
	}
	return nil
}

// LoadDriftConfig loads one scope's current config over the delta
// overlay: module calls merge first (the prefix maps must see the same
// live tree the resources do), then resources decode against those maps.
// It satisfies the OverlaidConfig seam. The requested generation must
// match the scope's active generation; otherwise the anchor raced
// activation and the read fails closed instead of joining config from
// the wrong generation. Anchor, module, and config legs share one
// read-only repeatable-read snapshot, so all three observe the same
// generation.
func (o TerraformDriftOverlay) LoadDriftConfig(
	ctx context.Context,
	scopeID string,
	generationID string,
) (tfstate.OverlaidConfig, error) {
	if o.Store == nil {
		return tfstate.OverlaidConfig{}, fmt.Errorf("terraform drift overlay store is required")
	}
	// Derive both base texts up front: each binds the same single scope
	// arg plus the delta-active scopes at placeholder 2.
	moduleBase, err := deriveDeltaOverlayBaseQuery(tfstate.ActiveModuleCallsForOverlayQuery, 2)
	if err != nil {
		return tfstate.OverlaidConfig{}, err
	}
	configBase, err := deriveDeltaOverlayBaseQuery(tfstate.ActiveConfigResourcesForOverlayQuery, 2)
	if err != nil {
		return tfstate.OverlaidConfig{}, err
	}
	sink := o.sink(terraformDriftReadConfig)
	// One snapshot for the anchor check and both legs: repeatable-read
	// isolation pins all three to the same generation, so a racing
	// activation fails the guard instead of splitting the read.
	var moduleRows, configRows []driftOverlayRow
	err = o.Store.withDeltaOverlaySnapshot(ctx, func(snapshot *deltaOverlaySnapshot) error {
		if err := checkDriftConfigAnchorOn(ctx, snapshot.store.database, scopeID, generationID); err != nil {
			return err
		}
		var queryErr error
		moduleRows, _, _, queryErr = o.overlaidRowsOnSnapshot(ctx, snapshot, sink, tfstate.ActiveModuleCallsForOverlayQuery, moduleBase, []any{scopeID}, scanDriftPayloadRow)
		if queryErr != nil {
			return queryErr
		}
		configRows, _, _, queryErr = o.overlaidRowsOnSnapshot(ctx, snapshot, sink, tfstate.ActiveConfigResourcesForOverlayQuery, configBase, []any{scopeID}, scanDriftPayloadRow)
		return queryErr
	})
	if err != nil {
		return tfstate.OverlaidConfig{}, err
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
