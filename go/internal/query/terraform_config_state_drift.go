// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const terraformConfigStateDriftFindingsCapability = "terraform_config_state_drift.findings.list"

const (
	terraformConfigStateDriftDefaultLimit = 100
	terraformConfigStateDriftMaxLimit     = 500
)

// TerraformConfigStateDriftFindingStore reads active Terraform
// config-vs-state drift reducer facts for one bounded state-snapshot scope.
type TerraformConfigStateDriftFindingStore interface {
	ListActiveFindings(ctx context.Context, filter TerraformConfigStateDriftFindingFilter) ([]TerraformConfigStateDriftFindingRow, error)
	CountActiveFindings(ctx context.Context, filter TerraformConfigStateDriftFindingFilter) (int, error)
}

// TerraformConfigStateDriftFindingFilter is the query-layer request shape for
// the Terraform config-vs-state drift read surface.
//
// Scoped and AllowedScopeIDs carry the caller's exact granted
// repository/ingestion-scope grant (#5442 P2, bindTerraformConfigStateDriftFilterAccess)
// through to PostgresTerraformConfigStateDriftFindingStore ->
// postgres.TerraformConfigStateDriftFindingFilter, the same defense-in-depth
// double guard IaCManagementFilter uses (#5167 W4, bindIaCManagementFilterAccess):
// when Scoped is true, the postgres store intersects every row with
// AllowedScopeIDs via `fact.scope_id = ANY(...)`, and returns zero rows
// WITHOUT querying Postgres at all when AllowedScopeIDs is empty. The zero
// value (Scoped false) preserves the pre-#5442-P2 all-scopes behavior, so
// every existing fakeTerraformConfigStateDriftStore test that builds this
// struct without setting Scoped stays correct.
type TerraformConfigStateDriftFindingFilter struct {
	ScopeID    string
	Address    string
	Outcome    string
	DriftKinds []string
	Limit      int
	Offset     int

	Scoped          bool
	AllowedScopeIDs []string
}

// TerraformConfigStateDriftFindingRow is one active finding as read back from
// storage.
//
// AmbiguousOwnerCandidatesWithheldCount is set (#5442 P1) when a scoped
// caller's grant excludes one or more of the finding's competing config
// repos: those candidates are removed from AmbiguousOwnerCandidates rather
// than leaked, and this field records how many were withheld so the caller
// can tell "ambiguous with a visible subset" apart from "ambiguous with no
// competing evidence at all." It is always zero (and omitted) for an
// unscoped caller, who always sees every candidate.
type TerraformConfigStateDriftFindingRow struct {
	FactID                                string           `json:"fact_id"`
	ScopeID                               string           `json:"scope_id"`
	GenerationID                          string           `json:"generation_id"`
	SourceSystem                          string           `json:"source_system"`
	CanonicalID                           string           `json:"canonical_id"`
	CandidateID                           string           `json:"candidate_id"`
	CandidateKind                         string           `json:"candidate_kind"`
	Outcome                               string           `json:"outcome"`
	Address                               string           `json:"address,omitempty"`
	DriftKind                             string           `json:"drift_kind,omitempty"`
	BackendKind                           string           `json:"backend_kind"`
	LocatorHash                           string           `json:"locator_hash"`
	Confidence                            float64          `json:"confidence"`
	AmbiguousOwnerCandidates              []map[string]any `json:"ambiguous_owner_candidates,omitempty"`
	AmbiguousOwnerCandidatesWithheldCount int              `json:"ambiguous_owner_candidates_withheld_count,omitempty"`
	Evidence                              []map[string]any `json:"evidence,omitempty"`
}

// PostgresTerraformConfigStateDriftFindingStore adapts
// postgres.TerraformConfigStateDriftFindingStore to the query-layer
// TerraformConfigStateDriftFindingStore contract, mirroring
// PostgresIaCManagementStore's wrapping of postgres.
// AWSCloudRuntimeDriftFindingStore.
type PostgresTerraformConfigStateDriftFindingStore struct {
	store postgres.TerraformConfigStateDriftFindingStore
}

