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
	// DivergenceReportDefaultTop is the number of highest-scoring findings
	// the report carries per kind: enough to judge whether a family is
	// worth opening the findings pages for, small enough to stay a rollup.
	DivergenceReportDefaultTop = 3
	divergenceReportMaxTop     = 10
	// divergenceReportMaxGroupsPerKind bounds member hydration per
	// content-index kind (exact, renamed, drifted). Stats are always fully
	// counted; only hydration windows when groups exceed the cap, and the
	// kind's truncated flag says so.
	divergenceReportMaxGroupsPerKind = 500
	// divergenceReportMaxWrapperFamilies bounds graph qualification work:
	// one wrapper track fans out per nominated family, so the report stops
	// qualifying past the cap and marks wrapper_bypass truncated.
	divergenceReportMaxWrapperFamilies = 200
)

// DivergenceReportRequest is the POST /api/v0/code/divergence/report body.
// RepoID is required and resolved against the caller's grant (an ungranted
// repo rejects with 400 before any read); TopPerKind selects how many of
// each kind's highest-scoring findings ride the report; IncludeTests opts
// test-file copies back into the member set for the exact, renamed, and
// convention_outlier families. It has no effect on drifted: pairs touching
// test files are dropped at write, so no drifted fact ever carries a test
// member to resurrect.
type DivergenceReportRequest struct {
	RepoID string `json:"repo_id"`
	// TopPerKind caps the top findings per kind. Zero selects the default.
	TopPerKind   int  `json:"top_per_kind"`
	IncludeTests bool `json:"include_tests"`
	// AllowedRepositoryIDs is never populated from the request body.
	AllowedRepositoryIDs []string `json:"-"`
}

// reportKinds is the deterministic kind order every report shape uses:
// content-index families first, then the two graph tracks.
func reportKinds() []codedivergence.Kind {
	return []codedivergence.Kind{
		codedivergence.KindExact,
		codedivergence.KindRenamed,
		codedivergence.KindDrifted,
		codedivergence.KindWrapperBypass,
		codedivergence.KindConventionOutlier,
	}
}

func (r DivergenceReportRequest) validate() error {
	if strings.TrimSpace(r.RepoID) == "" {
		return fmt.Errorf("repo_id is required")
	}
	if r.TopPerKind < 0 {
		return fmt.Errorf("top_per_kind must be >= 0")
	}
	if r.TopPerKind > divergenceReportMaxTop {
		return fmt.Errorf("top_per_kind must be <= 10")
	}
	return nil
}

func (r DivergenceReportRequest) normalizedTop() int {
	if r.TopPerKind <= 0 {
		return DivergenceReportDefaultTop
	}
	if r.TopPerKind > divergenceReportMaxTop {
		return divergenceReportMaxTop
	}
	return r.TopPerKind
}

func (h *CodeHandler) handleDivergenceReport(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCodeDivergence,
		"POST /api/v0/code/divergence/report",
		divergenceFindingsCapability,
	)
	defer span.End()

	var req DivergenceReportRequest
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

	data, err := h.divergenceReportData(r.Context(), req)
	if err != nil {
		if errors.Is(err, errDivergenceFindingsUnavailable) {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	sourceBackend := "postgres_content_store"
	if data.wrapperEmitted || data.outlierEmitted {
		sourceBackend = "postgres_content_store+graph"
	}
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		map[string]any{
			"repo_id":        req.RepoID,
			"counts":         data.counts,
			"total":          data.total,
			"top":            data.top,
			"top_per_kind":   req.normalizedTop(),
			"truncated":      data.truncated,
			"suppressions":   data.suppressions,
			"source_backend": sourceBackend,
		},
		BuildTruthEnvelope(h.profile(), divergenceFindingsCapability, TruthBasisContentIndex, "resolved from bounded fingerprint equality groups and active drifted-pair facts with one-hop wrapper qualification and cohort-bounded outlier selection"),
	)
}

// divergenceReportData is one report call: counts by kind plus the top
// findings per kind. Counts are assembled post-suppression findings over the
// scanned window, not stat nominations: a family that suppresses away
// contributes its suppression counts but no count. Content-index kinds scan
// every nominated group up to the hydration cap; the wrapper track qualifies
// nominated families up to its own cap; the outlier track always runs whole,
// bounded by cohort caps. A capped window marks its kind truncated so a
// quiet count is distinguishable from a complete one. A graph track the
// bounded read budget cuts short degrades to a counted *_graph_timeout
// suppression with its kind truncated instead of failing the other four
// kinds; only a missing store (503) or a non-graph, non-deadline error
// fails the call.
type divergenceReportData struct {
	counts         map[string]int
	total          int
	top            map[string][]map[string]any
	truncated      map[string]bool
	suppressions   map[string]int
	wrapperEmitted bool
	outlierEmitted bool
}

func newDivergenceReportData() divergenceReportData {
	data := divergenceReportData{
		counts:       map[string]int{},
		top:          map[string][]map[string]any{},
		truncated:    map[string]bool{},
		suppressions: map[string]int{},
	}
	for _, kind := range reportKinds() {
		key := string(kind)
		data.counts[key] = 0
		data.top[key] = []map[string]any{}
		data.truncated[key] = false
	}
	return data
}

