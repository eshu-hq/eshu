// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	divergenceFindingsCapability = "code_divergence.findings"
	// DivergenceFindingsDefaultLimit matches the sibling list reads: a small
	// first page, with the display-limit-plus-one probe behind it.
	DivergenceFindingsDefaultLimit = 25
	divergenceFindingsMaxLimit     = 100
	divergenceFindingsMaxOffset    = 10000
)

var errDivergenceFindingsUnavailable = errors.New("divergence findings content index is unavailable")

// DivergenceFindingsRequest is the POST /api/v0/code/divergence/findings
// body. RepoID is required and resolved against the caller's grant (an
// ungranted repo rejects with 400 before any read); Kind selects the
// equality family ("" reads both); IncludeTests opts test-file copies back
// into the member set.
type DivergenceFindingsRequest struct {
	RepoID       string `json:"repo_id"`
	Kind         string `json:"kind"`
	Limit        int    `json:"limit"`
	Offset       int    `json:"offset"`
	IncludeTests bool   `json:"include_tests"`
	// AllowedRepositoryIDs is never populated from the request body.
	AllowedRepositoryIDs []string `json:"-"`
}

// divergenceStore is the narrow content-store surface the findings read
// needs. ContentReader satisfies it; tests substitute a fixture fake.
type divergenceStore interface {
	DivergenceGroupStats(context.Context, string, codedivergence.Kind, int) ([]codedivergence.GroupStat, error)
	DivergenceMembers(context.Context, string, codedivergence.Kind, []string) (map[string][]codedivergence.Member, error)
}

func (r DivergenceFindingsRequest) validate() error {
	if strings.TrimSpace(r.RepoID) == "" {
		return fmt.Errorf("repo_id is required")
	}
	switch r.Kind {
	case "", "exact", "renamed":
	default:
		return fmt.Errorf("kind must be one of: \"\", \"exact\", \"renamed\"")
	}
	if r.Limit > divergenceFindingsMaxLimit {
		return fmt.Errorf("limit must be <= 100")
	}
	if r.Offset < 0 {
		return fmt.Errorf("offset must be >= 0")
	}
	if r.Offset > divergenceFindingsMaxOffset {
		return fmt.Errorf("offset must be <= 10000")
	}
	return nil
}

func (r DivergenceFindingsRequest) kinds() []codedivergence.Kind {
	switch r.Kind {
	case "exact":
		return []codedivergence.Kind{codedivergence.KindExact}
	case "renamed":
		return []codedivergence.Kind{codedivergence.KindRenamed}
	default:
		return []codedivergence.Kind{codedivergence.KindExact, codedivergence.KindRenamed}
	}
}

func (r DivergenceFindingsRequest) normalizedLimit() int {
	if r.Limit <= 0 {
		return DivergenceFindingsDefaultLimit
	}
	if r.Limit > divergenceFindingsMaxLimit {
		return divergenceFindingsMaxLimit
	}
	return r.Limit
}

