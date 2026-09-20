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
// ungranted repo rejects with 400 before any read); Kind selects the family
// ("" reads all three: exact, renamed, and drifted); IncludeTests opts
// test-file copies back into the member set.
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
// needs. ContentReader satisfies it; tests substitute a fixture fake. The
// drifted pair adds its own stats/rows pair because drifted findings read
// from reducer fact rows (keyed by writer finding id), not fingerprint
// groups.
type divergenceStore interface {
	DivergenceGroupStats(context.Context, string, codedivergence.Kind, int) ([]codedivergence.GroupStat, error)
	DivergenceMembers(context.Context, string, codedivergence.Kind, []string) (map[string][]codedivergence.Member, error)
	DriftedFindingStats(context.Context, string) ([]codedivergence.GroupStat, error)
	DriftedFindingRows(context.Context, string, []string) (map[string]codedivergence.DriftedRow, error)
}

func (r DivergenceFindingsRequest) validate() error {
	if strings.TrimSpace(r.RepoID) == "" {
		return fmt.Errorf("repo_id is required")
	}
	switch r.Kind {
	case "", "exact", "renamed", "drifted",
		string(codedivergence.KindExact), string(codedivergence.KindRenamed), string(codedivergence.KindDrifted):
	default:
		return fmt.Errorf("kind must be one of: \"\", \"exact\", \"renamed\", \"drifted\" (qualified \"parallel_implementation.*\" spellings accepted)")
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
	case "exact", string(codedivergence.KindExact):
		return []codedivergence.Kind{codedivergence.KindExact}
	case "renamed", string(codedivergence.KindRenamed):
		return []codedivergence.Kind{codedivergence.KindRenamed}
	case "drifted", string(codedivergence.KindDrifted):
		return []codedivergence.Kind{codedivergence.KindDrifted}
	default:
		return []codedivergence.Kind{codedivergence.KindExact, codedivergence.KindRenamed, codedivergence.KindDrifted}
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

	basis := "resolved from bounded fingerprint equality groups"
	for _, kind := range req.kinds() {
		if kind == codedivergence.KindDrifted {
			basis = "resolved from bounded fingerprint equality groups and active drifted-pair facts"
			break
		}
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
			"next_offset":    nextDivergenceOffset(req.Offset, data.consumed, data.truncated),
			"suppressions":   data.suppressions,
			"source_backend": "postgres_content_store",
		},
		BuildTruthEnvelope(h.profile(), divergenceFindingsCapability, TruthBasisContentIndex, basis),
	)
}

type divergenceFindingsData struct {
	findings     []map[string]any
	suppressions map[string]int
	truncated    bool
	// consumed is the pre-suppression stat window length behind this
	// page: next_offset advances past consumed stats, not past emitted
	// findings, so a fully-suppressed window still moves the cursor and
	// a partially-suppressed one never re-emits.
	consumed int
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
		if kind == codedivergence.KindDrifted {
			driftedStats, err := reader.DriftedFindingStats(ctx, req.RepoID)
			if err != nil {
				return divergenceFindingsData{}, err
			}
			stats = append(stats, driftedStats...)
			continue
		}
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
	// Drifted hydrates fact rows by writer finding id instead of group
	// members by fingerprint.
	byKind := map[codedivergence.Kind][]string{}
	for _, stat := range window {
		byKind[stat.Kind] = append(byKind[stat.Kind], stat.Fingerprint)
	}
	memberLookup := make(map[codedivergence.Kind]map[string][]codedivergence.Member, len(byKind))
	driftedRows := map[string]codedivergence.DriftedRow{}
	for kind, fingerprints := range byKind {
		if kind == codedivergence.KindDrifted {
			rows, err := reader.DriftedFindingRows(ctx, req.RepoID, fingerprints)
			if err != nil {
				return divergenceFindingsData{}, err
			}
			for id, row := range rows {
				driftedRows[id] = row
			}
			continue
		}
		members, err := reader.DivergenceMembers(ctx, req.RepoID, kind, fingerprints)
		if err != nil {
			return divergenceFindingsData{}, err
		}
		memberLookup[kind] = members
	}
	// Assemble per kind (the finding kind stamps from its stream) and merge
	// suppression counts. Groups rebuild from the window stats, so the
	// kind travels with its fingerprint: a fingerprint shared by both
	// kinds assembles once per kind instead of colliding on the
	// fingerprint alone. Emission follows final post-suppression score
	// order, not stat-score window order: suppression changes scores
	// unequally, so the window order goes stale under suppression while
	// the emitted scores stay exact.
	byKindGroups := map[codedivergence.Kind][]codedivergence.Group{}
	for _, stat := range window {
		if stat.Kind == codedivergence.KindDrifted {
			continue
		}
		byKindGroups[stat.Kind] = append(byKindGroups[stat.Kind], codedivergence.Group{
			Fingerprint: stat.Fingerprint,
			Members:     memberLookup[stat.Kind][stat.Fingerprint],
		})
	}
	assembled := make([]codedivergence.Finding, 0, len(window))
	suppressions := map[string]int{}
	mergeCounts := func(counts map[string]int) {
		for rule, count := range counts {
			suppressions[rule] += count
		}
	}
	for kind, kindGroups := range byKindGroups {
		page, counts := codedivergence.AssemblePage(req.RepoID, kind, kindGroups, req.IncludeTests)
		mergeCounts(counts)
		assembled = append(assembled, page...)
	}
	// Drifted rows assemble from their fact payloads (similarity, threshold,
	// band evidence ride the row, not a fingerprint group). A windowed row
	// missing from hydration drops out silently: the stat ranked it, but
	// the fact went stale between the two reads.
	for _, stat := range window {
		if stat.Kind != codedivergence.KindDrifted {
			continue
		}
		row, ok := driftedRows[stat.Fingerprint]
		if !ok {
			continue
		}
		finding, ok := codedivergence.AssembleDriftedFinding(req.RepoID, row, req.IncludeTests)
		mergeCounts(finding.Suppressions)
		if !ok {
			continue
		}
		assembled = append(assembled, finding)
	}
	codedivergence.SortFindings(assembled)
	findings := make([]map[string]any, 0, len(assembled))
	for _, finding := range assembled {
		findings = append(findings, divergenceFindingResult(finding))
	}
	return divergenceFindingsData{findings: findings, suppressions: suppressions, truncated: truncated, consumed: len(window)}, nil
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

func nextDivergenceOffset(offset, consumed int, truncated bool) any {
	if !truncated {
		return nil
	}
	return offset + consumed
}