// NewPostgresTerraformConfigStateDriftFindingStore constructs the adapter
// over an instrumented Postgres handle.
func NewPostgresTerraformConfigStateDriftFindingStore(db *sql.DB) *PostgresTerraformConfigStateDriftFindingStore {
	storeDB := &postgres.InstrumentedDB{
		Inner:     postgres.SQLDB{DB: db},
		Tracer:    otel.Tracer(telemetry.DefaultSignalName),
		StoreName: "terraform_config_state_drift",
	}
	return &PostgresTerraformConfigStateDriftFindingStore{store: postgres.NewTerraformConfigStateDriftFindingStore(storeDB)}
}

// ListActiveFindings implements TerraformConfigStateDriftFindingStore.
func (s *PostgresTerraformConfigStateDriftFindingStore) ListActiveFindings(
	ctx context.Context,
	filter TerraformConfigStateDriftFindingFilter,
) ([]TerraformConfigStateDriftFindingRow, error) {
	if s == nil {
		return nil, nil
	}
	rows, err := s.store.ListActiveFindings(ctx, terraformConfigStateDriftFilterToPostgres(filter))
	if err != nil {
		return nil, err
	}
	out := make([]TerraformConfigStateDriftFindingRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, terraformConfigStateDriftRowFromPostgres(row))
	}
	return out, nil
}

// CountActiveFindings implements TerraformConfigStateDriftFindingStore.
func (s *PostgresTerraformConfigStateDriftFindingStore) CountActiveFindings(
	ctx context.Context,
	filter TerraformConfigStateDriftFindingFilter,
) (int, error) {
	if s == nil {
		return 0, nil
	}
	return s.store.CountActiveFindings(ctx, terraformConfigStateDriftFilterToPostgres(filter))
}

// terraformConfigStateDriftFilterToPostgres maps the query-layer filter onto
// the postgres-layer filter shape. It is the single choke point both
// ListActiveFindings and CountActiveFindings use, so a field added to either
// filter struct cannot silently stop flowing through here (#5442 P2: Scoped
// and AllowedScopeIDs were previously dropped by this mapping, leaving the
// postgres store's SQL-layer grant intersection permanently inert for this
// domain even though the store itself supports it).
func terraformConfigStateDriftFilterToPostgres(
	filter TerraformConfigStateDriftFindingFilter,
) postgres.TerraformConfigStateDriftFindingFilter {
	return postgres.TerraformConfigStateDriftFindingFilter{
		ScopeID:         filter.ScopeID,
		Address:         filter.Address,
		Outcome:         filter.Outcome,
		DriftKinds:      filter.DriftKinds,
		Limit:           filter.Limit,
		Offset:          filter.Offset,
		Scoped:          filter.Scoped,
		AllowedScopeIDs: filter.AllowedScopeIDs,
	}
}

func terraformConfigStateDriftRowFromPostgres(row postgres.TerraformConfigStateDriftFindingRow) TerraformConfigStateDriftFindingRow {
	evidence := make([]map[string]any, 0, len(row.Evidence))
	for _, atom := range row.Evidence {
		evidence = append(evidence, map[string]any{
			"id":            atom.ID,
			"source_system": atom.SourceSystem,
			"evidence_type": atom.EvidenceType,
			"scope_id":      atom.ScopeID,
			"key":           atom.Key,
			"value":         atom.Value,
			"confidence":    atom.Confidence,
		})
	}
	return TerraformConfigStateDriftFindingRow{
		FactID:                   row.FactID,
		ScopeID:                  row.ScopeID,
		GenerationID:             row.GenerationID,
		SourceSystem:             row.SourceSystem,
		CanonicalID:              row.CanonicalID,
		CandidateID:              row.CandidateID,
		CandidateKind:            row.CandidateKind,
		Outcome:                  row.Outcome,
		Address:                  row.Address,
		DriftKind:                row.DriftKind,
		BackendKind:              row.BackendKind,
		LocatorHash:              row.LocatorHash,
		Confidence:               row.Confidence,
		AmbiguousOwnerCandidates: row.AmbiguousOwnerCandidates,
		Evidence:                 evidence,
	}
}

