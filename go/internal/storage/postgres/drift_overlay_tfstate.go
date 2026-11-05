// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/tfstate"
)

// Closed read labels for TerraformDriftReadDeltaUnion, one per overlay
// entry point below.
const (
	terraformDriftReadCanonical    = "canonical"
	terraformDriftReadBackendFacts = "backend_facts"
	terraformDriftReadConfig       = "config"
	terraformDriftReadSubject      = "terraform drift read"
	terraformDriftReadSpanEvent    = "terraform_drift_read.delta_union"
)

// TerraformDriftOverlay groups the delta-aware Terraform drift readers
// (issue #10006). The tfstate package holds the static query texts and
// pure decode helpers but cannot run the overlay itself: this package
// imports tfstate, so an import back would be a cycle. Construct one per
// wiring site from the site's FactStore and wire the value itself into
// the resolver, discovery, and evidence-loader ports: it satisfies
// tfstatebackend.TerraformBackendQuery and
// terraformstate.BackendFactReader, and its LoadDriftConfig feeds the
// evidence loader's OverlaidConfig seam. A nil Store fails every entry
// closed.
type TerraformDriftOverlay struct {
	Store *FactStore
}

// Port assertions: the overlay value plugs into production wiring
// wherever the active-only readers sit today.
var (
	_ tfstatebackend.TerraformBackendQuery = TerraformDriftOverlay{}
	_ terraformstate.BackendFactReader     = TerraformDriftOverlay{}
)

// sink names where one entry point records its overlay decisions.
func (o TerraformDriftOverlay) sink(read string) deltaOverlayTelemetry {
	sink := deltaOverlayTelemetry{
		read:      read,
		subject:   terraformDriftReadSubject,
		spanEvent: terraformDriftReadSpanEvent,
	}
	if o.Store != nil {
		if inst := o.Store.Instruments(); inst != nil {
			sink.counter = inst.TerraformDriftReadDeltaUnion
		}
	}
	return sink
}

// driftOverlayRow is one overlay-mergeable row from a tfstate overlay
// query text: the payload the reader decodes plus the row identity the
// shared overlay merge keys on. Per-text scan functions fill the fields
// their text selects; unused fields stay zero.
type driftOverlayRow struct {
	factID        string
	scopeID       string
	generationID  string
	stableKey     string
	relativePath  string
	observedAt    time.Time
	repoID        string
	repoLocalPath string
	// payload carries the decoded-array column (terraform_resources or
	// terraform_modules); contextPayload carries the backend-context
	// object column (canonical and backend-facts texts).
	payload        []byte
	contextPayload []byte
}

// overlayEnvelope projects a row to the partial envelope the shared
// merge keys on: file-kind attribution by payload relative_path plus
// stable-key replacement. Tombstones never reach here (every overlay
// text filters them); the delta's own tombstones arrive through the
// overlay inputs read and remove base rows by key there.
func overlayEnvelope(row driftOverlayRow) facts.Envelope {
	return facts.Envelope{
		FactID:        row.factID,
		ScopeID:       row.scopeID,
		GenerationID:  row.generationID,
		FactKind:      "file",
		StableFactKey: row.stableKey,
		ObservedAt:    row.observedAt,
		Payload:       map[string]any{"relative_path": row.relativePath},
	}
}