func (h *CodeHandler) divergenceReportData(
	ctx context.Context,
	req DivergenceReportRequest,
) (divergenceReportData, error) {
	data := newDivergenceReportData()
	if h == nil || h.Content == nil {
		return divergenceReportData{}, errDivergenceFindingsUnavailable
	}
	reader, ok := h.Content.(divergenceStore)
	if !ok {
		return divergenceReportData{}, errDivergenceFindingsUnavailable
	}
	// Required-repo pattern (like the findings read): a grantless scoped
	// caller gets the empty report, not an error.
	_, blocked := codeContentGrantScope(ctx, req.RepoID)
	if blocked {
		return data, nil
	}
	topN := req.normalizedTop()
	mergeCounts := func(counts map[string]int) {
		for rule, count := range counts {
			data.suppressions[rule] += count
		}
	}
	// Content-index kinds scan every nominated group up to the hydration
	// cap in stat-score order, so the top slice is exact whenever the
	// window is complete.
	for _, kind := range []codedivergence.Kind{codedivergence.KindExact, codedivergence.KindRenamed, codedivergence.KindDrifted} {
		findings, counts, truncated, err := h.reportContentKind(ctx, reader, req.RepoID, kind, req.IncludeTests)
		if err != nil {
			return divergenceReportData{}, err
		}
		mergeCounts(counts)
		key := string(kind)
		data.counts[key] = len(findings)
		data.truncated[key] = truncated
		data.top[key] = topFindingMaps(findings, topN)
	}
	// Wrapper-bypass qualifies nominated exact families one track at a
	// time over one-hop graph rows, up to the family cap.
	wrapperFindings, wrapperCounts, wrapperTruncated, wrapperEmitted, err := h.reportWrapperKind(ctx, reader, req.RepoID, req.IncludeTests)
	if err != nil {
		return divergenceReportData{}, err
	}
	mergeCounts(wrapperCounts)
	data.counts[string(codedivergence.KindWrapperBypass)] = len(wrapperFindings)
	data.truncated[string(codedivergence.KindWrapperBypass)] = wrapperTruncated
	data.top[string(codedivergence.KindWrapperBypass)] = topFindingMaps(wrapperFindings, topN)
	data.wrapperEmitted = wrapperEmitted
	// Convention-outlier always runs the whole cohort sweep, bounded by
	// cohort caps, so its window is complete unless the graph is down or
	// the bounded read budget cuts the sweep short.
	outlierFindings, outlierCounts, outlierTruncated, outlierEmitted, err := h.reportOutlierKind(ctx, req.RepoID, req.IncludeTests, data.suppressions)
	if err != nil {
		return divergenceReportData{}, err
	}
	mergeCounts(outlierCounts)
	data.counts[string(codedivergence.KindConventionOutlier)] = len(outlierFindings)
	data.truncated[string(codedivergence.KindConventionOutlier)] = outlierTruncated
	data.top[string(codedivergence.KindConventionOutlier)] = topFindingMaps(outlierFindings, topN)
	data.outlierEmitted = outlierEmitted
	for _, count := range data.counts {
		data.total += count
	}
	return data, nil
}

// reportContentKind assembles every nominated group of one content-index
// kind (exact, renamed, drifted) up to the hydration cap. Drifted hydrates
// fact rows by writer finding id instead of group members by fingerprint;
// a windowed drifted row missing from hydration drops out silently, the
// same stale-read rule the findings page uses.
func (h *CodeHandler) reportContentKind(
	ctx context.Context,
	reader divergenceStore,
	repoID string,
	kind codedivergence.Kind,
	includeTests bool,
) ([]codedivergence.Finding, map[string]int, bool, error) {
	suppressions := map[string]int{}
	stats, err := codedivergence.PhaseOneStats(ctx, reader, repoID, []codedivergence.Kind{kind}, false, kind == codedivergence.KindExact)
	if err != nil {
		return nil, nil, false, err
	}
	window := codedivergence.PageStats(repoID, stats, 0, divergenceReportMaxGroupsPerKind)
	truncated := len(stats) > divergenceReportMaxGroupsPerKind
	if kind == codedivergence.KindDrifted {
		findingIDs := make([]string, 0, len(window))
		for _, stat := range window {
			findingIDs = append(findingIDs, stat.Fingerprint)
		}
		rows, err := reader.DriftedFindingRows(ctx, repoID, findingIDs)
		if err != nil {
			return nil, nil, false, err
		}
		assembled := make([]codedivergence.Finding, 0, len(window))
		for _, stat := range window {
			row, ok := rows[stat.Fingerprint]
			if !ok {
				continue
			}
			finding, ok := codedivergence.AssembleDriftedFinding(repoID, row, includeTests)
			for rule, count := range finding.Suppressions {
				suppressions[rule] += count
			}
			if !ok {
				continue
			}
			assembled = append(assembled, finding)
		}
		codedivergence.SortFindings(assembled)
		return assembled, suppressions, truncated, nil
	}
	fingerprints := make([]string, 0, len(window))
	for _, stat := range window {
		fingerprints = append(fingerprints, stat.Fingerprint)
	}
	members, err := reader.DivergenceMembers(ctx, repoID, kind, fingerprints)
	if err != nil {
		return nil, nil, false, err
	}
	groups := make([]codedivergence.Group, 0, len(window))
	for _, stat := range window {
		groups = append(groups, codedivergence.Group{Fingerprint: stat.Fingerprint, Members: members[stat.Fingerprint]})
	}
	findings, counts := codedivergence.AssemblePage(repoID, kind, groups, includeTests)
	for rule, count := range counts {
		suppressions[rule] += count
	}
	return findings, suppressions, truncated, nil
}