// TerraformConfigStateDriftHandler serves the Terraform config-vs-state
// drift read surface (issue #5442). It is a separate, provider-neutral
// handler/route/capability from the AWS and multi-cloud runtime-drift
// handlers: config-vs-state drift is not cloud-specific.
type TerraformConfigStateDriftHandler struct {
	Store   TerraformConfigStateDriftFindingStore
	Profile QueryProfile
}

// Mount registers the Terraform config-vs-state drift route on the given mux.
func (h *TerraformConfigStateDriftHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/terraform/config-state-drift/findings", h.handleFindings)
}

func (h *TerraformConfigStateDriftHandler) profile() QueryProfile {
	if h == nil {
		return ProfileProduction
	}
	return NormalizeQueryProfile(string(h.Profile))
}

type terraformConfigStateDriftRequest struct {
	ScopeID    string   `json:"scope_id"`
	Address    string   `json:"address"`
	Outcome    string   `json:"outcome"`
	DriftKinds []string `json:"drift_kinds"`
	Limit      int      `json:"limit"`
	Offset     int      `json:"offset"`
}

func (h *TerraformConfigStateDriftHandler) handleFindings(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryTerraformConfigStateDriftFindings,
		"POST /api/v0/terraform/config-state-drift/findings",
		terraformConfigStateDriftFindingsCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), terraformConfigStateDriftFindingsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"Terraform config-vs-state drift findings require reducer-materialized drift facts",
			ErrorCodeUnsupportedCapability,
			terraformConfigStateDriftFindingsCapability,
			h.profile(),
			requiredProfile(terraformConfigStateDriftFindingsCapability),
		)
		return
	}

	var req terraformConfigStateDriftRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter, err := normalizeTerraformConfigStateDriftRequest(req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if h == nil || h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "Terraform config-vs-state drift finding store is required")
		return
	}

	// Access scoping mirrors handleAWSRuntimeDriftFindings (#5167 Group B): a
	// scoped caller must supply an exact granted scope_id; ScopeID is always
	// required here (no account-wide fallback exists for this domain), so the
	// precheck is simpler than AWS's.
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() || (access.scoped() && !access.allowsRepositoryID(filter.ScopeID)) {
		writeTerraformConfigStateDriftFindings(w, r, h, filter, nil, 0)
		return
	}
	// #5442 P2 defense-in-depth: bind Scoped/AllowedScopeIDs onto the filter
	// so the SQL layer also enforces the caller's grant, not only this
	// handler's own precheck above.
	filter = bindTerraformConfigStateDriftFilterAccess(access, filter)

	totalFindings, err := h.Store.CountActiveFindings(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows, err := h.Store.ListActiveFindings(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	findings := terraformConfigStateDriftFindingRows(rows)
	// #5442 P1: an ambiguous finding's competing-owner candidates are
	// per-candidate cross-repo identifiers, not gated by the finding's own
	// scope_id grant check above, so they must be filtered against the
	// caller's grant independently before the response is written.
	findings = filterTerraformConfigStateDriftAmbiguousOwnerCandidates(findings, access)
	// #5442 P3: an exact finding's Evidence[] config atom carries the config
	// repo's own scope_id (anchor.ScopeID), not the finding's own granted
	// state_snapshot scope_id checked above, so it must be redacted against
	// the caller's grant independently before the response is written.
	findings = filterTerraformConfigStateDriftEvidence(findings, access)
	writeTerraformConfigStateDriftFindings(w, r, h, filter, findings, totalFindings)
}

// bindTerraformConfigStateDriftFilterAccess binds a
// TerraformConfigStateDriftFindingFilter to the caller's exact granted
// repository/ingestion-scope grant (#5442 P2), mirroring
// bindIaCManagementFilterAccess (#5167 W4). It is the one place
// Scoped/AllowedScopeIDs get set, and PostgresTerraformConfigStateDriftFindingStore
// carries them through to postgres.TerraformConfigStateDriftFindingFilter,
// which intersects every row with the grant (or returns zero rows without
// querying, for an empty grant). An all-scopes caller (no AuthContext,
// admin, or shared-key token) is unaffected: repositoryAccessFilterFromContext
// returns allScopes true, so filter.Scoped stays false and the
// handler-supplied scope_id alone bounds the read exactly as before this
// field existed. AllowedScopeIDs uses the caller's merged
// repository-and-ingestion-scope grant (repositorySearchIDs) rather than
// AllowedScopeIDs alone, because this domain's handler-level precheck
// (access.allowsRepositoryID) already accepts a grant on either list -- the
// state-snapshot scope_id itself is commonly granted as a repository ID (see
// terraform_config_state_drift_test.go) -- so the SQL-layer guard must
// intersect the same merged set the precheck already validated against.
func bindTerraformConfigStateDriftFilterAccess(
	access repositoryAccessFilter,
	filter TerraformConfigStateDriftFindingFilter,
) TerraformConfigStateDriftFindingFilter {
	filter.Scoped = access.scoped()
	if filter.Scoped {
		filter.AllowedScopeIDs = access.repositorySearchIDs()
	}
	return filter
}

// filterTerraformConfigStateDriftAmbiguousOwnerCandidates removes
// ambiguous_owner_candidates entries whose repo_id is outside a scoped
// caller's grant (#5442 P1), mirroring repositoryAccessFilter.filterRepositoryMaps's
// filter-out behavior for repository-keyed map lists. It never changes a
// finding's Outcome: an ambiguous finding stays reported as ambiguous even
// when every candidate is withheld, with AmbiguousOwnerCandidatesWithheldCount
// recording how many were removed, so a scoped caller can tell "ambiguous
// with a visible subset" apart from "ambiguous with no competing evidence at
// all" rather than seeing a payload that looks like a clean finding. An
// unscoped (admin) caller is unaffected and always sees every candidate.
func filterTerraformConfigStateDriftAmbiguousOwnerCandidates(
	findings []TerraformConfigStateDriftFindingRow,
	access repositoryAccessFilter,
) []TerraformConfigStateDriftFindingRow {
	if !access.scoped() || len(findings) == 0 {
		return findings
	}
	for i := range findings {
		if len(findings[i].AmbiguousOwnerCandidates) == 0 {
			continue
		}
		filtered := make([]map[string]any, 0, len(findings[i].AmbiguousOwnerCandidates))
		withheld := 0
		for _, candidate := range findings[i].AmbiguousOwnerCandidates {
			if access.allowsRepositoryID(StringVal(candidate, "repo_id")) {
				filtered = append(filtered, candidate)
				continue
			}
			withheld++
		}
		if len(filtered) == 0 {
			filtered = nil
		}
		findings[i].AmbiguousOwnerCandidates = filtered
		findings[i].AmbiguousOwnerCandidatesWithheldCount = withheld
	}
	return findings
}

func writeTerraformConfigStateDriftFindings(
	w http.ResponseWriter,
	r *http.Request,
	h *TerraformConfigStateDriftHandler,
	filter TerraformConfigStateDriftFindingFilter,
	findings []TerraformConfigStateDriftFindingRow,
	totalFindings int,
) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"scope_id":              filter.ScopeID,
		"address":               filter.Address,
		"outcome":               filter.Outcome,
		"drift_kinds":           filter.DriftKinds,
		"story":                 terraformConfigStateDriftStory(filter, findings, totalFindings),
		"outcome_groups":        terraformConfigStateDriftOutcomeGroups(findings),
		"drift_findings":        findings,
		"findings_count":        len(findings),
		"total_findings_count":  totalFindings,
		"limit":                 filter.Limit,
		"offset":                filter.Offset,
		"truncated":             iacManagementTruncated(filter.Offset, len(findings), totalFindings),
		"next_offset":           iacManagementNextOffset(filter.Offset, len(findings), totalFindings),
		"truth_basis":           "materialized_reducer_rows",
		"analysis_status":       "materialized_terraform_config_state_drift",
		"graph_projection_note": "read-model-backed drift surface; graph projection remains deferred, mirroring the AWS and multi-cloud runtime drift domains",
		"limitations": []string{
			"bounded to active Terraform config-vs-state drift reducer facts for the requested state-snapshot scope",
			"outcome is either \"exact\" (a classified per-address finding) or \"ambiguous\" (backend-owner resolution found more than one candidate config repo; no per-address classification ran)",
			"\"stale\", \"derived\", \"unresolved\", and \"rejected\" outcomes are not emitted by this version -- see go/internal/correlation/drift/tfconfigstate/doc.go",
		},
	}, BuildTruthEnvelope(
		h.profile(),
		terraformConfigStateDriftFindingsCapability,
		TruthBasisSemanticFacts,
		"resolved from active reducer-materialized Terraform config-vs-state drift findings",
	))
}

