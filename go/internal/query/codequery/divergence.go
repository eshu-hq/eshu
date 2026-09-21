// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
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
// ("" reads all four: exact, renamed, drifted, and wrapper_bypass); IncludeTests opts
// test-file copies back into the member set for the exact and renamed
// families. It has no effect on drifted: pairs touching test files are
// dropped at write, so no drifted fact ever carries a test member to
// resurrect.
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
	DivergenceMembersByEntityID(context.Context, string, []string) (map[string]codedivergence.Member, error)
	DriftedFindingStats(context.Context, string) ([]codedivergence.GroupStat, error)
	DriftedFindingRows(context.Context, string, []string) (map[string]codedivergence.DriftedRow, error)
}

func (r DivergenceFindingsRequest) validate() error {
	if strings.TrimSpace(r.RepoID) == "" {
		return fmt.Errorf("repo_id is required")
	}
	switch r.Kind {
	case "", "exact", "renamed", "drifted", "wrapper_bypass",
		string(codedivergence.KindExact), string(codedivergence.KindRenamed),
		string(codedivergence.KindDrifted), string(codedivergence.KindWrapperBypass):
	default:
		return fmt.Errorf("kind must be one of: \"\", \"exact\", \"renamed\", \"drifted\", \"wrapper_bypass\" (qualified \"parallel_implementation.*\" spellings accepted)")
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
	case "wrapper_bypass", string(codedivergence.KindWrapperBypass):
		return []codedivergence.Kind{codedivergence.KindWrapperBypass}
	default:
		return []codedivergence.Kind{codedivergence.KindExact, codedivergence.KindRenamed, codedivergence.KindDrifted, codedivergence.KindWrapperBypass}
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
	for _, kind := range req.kinds() {
		if kind == codedivergence.KindWrapperBypass {
			basis += " with one-hop wrapper qualification"
			break
		}
	}
	sourceBackend := "postgres_content_store"
	if data.wrapperEmitted {
		sourceBackend = "postgres_content_store+graph"
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
			"source_backend": sourceBackend,
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
	// wrapperEmitted reports a wrapper_bypass finding on the page, so the
	// response names the graph backend beside the content store.
	wrapperEmitted bool
}

// kindsContainDivergenceKind reports whether a resolved kind list selects
// one family.
func kindsContainDivergenceKind(kinds []codedivergence.Kind, kind codedivergence.Kind) bool {
	for _, candidate := range kinds {
		if candidate == kind {
			return true
		}
	}
	return false
}

// divergencePhaseOneStats ranks every multi-member group in the repo
// (narrow stat rows, cheap at corpus scale) across the requested kinds.
// Wrapper-bypass nominates from exact groups, re-stamped when the exact
// kind itself is not requested, so one family never double-reports.
func divergencePhaseOneStats(
	ctx context.Context,
	reader divergenceStore,
	req DivergenceFindingsRequest,
	kinds []codedivergence.Kind,
	wrapperRequested, exactRequested bool,
) ([]codedivergence.GroupStat, error) {
	stats := make([]codedivergence.GroupStat, 0, 64)
	for _, kind := range kinds {
		if kind == codedivergence.KindDrifted {
			driftedStats, err := reader.DriftedFindingStats(ctx, req.RepoID)
			if err != nil {
				return nil, err
			}
			stats = append(stats, driftedStats...)
			continue
		}
		if kind == codedivergence.KindWrapperBypass {
			// Wrapper-bypass nominates from exact groups (phase two
			// partitions them); it never stats the store itself, so a
			// wrapper family cannot double-report as both exact and
			// wrapper_bypass.
			continue
		}
		kindStats, err := reader.DivergenceGroupStats(ctx, req.RepoID, kind, codedivergence.TokenFloor)
		if err != nil {
			return nil, err
		}
		stats = append(stats, kindStats...)
	}
	if wrapperRequested && !exactRequested {
		// Wrapper-only page: the exact stats still nominate, re-stamped
		// so finding ids derive from the wrapper kind.
		nominators, err := reader.DivergenceGroupStats(ctx, req.RepoID, codedivergence.KindExact, codedivergence.TokenFloor)
		if err != nil {
			return nil, err
		}
		for _, stat := range nominators {
			stat.Kind = codedivergence.KindWrapperBypass
			stats = append(stats, stat)
		}
	}
	return stats, nil
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
	kinds := req.kinds()
	wrapperRequested := kindsContainDivergenceKind(kinds, codedivergence.KindWrapperBypass)
	exactRequested := kindsContainDivergenceKind(kinds, codedivergence.KindExact)
	stats, err := divergencePhaseOneStats(ctx, reader, req, kinds, wrapperRequested, exactRequested)
	if err != nil {
		return divergenceFindingsData{}, err
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
		if kind == codedivergence.KindWrapperBypass {
			// Wrapper nominators hydrate through the exact column: the
			// shape gate below decides which groups the graph qualifies.
			members, err := reader.DivergenceMembers(ctx, req.RepoID, codedivergence.KindExact, fingerprints)
			if err != nil {
				return divergenceFindingsData{}, err
			}
			memberLookup[kind] = members
			continue
		}
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
	assembled := make([]codedivergence.Finding, 0, len(window))
	suppressions := map[string]int{}
	mergeCounts := func(counts map[string]int) {
		for rule, count := range counts {
			suppressions[rule] += count
		}
	}
	byKindGroups := map[codedivergence.Kind][]codedivergence.Group{}
	// wrapperNominations holds exact-group survivors by fingerprint for the
	// graph track. A shaped group diverts here instead of assembling as
	// exact, so it reports once as wrapper_bypass instead of suppressing
	// under wrapper_family and surfacing nowhere. Iteration below sorts
	// fingerprints so nomination order stays deterministic.
	wrapperNominations := map[string][]codedivergence.Member{}
	for _, stat := range window {
		if stat.Kind == codedivergence.KindDrifted {
			continue
		}
		members := memberLookup[stat.Kind][stat.Fingerprint]
		if wrapperRequested && (stat.Kind == codedivergence.KindExact || stat.Kind == codedivergence.KindWrapperBypass) {
			survivors, counts, shaped := codedivergence.WrapperFamilySurvivors(members, req.IncludeTests)
			mergeCounts(counts)
			if shaped {
				wrapperNominations[stat.Fingerprint] = survivors
				continue
			}
			if stat.Kind == codedivergence.KindWrapperBypass {
				suppressions[codedivergence.RuleNotWrapperFamily] += len(members)
				continue
			}
		}
		byKindGroups[stat.Kind] = append(byKindGroups[stat.Kind], codedivergence.Group{
			Fingerprint: stat.Fingerprint,
			Members:     members,
		})
	}
	for kind, kindGroups := range byKindGroups {
		page, counts := codedivergence.AssemblePage(req.RepoID, kind, kindGroups, req.IncludeTests)
		mergeCounts(counts)
		assembled = append(assembled, page...)
	}
	// Wrapper families expand 1:N: one nominated group can qualify several
	// targets, so the page may carry more findings than the requested
	// limit. Limit bounds nominating groups; cursors advance by groups
	// consumed, so nothing is lost or duplicated, only overshot.
	wrapperEmitted := false
	if len(wrapperNominations) > 0 {
		nominated := make([]string, 0, len(wrapperNominations))
		for fingerprint := range wrapperNominations {
			nominated = append(nominated, fingerprint)
		}
		sort.Strings(nominated)
		for _, fingerprint := range nominated {
			track, counts, err := h.assembleWrapperTrack(ctx, req.RepoID, wrapperNominations[fingerprint], req.IncludeTests)
			if err != nil {
				if wrapperGraphUnavailable(err) {
					suppressions[codedivergence.RuleWrapperGraphUnavailable] += len(wrapperNominations[fingerprint])
					continue
				}
				return divergenceFindingsData{}, err
			}
			mergeCounts(counts)
			if len(track) > 0 {
				wrapperEmitted = true
			}
			assembled = append(assembled, track...)
		}
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
	return divergenceFindingsData{findings: findings, suppressions: suppressions, truncated: truncated, consumed: len(window), wrapperEmitted: wrapperEmitted}, nil
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
	result := map[string]any{
		"finding_id":  finding.ID,
		"kind":        string(finding.Kind),
		"fingerprint": finding.Fingerprint,
		"score":       finding.Score,
		"members":     members,
		"reasons":     reasons,
	}
	if finding.Confidence > 0 {
		result["confidence"] = finding.Confidence
	}
	return result
}

func nextDivergenceOffset(offset, consumed int, truncated bool) any {
	if !truncated {
		return nil
	}
	return offset + consumed
}