// overlaidRows runs one overlay query text through the manifest reader's
// structure: derive the base text up front (fail fast, no transaction
// wasted), page the active text on one read-only repeatable-read
// snapshot, look up the delta-active scopes, page the base text only
// when a baseline-carrying scope exists (the skip-scan: no base scan,
// no resolve, no inputs read otherwise), and merge through the shared
// overlayDeltaBase. Active args are the active text's bind parameters;
// the base text binds the same args plus the delta-active scope ids
// appended at len(activeArgs)+1, which every overlay text documents as
// its scope placeholder.
func (o TerraformDriftOverlay) overlaidRows(
	ctx context.Context,
	sink deltaOverlayTelemetry,
	activeQuery string,
	activeArgs []any,
	scan func(db.Rows) (driftOverlayRow, error),
) (merged, active []driftOverlayRow, targets []deltaOverlayTarget, err error) {
	if o.Store == nil {
		return nil, nil, nil, fmt.Errorf("terraform drift overlay store is required")
	}
	var baseQuery string
	baseQuery, err = deriveDeltaOverlayBaseQuery(activeQuery, len(activeArgs)+1)
	if err != nil {
		return nil, nil, nil, err
	}
	err = o.Store.withDeltaOverlaySnapshot(ctx, func(snapshot *deltaOverlaySnapshot) error {
		var queryErr error
		merged, active, targets, queryErr = o.overlaidRowsOnSnapshot(ctx, snapshot, sink, activeQuery, baseQuery, activeArgs, scan)
		return queryErr
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return merged, active, targets, nil
}

// overlaidRowsOnSnapshot runs one active/base leg pair on an already-open
// snapshot. Callers that read several legs under one atomic view (anchor
// plus module plus config) share the snapshot — and its cached targets
// lookup — instead of opening a snapshot per leg.
func (o TerraformDriftOverlay) overlaidRowsOnSnapshot(
	ctx context.Context,
	snapshot *deltaOverlaySnapshot,
	sink deltaOverlayTelemetry,
	activeQuery string,
	baseQuery string,
	activeArgs []any,
	scan func(db.Rows) (driftOverlayRow, error),
) (merged, active []driftOverlayRow, targets []deltaOverlayTarget, err error) {
	reader := snapshot.store
	active, err = queryDriftOverlayRows(ctx, reader.database, activeQuery, activeArgs, scan)
	if err != nil {
		return nil, nil, nil, err
	}
	targets, err = snapshot.deltaTargets(ctx)
	if err != nil {
		reader.recordDeltaOverlayFailure(ctx, sink)
		return nil, nil, nil, err
	}
	var base []driftOverlayRow
	if scopes := deltaOverlayBaseScopes(targets); len(scopes) > 0 {
		baseArgs := append(append([]any{}, activeArgs...), scopes)
		base, err = queryDriftOverlayRows(ctx, reader.database, baseQuery, baseArgs, scan)
		if err != nil {
			reader.recordDeltaOverlayFailure(ctx, sink)
			return nil, nil, nil, err
		}
	}
	merged, err = mergeDriftOverlayRows(ctx, reader, sink, targets, active, base)
	if err != nil {
		return nil, nil, nil, err
	}
	return merged, active, targets, nil
}

// queryDriftOverlayRows runs one overlay query text and scans every row.
func queryDriftOverlayRows(
	ctx context.Context,
	database db.Queryer,
	query string,
	args []any,
	scan func(db.Rows) (driftOverlayRow, error),
) ([]driftOverlayRow, error) {
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list terraform drift overlay rows: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []driftOverlayRow
	for rows.Next() {
		row, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan terraform drift overlay row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate terraform drift overlay rows: %w", err)
	}
	return out, nil
}

// mergeDriftOverlayRows overlays base rows under active rows through the
// shared overlayDeltaBase and maps the surviving envelopes back to rows
// by fact id. Every survivor originates from the active or base page,
// so the lookup is total.
func mergeDriftOverlayRows(
	ctx context.Context,
	reader FactStore,
	sink deltaOverlayTelemetry,
	targets []deltaOverlayTarget,
	active, base []driftOverlayRow,
) ([]driftOverlayRow, error) {
	toEnvelopes := func(rows []driftOverlayRow) []facts.Envelope {
		env := make([]facts.Envelope, 0, len(rows))
		for _, row := range rows {
			env = append(env, overlayEnvelope(row))
		}
		return env
	}
	merged, err := reader.overlayDeltaBase(ctx, sink, targets, toEnvelopes(active), toEnvelopes(base))
	if err != nil {
		return nil, err
	}
	byID := make(map[string]driftOverlayRow, len(active)+len(base))
	for _, row := range active {
		byID[row.factID] = row
	}
	for _, row := range base {
		byID[row.factID] = row
	}
	out := make([]driftOverlayRow, 0, len(merged))
	for _, env := range merged {
		row, ok := byID[env.FactID]
		if !ok {
			return nil, fmt.Errorf("overlay merged unknown fact id %q", env.FactID)
		}
		out = append(out, row)
	}
	return out, nil
}

// ListTerraformBackendsByLocator answers
// tfstatebackend.TerraformBackendQuery over the delta overlay:
// base-full backend files merge under their scope's active generation,
// so one (repo, scope) group survives per live owner with CommitID at
// the active generation. It satisfies the resolver's query port; wire
// the overlay value itself wherever the active-only
// tfstate.PostgresTerraformBackendQuery is constructed today. Blank
// inputs are rejected exactly like the active-only method.
func (o TerraformDriftOverlay) ListTerraformBackendsByLocator(
	ctx context.Context,
	backendKind string,
	locatorHash string,
) ([]tfstatebackend.TerraformBackendRow, error) {
	if o.Store == nil {
		return nil, fmt.Errorf("terraform drift overlay store is required")
	}
	backendKind = strings.TrimSpace(backendKind)
	if backendKind == "" {
		return nil, fmt.Errorf("backend kind must not be blank")
	}
	locatorHash = strings.TrimSpace(locatorHash)
	if locatorHash == "" {
		return nil, fmt.Errorf("locator hash must not be blank")
	}
	start := time.Now()
	ctx = db.WithQuerySummary(ctx, tfstate.TerraformBackendReadCanonical)
	sink := o.sink(terraformDriftReadCanonical)
	rows, _, targets, err := o.overlaidRows(ctx, sink, tfstate.ActiveTerraformBackendCanonicalQuery, nil, scanDriftCanonicalRow)
	if err != nil {
		tfstate.RecordBackendRead(ctx, o.Store.Instruments(), tfstate.TerraformBackendReadCanonical, start, true)
		return nil, err
	}
	out, err := matchDriftCanonicalRows(rows, targets, backendKind, locatorHash)
	if err != nil {
		tfstate.RecordBackendRead(ctx, o.Store.Instruments(), tfstate.TerraformBackendReadCanonical, start, true)
		return nil, err
	}
	tfstate.RecordBackendRead(ctx, o.Store.Instruments(), tfstate.TerraformBackendReadCanonical, start, false)
	return out, nil
}

// scanDriftCanonicalRow scans ActiveTerraformBackendCanonicalQuery's ten
// columns in order.
func scanDriftCanonicalRow(rows db.Rows) (driftOverlayRow, error) {
	var row driftOverlayRow
	var rawContext []byte
	var tombstone bool
	if err := rows.Scan(
		&row.repoID, &row.scopeID, &row.generationID, &row.observedAt,
		&row.repoLocalPath, &rawContext,
		&row.factID, &row.stableKey, &tombstone, &row.relativePath,
	); err != nil {
		return driftOverlayRow{}, err
	}
	row.contextPayload = rawContext
	return row, nil
}

// matchDriftCanonicalRows groups surviving rows by (repo, scope) at the
// scope's active generation — the delta generation for reached
// delta-active scopes, the rows' own generation otherwise — merges each
// group's backend contexts in deterministic row order, and converts the
// groups matching the composite key. Folding base rows into the active
// group (instead of one group per generation) is what keeps the anchor
// on the live generation: separate base/delta groups for one repo would
// read as ambiguity or anchor the drift join at a superseded commit.
func matchDriftCanonicalRows(
	rows []driftOverlayRow,
	targets []deltaOverlayTarget,
	backendKind string,
	locatorHash string,
) ([]tfstatebackend.TerraformBackendRow, error) {
	type canonicalGroup struct {
		repoID        string
		scopeID       string
		commitID      string
		observedAt    time.Time
		repoLocalPath string
		context       tfstate.TerraformBackendFactContext
	}
	// The live generation per scope: the delta generation for reached
	// delta-active scopes, the rows' own generation otherwise. Base rows
	// surviving the merge belong to a reached scope by construction (an
	// unresolved or refused base keeps delta rows only), so every group
	// anchors at a live commit.
	activeGen := make(map[string]string, len(targets))
	for _, target := range targets {
		activeGen[strings.TrimSpace(target.scopeID)] = strings.TrimSpace(target.generationID)
	}
	ordered := append([]driftOverlayRow{}, rows...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].repoID != ordered[j].repoID {
			return ordered[i].repoID < ordered[j].repoID
		}
		if ordered[i].scopeID != ordered[j].scopeID {
			return ordered[i].scopeID < ordered[j].scopeID
		}
		if ordered[i].generationID != ordered[j].generationID {
			return ordered[i].generationID < ordered[j].generationID
		}
		if !ordered[i].observedAt.Equal(ordered[j].observedAt) {
			return ordered[i].observedAt.Before(ordered[j].observedAt)
		}
		return ordered[i].factID < ordered[j].factID
	})
	groups := map[string]*canonicalGroup{}
	var order []string
	for _, row := range ordered {
		// Mirror the active-only reader's input hygiene
		// (tfstate/backend_canonical.go): trim grouping IDs and skip
		// blank keys so corrupt rows cannot form phantom groups.
		repoID := strings.TrimSpace(row.repoID)
		scopeID := strings.TrimSpace(row.scopeID)
		if repoID == "" || scopeID == "" || strings.TrimSpace(row.generationID) == "" {
			continue
		}
		key := repoID + "\x00" + scopeID
		group, seen := groups[key]
		if !seen {
			commitID := strings.TrimSpace(row.generationID)
			if active, ok := activeGen[scopeID]; ok {
				commitID = active
			}
			group = &canonicalGroup{
				repoID:     repoID,
				scopeID:    scopeID,
				commitID:   commitID,
				observedAt: row.observedAt.UTC(),
			}
			groups[key] = group
			order = append(order, key)
		}
		contextValue, err := tfstate.DecodeTerraformBackendFactContext(row.contextPayload)
		if err != nil {
			return nil, err
		}
		group.context = tfstate.MergeTerraformBackendFactContext(group.context, contextValue)
		if row.observedAt.Before(group.observedAt) {
			group.observedAt = row.observedAt.UTC()
		}
		// Prefer the live generation's checkout root: base rows resolve
		// the base repository fact, delta rows the delta's. A group with
		// only base rows (delta touched no backend file) keeps the base
		// path, which still names the live checkout for that file.
		if path := row.repoLocalPath; path != "" {
			if row.generationID == group.commitID || group.repoLocalPath == "" {
				group.repoLocalPath = path
			}
		}
	}
	var out []tfstatebackend.TerraformBackendRow
	for _, key := range order {
		group := groups[key]
		candidates := tfstate.TerraformBackendCandidatesFromContext(group.repoID, group.repoLocalPath, group.context)
		out = append(out, tfstate.MatchingBackendRowsFromCandidates(
			group.repoID, group.scopeID, group.commitID, group.observedAt,
			candidates, backendKind, locatorHash,
		)...)
	}
	return out, nil
}