func terraformConfigStateDriftFindingRows(rows []TerraformConfigStateDriftFindingRow) []TerraformConfigStateDriftFindingRow {
	if rows == nil {
		return nil
	}
	return rows
}

func terraformConfigStateDriftOutcomeGroups(findings []TerraformConfigStateDriftFindingRow) []map[string]any {
	byOutcome := map[string]int{}
	var outcomes []string
	for _, finding := range findings {
		if _, ok := byOutcome[finding.Outcome]; !ok {
			outcomes = append(outcomes, finding.Outcome)
		}
		byOutcome[finding.Outcome]++
	}
	sort.Strings(outcomes)
	out := make([]map[string]any, 0, len(outcomes))
	for _, outcome := range outcomes {
		out = append(out, map[string]any{"outcome": outcome, "count": byOutcome[outcome]})
	}
	return out
}

func terraformConfigStateDriftStory(
	filter TerraformConfigStateDriftFindingFilter,
	findings []TerraformConfigStateDriftFindingRow,
	total int,
) string {
	scope := filter.ScopeID
	if scope == "" {
		scope = "the requested Terraform state scope"
	}
	return fmt.Sprintf(
		"%d active Terraform config-vs-state drift findings matched %s; %d returned in this page.",
		total,
		scope,
		len(findings),
	)
}