// reportWrapperKind qualifies every nominated wrapper family in
// deterministic fingerprint order up to the family cap. A shaped group
// diverts to the graph track instead of assembling as exact, so it counts
// once as wrapper_bypass instead of suppressing under wrapper_family and
// counting nowhere. A degraded graph backend counts one
// wrapper_graph_unavailable suppression per family instead of failing the
// report, the same degraded-read rule the findings page uses.
func (h *CodeHandler) reportWrapperKind(
	ctx context.Context,
	reader divergenceStore,
	repoID string,
	includeTests bool,
) ([]codedivergence.Finding, map[string]int, bool, bool, error) {
	suppressions := map[string]int{}
	stats, err := codedivergence.PhaseOneStats(ctx, reader, repoID, []codedivergence.Kind{codedivergence.KindWrapperBypass}, true, false)
	if err != nil {
		return nil, nil, false, false, err
	}
	window := codedivergence.PageStats(repoID, stats, 0, divergenceReportMaxWrapperFamilies)
	truncated := len(stats) > divergenceReportMaxWrapperFamilies
	fingerprints := make([]string, 0, len(window))
	for _, stat := range window {
		fingerprints = append(fingerprints, stat.Fingerprint)
	}
	sort.Strings(fingerprints)
	members, err := reader.DivergenceMembers(ctx, repoID, codedivergence.KindExact, fingerprints)
	if err != nil {
		return nil, nil, false, false, err
	}
	assembled := make([]codedivergence.Finding, 0, len(fingerprints))
	emitted := false
	for _, fingerprint := range fingerprints {
		survivors, counts, shaped := codedivergence.WrapperFamilySurvivors(members[fingerprint], includeTests)
		for rule, count := range counts {
			suppressions[rule] += count
		}
		if !shaped {
			suppressions[codedivergence.RuleNotWrapperFamily] += len(members[fingerprint])
			continue
		}
		track, counts, err := h.assembleWrapperTrack(ctx, repoID, survivors, includeTests)
		if err != nil {
			if wrapperGraphUnavailable(err) {
				suppressions[codedivergence.RuleWrapperGraphUnavailable] += len(survivors)
				continue
			}
			if errors.Is(err, querycontract.ErrGraphReadDeadline) {
				// One slow family must not veto the other four kinds: the
				// rollup counts the timeout and marks the kind truncated.
				// The findings page keeps the loud deadline.
				suppressions[codedivergence.RuleWrapperGraphTimeout] += len(survivors)
				truncated = true
				continue
			}
			return nil, nil, false, false, err
		}
		for rule, count := range counts {
			suppressions[rule] += count
		}
		if len(track) > 0 {
			emitted = true
		}
		assembled = append(assembled, track...)
	}
	codedivergence.SortFindings(assembled)
	return assembled, suppressions, truncated, emitted, nil
}

// reportOutlierKind runs the whole convention-outlier cohort sweep: the
// track pages nowhere, so the report counts every qualified finding. A
// degraded graph backend counts one outlier_graph_unavailable suppression
// instead of failing the report; a sweep the bounded read budget cuts short
// counts one outlier_graph_timeout suppression and marks the kind truncated
// instead of failing the other four kinds with it. The findings page keeps
// both loud.
func (h *CodeHandler) reportOutlierKind(
	ctx context.Context,
	repoID string,
	includeTests bool,
	suppressions map[string]int,
) ([]codedivergence.Finding, map[string]int, bool, bool, error) {
	track, counts, err := h.assembleOutlierTrack(ctx, repoID, includeTests)
	if err != nil {
		if errors.Is(err, querycontract.ErrGraphUnavailable) {
			suppressions[codedivergence.RuleOutlierGraphUnavailable]++
			return nil, map[string]int{}, false, false, nil
		}
		if errors.Is(err, querycontract.ErrGraphReadDeadline) {
			suppressions[codedivergence.RuleOutlierGraphTimeout]++
			return nil, map[string]int{}, true, false, nil
		}
		return nil, nil, false, false, err
	}
	codedivergence.SortFindings(track)
	return track, counts, false, len(track) > 0, nil
}

// topFindingMaps renders the first topN findings in final score order.
func topFindingMaps(findings []codedivergence.Finding, topN int) []map[string]any {
	if len(findings) > topN {
		findings = findings[:topN]
	}
	top := make([]map[string]any, 0, len(findings))
	for _, finding := range findings {
		top = append(top, codedivergence.ResultMap(finding))
	}
	return top
}