func (h *CodeHandler) handleDivergenceFindings(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCodeDivergence,
		"POST /api/v0/code/divergence/findings",
		divergenceFindingsCapability,
	)
	defer span.End()

	var req DivergenceFindingsRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if querycontract.CapabilityUnsupported(h.profile(), divergenceFindingsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"code divergence findings require a supported query profile",
			ErrorCodeUnsupportedCapability,
			divergenceFindingsCapability,
			h.profile(),
			querycontract.RequiredProfile(divergenceFindingsCapability),
		)
		return
	}
	if err := req.validate(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, divergenceFindingsCapability) {
		return
	}

	data, err := h.divergenceFindingsData(r.Context(), req)
	if err != nil {
		if errors.Is(err, errDivergenceFindingsUnavailable) {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteSuccess(
		w,
		r,
		http.StatusOK,
		map[string]any{
			"repo_id":        req.RepoID,
			"kind":           req.Kind,
			"limit":          req.normalizedLimit(),
			"offset":         req.Offset,
			"findings":       data.findings,
			"count":          len(data.findings),
			"truncated":      data.truncated,
			"next_offset":    nextDivergenceOffset(req.Offset, len(data.findings), data.truncated),
			"suppressions":   data.suppressions,
			"source_backend": "postgres_content_store",
		},
		BuildTruthEnvelope(h.profile(), divergenceFindingsCapability, TruthBasisContentIndex, "resolved from bounded fingerprint equality groups"),
	)
}

type divergenceFindingsData struct {
	findings     []map[string]any
	suppressions map[string]int
	truncated    bool
}

func (h *CodeHandler) divergenceFindingsData(
	ctx context.Context,
	req DivergenceFindingsRequest,
) (divergenceFindingsData, error) {
	if h == nil || h.Content == nil {
		return divergenceFindingsData{}, errDivergenceFindingsUnavailable
	}
	reader, ok := h.Content.(divergenceStore)
	if !ok {
		return divergenceFindingsData{}, errDivergenceFindingsUnavailable
	}
	// Required-repo pattern (like call-graph metrics): the selector above
	// already rejected an ungranted repo_id, so the read binds repo_id
	// equality and nothing wider. A grantless scoped caller never reaches
	// here; anything else that does gets the empty page, not an error.
	_, blocked := codeContentGrantScope(ctx, req.RepoID)
	if blocked {
		return divergenceFindingsData{
			findings:     []map[string]any{},
			suppressions: map[string]int{},
		}, nil
	}
	displayLimit := req.normalizedLimit()
	// Phase one: rank every multi-member group in the repo (narrow stat
	// rows, cheap at corpus scale) and window the merged cross-kind order
	// before any member fetch, so paging is exact: score desc, finding id
	// asc, with a one-group probe past the display limit.
	stats := make([]codedivergence.GroupStat, 0, 64)
	for _, kind := range req.kinds() {
		kindStats, err := reader.DivergenceGroupStats(ctx, req.RepoID, kind, codedivergence.TokenFloor)
		if err != nil {
			return divergenceFindingsData{}, err
		}
		stats = append(stats, kindStats...)
	}
	window := codedivergence.PageStats(req.RepoID, stats, req.Offset, displayLimit+1)
	truncated := len(window) > displayLimit
	if truncated {
		window = window[:displayLimit]
	}
	// Phase two: hydrate exactly the window's members, one lookup per kind.
	byKind := map[codedivergence.Kind][]string{}
	for _, stat := range window {
		byKind[stat.Kind] = append(byKind[stat.Kind], stat.Fingerprint)
	}
	groups := make([]codedivergence.Group, 0, len(window))
	for kind, fingerprints := range byKind {
		members, err := reader.DivergenceMembers(ctx, req.RepoID, kind, fingerprints)
		if err != nil {
			return divergenceFindingsData{}, err
		}
		for _, fingerprint := range fingerprints {
			groups = append(groups, codedivergence.Group{
				Fingerprint: fingerprint,
				Members:     members[fingerprint],
			})
		}
	}
	// Assemble per kind (the finding kind stamps from its stream), merge
	// suppression counts, and emit in window order. Suppression can only
	// remove window entries, never reorder them, so the merged page keeps
	// the exact score-desc, finding-id order PageStats produced.
	byKindGroups := map[codedivergence.Kind][]codedivergence.Group{}
	for _, group := range groups {
		kind := groupKind(window, group.Fingerprint)
		byKindGroups[kind] = append(byKindGroups[kind], group)
	}
	byID := map[string]map[string]any{}
	suppressions := map[string]int{}
	for kind, kindGroups := range byKindGroups {
		page, counts := codedivergence.AssemblePage(req.RepoID, kind, kindGroups, req.IncludeTests)
		for rule, count := range counts {
			suppressions[rule] += count
		}
		for _, finding := range page {
			byID[finding.ID] = divergenceFindingResult(finding)
		}
	}
	findings := make([]map[string]any, 0, len(window))
	for _, stat := range window {
		if result, ok := byID[codedivergence.StatID(req.RepoID, stat.Kind, stat.Fingerprint)]; ok {
			findings = append(findings, result)
		}
	}
	return divergenceFindingsData{findings: findings, suppressions: suppressions, truncated: truncated}, nil
}

// groupKind recovers a window group's kind by fingerprint. Fingerprints are
// unique per kind stream in the window (phase one groups by one column), so
// the first stat carrying the fingerprint owns it.
func groupKind(window []codedivergence.GroupStat, fingerprint string) codedivergence.Kind {
	for _, stat := range window {
		if stat.Fingerprint == fingerprint {
			return stat.Kind
		}
	}
	return codedivergence.KindExact
}

func divergenceFindingResult(finding codedivergence.Finding) map[string]any {
	members := make([]map[string]any, 0, len(finding.Members))
	for _, member := range finding.Members {
		members = append(members, map[string]any{
			"entity_id":     member.EntityID,
			"entity_name":   member.EntityName,
			"entity_type":   member.EntityType,
			"file_path":     member.RelativePath,
			"relative_path": member.RelativePath,
			"repo_id":       finding.RepoID,
			"language":      member.Language,
			"package":       codedivergence.PackageOf(member.RelativePath),
			"start_line":    member.StartLine,
			"end_line":      member.EndLine,
			"token_count":   member.TokenCount,
			"source_handle": map[string]any{
				"repo_id":        finding.RepoID,
				"file_path":      member.RelativePath,
				"relative_path":  member.RelativePath,
				"start_line":     member.StartLine,
				"end_line":       member.EndLine,
				"entity_id":      member.EntityID,
				"content_tool":   "get_file_lines",
				"drilldown_tool": "get_entity_context",
			},
		})
	}
	reasons := make([]map[string]any, 0, len(finding.Reasons))
	for _, reason := range finding.Reasons {
		reasons = append(reasons, map[string]any{
			"code":     reason.Code,
			"sentence": reason.Sentence,
			"value":    reason.Value,
		})
	}
	return map[string]any{
		"finding_id":  finding.ID,
		"kind":        string(finding.Kind),
		"fingerprint": finding.Fingerprint,
		"score":       finding.Score,
		"members":     members,
		"reasons":     reasons,
	}
}

func nextDivergenceOffset(offset, count int, truncated bool) any {
	if !truncated {
		return nil
	}
	return offset + count
}

// DivergenceInvestigateRequest is the POST
// /api/v0/code/divergence/investigate body: one finding addressed by
// (repo_id, kind, fingerprint). Kind is required here (no both-kinds
// default): a fingerprint is only unique within its equality family.
type DivergenceInvestigateRequest struct {
	RepoID      string `json:"repo_id"`
	Kind        string `json:"kind"`
	Fingerprint string `json:"fingerprint"`
}

func (r DivergenceInvestigateRequest) validate() error {
	if strings.TrimSpace(r.RepoID) == "" {
		return fmt.Errorf("repo_id is required")
	}
	switch r.Kind {
	case "exact", "renamed":
	default:
		return fmt.Errorf("kind must be one of: \"exact\", \"renamed\"")
	}
	if strings.TrimSpace(r.Fingerprint) == "" {
		return fmt.Errorf("fingerprint is required")
	}
	return nil
}

func (r DivergenceInvestigateRequest) kind() codedivergence.Kind {
	if r.Kind == "renamed" {
		return codedivergence.KindRenamed
	}
	return codedivergence.KindExact
}

func (h *CodeHandler) handleDivergenceInvestigate(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCodeDivergence,
		"POST /api/v0/code/divergence/investigate",
		divergenceFindingsCapability,
	)
	defer span.End()

	var req DivergenceInvestigateRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if querycontract.CapabilityUnsupported(h.profile(), divergenceFindingsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"code divergence findings require a supported query profile",
			ErrorCodeUnsupportedCapability,
			divergenceFindingsCapability,
			h.profile(),
			querycontract.RequiredProfile(divergenceFindingsCapability),
		)
		return
	}
	if err := req.validate(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, divergenceFindingsCapability) {
		return
	}
	if h == nil || h.Content == nil {
		WriteError(w, http.StatusServiceUnavailable, errDivergenceFindingsUnavailable.Error())
		return
	}
	reader, ok := h.Content.(divergenceStore)
	if !ok {
		WriteError(w, http.StatusServiceUnavailable, errDivergenceFindingsUnavailable.Error())
		return
	}
	if _, blocked := codeContentGrantScope(r.Context(), req.RepoID); blocked {
		WriteError(w, http.StatusNotFound, "divergence finding not found")
		return
	}
	membersByFP, err := reader.DivergenceMembers(r.Context(), req.RepoID, req.kind(), []string{req.Fingerprint})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	members := membersByFP[req.Fingerprint]
	if len(members) == 0 {
		WriteError(w, http.StatusNotFound, "divergence finding not found")
		return
	}
	finding, ok := codedivergence.AssembleFinding(req.RepoID, req.kind(), req.Fingerprint, members, false)
	if !ok {
		WriteError(w, http.StatusNotFound, "divergence finding not found")
		return
	}
	steps, truncated := codedivergence.InvestigateSteps(req.RepoID, finding)
	stepResults := make([]map[string]any, 0, len(steps))
	for _, step := range steps {
		stepResults = append(stepResults, map[string]any{
			"tool":    step.Tool,
			"args":    step.Args,
			"purpose": step.Purpose,
		})
	}
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		map[string]any{
			"repo_id":        req.RepoID,
			"finding":        divergenceFindingResult(finding),
			"next_steps":     stepResults,
			"truncated":      truncated,
			"suppressions":   finding.Suppressions,
			"source_backend": "postgres_content_store",
		},
		BuildTruthEnvelope(h.profile(), divergenceFindingsCapability, TruthBasisContentIndex, "resolved from one fingerprint equality group with bounded follow-ups"),
	)
}