func normalizeTerraformConfigStateDriftRequest(req terraformConfigStateDriftRequest) (TerraformConfigStateDriftFindingFilter, error) {
	filter := TerraformConfigStateDriftFindingFilter{
		ScopeID:    strings.TrimSpace(req.ScopeID),
		Address:    strings.TrimSpace(req.Address),
		Outcome:    strings.TrimSpace(req.Outcome),
		DriftKinds: req.DriftKinds,
		Limit:      req.Limit,
		Offset:     req.Offset,
	}
	if filter.ScopeID == "" {
		return TerraformConfigStateDriftFindingFilter{}, fmt.Errorf("scope_id is required")
	}
	if !strings.HasPrefix(filter.ScopeID, "state_snapshot:") {
		return TerraformConfigStateDriftFindingFilter{}, fmt.Errorf("scope_id must be a state_snapshot scope")
	}
	if filter.Outcome != "" && filter.Outcome != "exact" && filter.Outcome != "ambiguous" {
		return TerraformConfigStateDriftFindingFilter{}, fmt.Errorf("outcome must be exact or ambiguous")
	}
	if filter.Limit <= 0 {
		filter.Limit = terraformConfigStateDriftDefaultLimit
	}
	if filter.Limit > terraformConfigStateDriftMaxLimit {
		filter.Limit = terraformConfigStateDriftMaxLimit
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter, nil
}
