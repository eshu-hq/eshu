// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/relationships"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// errOutlierFindingNotFound reports an outlier point lookup with no
// qualified cohort: the fingerprint misaddresses, the cohort is gone, or no
// majority callee qualifies. Callers map it to 404.
var errOutlierFindingNotFound = errors.New("convention-outlier finding not found")

// outlierCohortSources enumerates the cohort definitions in trust order:
// interface implementers, router handlers, package siblings.
func outlierCohortSources() []codedivergence.CohortSource {
	return []codedivergence.CohortSource{
		codedivergence.CohortInterface,
		codedivergence.CohortRouter,
		codedivergence.CohortPackage,
	}
}

// outlierScopePredicates binds the repository scope and the caller's grant
// to one graph variable, mirroring the wrapper reads: repo equality plus
// the grant condition, both in the anchoring WHERE.
func outlierScopePredicates(repoID string, access querycontract.RepositoryAccessFilter, alias string, params map[string]any) []string {
	predicates := make([]string, 0, 2)
	if strings.TrimSpace(repoID) != "" {
		params["repo_id"] = strings.TrimSpace(repoID)
		predicates = append(predicates, "coalesce("+alias+".repo_id, '') = $repo_id")
	}
	if access.Scoped() {
		// GraphParams merges into params in place; the return is the same
		// map, so no reassignment is needed here.
		access.GraphParams(params)
		predicates = append(predicates, access.GraphConditionOnProperty(alias, "repo_id"))
	}
	return predicates
}

// BuildOutlierCohortsCypher renders one cohort enumeration read for the
// whole repository: every (cohort, member) pair for one source. All three
// shapes anchor on the Function member with repo/grant in the anchoring
// WHERE and expand exactly one hop, so work is linear in the repo's
// membership rows. The NornicDB and Neo4j texts are identical: a repo-wide
// enumeration has no uid anchor to dialect-split on.
func BuildOutlierCohortsCypher(
	source codedivergence.CohortSource,
	repoID string,
	backend querycontract.GraphBackend,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	params := map[string]any{}
	_ = backend
	predicates := outlierScopePredicates(repoID, access, "member", params)
	where := ""
	if len(predicates) > 0 {
		where = "\n\t\tWHERE " + strings.Join(predicates, " AND ")
	}
	var cypher strings.Builder
	cypher.WriteString("\n\t\tMATCH (member:Function)")
	cypher.WriteString(where)
	switch source {
	case codedivergence.CohortInterface:
		// Implementers of one interface: the member's container implements
		// the interface. The container rides the edge (never scanned), so
		// no container label list is needed on either dialect.
		cypher.WriteString("\n\t\tMATCH (member)<-[:CONTAINS]-(impl)-[:IMPLEMENTS]->(iface)")
		cypher.WriteString("\n\t\tRETURN coalesce(iface.id, iface.uid) as iface_id,\n")
		cypher.WriteString("\t\t       iface.name as iface_name,\n")
	case codedivergence.CohortRouter:
		// Handlers registered on one framework router: HANDLES_ROUTE edges
		// are the existing framework-route evidence. The mount groups in
		// Go; the read returns the raw endpoint path.
		cypher.WriteString("\n\t\tMATCH (member)-[:HANDLES_ROUTE]->(ep:Endpoint)")
		cypher.WriteString("\n\t\tRETURN ep.path as endpoint_path,\n")
	default:
		// Same-role siblings in one package: members contained in Files;
		// the package directory groups in Go via PackageOf.
		cypher.WriteString("\n\t\tMATCH (member)<-[:CONTAINS]-(containerFile:File)")
		cypher.WriteString("\n\t\tRETURN containerFile.relative_path as file_path,\n")
	}
	cypher.WriteString("\t\t       coalesce(member.id, member.uid) as member_id,\n")
	cypher.WriteString("\t\t       member.name as member_name\n")
	return cypher.String(), params
}

// BuildOutlierCalleeEdgesCypher renders the batched outgoing-CALLS read for
// outlier selection: UNWIND over member entity ids, one row per outgoing
// CALLS edge with the provenance the weakest-edge confidence and the
// inferred label read. The mediation pass reuses it with mediator-candidate
// ids: same columns, same anchoring, no second shape to pin.
func BuildOutlierCalleeEdgesCypher(
	memberIDs []string,
	repoID string,
	backend querycontract.GraphBackend,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	params := map[string]any{"member_ids": dedupeEntityIDs(memberIDs)}
	predicates := make([]string, 0, 4)
	if strings.TrimSpace(repoID) != "" {
		params["repo_id"] = strings.TrimSpace(repoID)
		predicates = append(predicates, "coalesce(callee.repo_id, '') = $repo_id")
	}
	if access.Scoped() {
		params = access.GraphParams(params)
		predicates = append(predicates, access.GraphConditionOnProperty("callee", "repo_id"))
	}
	returns := "\n\t\tMATCH (member)-[rel:CALLS]->(callee)"
	if len(predicates) > 0 {
		returns += "\n\t\tWHERE " + strings.Join(predicates, " AND ")
	}
	returns += "\n\t\tRETURN mid as member_id,\n" +
		"\t\t       coalesce(callee.id, callee.uid) as callee_id,\n" +
		"\t\t       callee.name as callee_name,\n" +
		"\t\t       rel.resolution_method as edge_method,\n" +
		"\t\t       coalesce(rel.confidence, 0) as edge_confidence\n"
	if backend == querycontract.GraphBackendNornicDB {
		var cypher strings.Builder
		cypher.WriteString("\n\t\tUNWIND $member_ids AS mid\n")
		cypher.WriteString("\t\tMATCH " + relationships.NornicDBNodePattern("member", "Function", "mid") + "\n")
		cypher.WriteString(returns)
		return cypher.String(), params
	}
	// The Neo4j anchor MATCHes member:Function{uid: mid} directly instead of
	// WHERE (member.id = mid OR member.uid = mid): uid carries the
	// function_uid_unique constraint (a NodeUniqueIndexSeek), id carries no
	// index on either backend, and the canonical writer always sets both
	// properties from the same EntityID (canonicalEntityProperties +
	// canonicalNodeEntityUpsertTemplate), so the two branches are always
	// equal by construction -- the id branch only ever adds an unindexed
	// NodeByLabelScan over every Function per id (issue #7057).
	var cypher strings.Builder
	cypher.WriteString("\n\t\tUNWIND $member_ids AS mid\n")
	cypher.WriteString("\t\tMATCH (member:Function {uid: mid})")
	if strings.TrimSpace(repoID) != "" {
		cypher.WriteString("\n\t\tWHERE coalesce(member.repo_id, '') = $repo_id")
	}
	cypher.WriteString(returns)
	return cypher.String(), params
}

// scanOutlierCohortSeed shapes one enumeration row into the grouping seed,
// tagging the source the query ran for so one grouper serves all three.
func scanOutlierCohortSeed(source codedivergence.CohortSource, row map[string]any) codedivergence.OutlierCohortSeed {
	return codedivergence.OutlierCohortSeed{
		Source:       source,
		IfaceID:      StringVal(row, "iface_id"),
		IfaceName:    StringVal(row, "iface_name"),
		EndpointPath: StringVal(row, "endpoint_path"),
		FilePath:     StringVal(row, "file_path"),
		MemberID:     StringVal(row, "member_id"),
	}
}

// readOutlierCohortSeeds runs the three enumeration reads and returns the
// tagged seeds in deterministic source order.
func (h *CodeHandler) readOutlierCohortSeeds(
	ctx context.Context,
	repoID string,
	sources []codedivergence.CohortSource,
) ([]codedivergence.OutlierCohortSeed, error) {
	backend := h.graphBackend()
	access := codeGrantAccessFilter(ctx)
	seeds := []codedivergence.OutlierCohortSeed{}
	for _, source := range sources {
		cypher, params := BuildOutlierCohortsCypher(source, repoID, backend, access)
		rows, err := h.runWrapperGraphRows(ctx, cypher, params)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			seeds = append(seeds, scanOutlierCohortSeed(source, row))
		}
	}
	return seeds, nil
}

// readOutlierCalleeEdges runs the batched outgoing-CALLS read over ids in
// UNWIND chunks, unioning the rows. One statement never carries more than
// the shared evidence batch size; the union is exact because every sink is
// order-insensitive. Names maps callee ids to display names for mediation
// sentences.
func (h *CodeHandler) readOutlierCalleeEdges(
	ctx context.Context,
	ids []string,
	repoID string,
) (map[string][]codedivergence.OutlierCallerEdge, map[string]string, error) {
	backend := h.graphBackend()
	access := codeGrantAccessFilter(ctx)
	rows, err := h.runWrapperKeyChunks(ctx, ids, func(chunk []string) (string, map[string]any) {
		return BuildOutlierCalleeEdgesCypher(chunk, repoID, backend, access)
	})
	if err != nil {
		return nil, nil, err
	}
	edges := map[string][]codedivergence.OutlierCallerEdge{}
	names := map[string]string{}
	for _, row := range rows {
		member := StringVal(row, "member_id")
		edge := codedivergence.OutlierCallerEdge{
			CalleeID:       StringVal(row, "callee_id"),
			CalleeName:     StringVal(row, "callee_name"),
			EdgeMethod:     StringVal(row, "edge_method"),
			EdgeConfidence: querycontract.FloatVal(row, "edge_confidence"),
		}
		if member == "" || edge.CalleeID == "" {
			continue
		}
		edges[member] = append(edges[member], edge)
		if edge.CalleeName != "" {
			names[edge.CalleeID] = edge.CalleeName
		}
	}
	return edges, names, nil
}

// mediateOutlierVerdicts attaches wrapper mediations: an outlier reaching a
// majority callee through a mid (outlier calls mid, mid calls the callee)
// is ambiguous, not a clean miss. Mids are the outliers' own callees, so
// one reuse of the callee-edges read over the mid set derives every
// mediation with no second query shape. It returns mediations keyed by
// (majority callee, outlier).
func (h *CodeHandler) mediateOutlierVerdicts(
	ctx context.Context,
	repoID string,
	verdicts []codedivergence.OutlierVerdict,
	edges map[string][]codedivergence.OutlierCallerEdge,
	names map[string]string,
) (map[string]map[string]codedivergence.OutlierMediation, error) {
	midSet := map[string]struct{}{}
	for _, verdict := range verdicts {
		for _, outlier := range verdict.OutlierIDs {
			for _, edge := range edges[outlier] {
				if edge.CalleeID != verdict.CalleeID {
					midSet[edge.CalleeID] = struct{}{}
				}
			}
		}
	}
	mids := make([]string, 0, len(midSet))
	for id := range midSet {
		mids = append(mids, id)
	}
	sort.Strings(mids)
	midEdges, midNames, err := h.readOutlierCalleeEdges(ctx, mids, repoID)
	if err != nil {
		return nil, err
	}
	for id, name := range midNames {
		names[id] = name
	}
	mediated := map[string]map[string]codedivergence.OutlierMediation{}
	for _, verdict := range verdicts {
		for _, outlier := range verdict.OutlierIDs {
			for _, edge := range edges[outlier] {
				mid := edge.CalleeID
				if mid == verdict.CalleeID {
					continue
				}
				for _, midEdge := range midEdges[mid] {
					if midEdge.CalleeID != verdict.CalleeID {
						continue
					}
					if mediated[verdict.CalleeID] == nil {
						mediated[verdict.CalleeID] = map[string]codedivergence.OutlierMediation{}
					}
					if _, ok := mediated[verdict.CalleeID][outlier]; !ok {
						name := names[mid]
						if name == "" {
							name = mid
						}
						mediated[verdict.CalleeID][outlier] = codedivergence.OutlierMediation{
							OutlierID: outlier, MediatorID: mid, MediatorName: name,
						}
					}
				}
			}
		}
	}
	return mediated, nil
}

// assembleOutlierTrack runs the cohort sweep: enumerate the three cohort
// definitions, fan out to majority callees per cohort, mediate the outliers
// through the wrapper-bypass lens, and assemble the positive verdicts.
// Graph evidence is shared across cohorts; content details resolve once per
// member id. A negative verdict drops with counted suppressions, never
// silently.
func (h *CodeHandler) assembleOutlierTrack(
	ctx context.Context,
	repoID string,
	includeTests bool,
) ([]codedivergence.Finding, map[string]int, error) {
	params := codedivergence.DefaultOutlierParams()
	reader, ok := h.Content.(divergenceStore)
	if !ok {
		return nil, nil, errDivergenceFindingsUnavailable
	}
	seeds, err := h.readOutlierCohortSeeds(ctx, repoID, outlierCohortSources())
	if err != nil {
		return nil, nil, err
	}
	cohorts, suppressions := codedivergence.GroupOutlierCohorts(seeds, params)
	memberSet := map[string]struct{}{}
	for _, cohort := range cohorts {
		for _, member := range cohort.Members {
			memberSet[member] = struct{}{}
		}
	}
	memberIDs := make([]string, 0, len(memberSet))
	for id := range memberSet {
		memberIDs = append(memberIDs, id)
	}
	sort.Strings(memberIDs)
	edges, names, err := h.readOutlierCalleeEdges(ctx, memberIDs, repoID)
	if err != nil {
		return nil, nil, err
	}
	cohortVerdicts := make([][]codedivergence.OutlierVerdict, len(cohorts))
	flat := []codedivergence.OutlierVerdict{}
	for i, cohort := range cohorts {
		cohortVerdicts[i] = codedivergence.SelectOutliers(cohort, edges, nil, params)
		flat = append(flat, cohortVerdicts[i]...)
	}
	mediated, err := h.mediateOutlierVerdicts(ctx, repoID, flat, edges, names)
	if err != nil {
		return nil, nil, err
	}
	contentIDs := map[string]struct{}{}
	for _, verdicts := range cohortVerdicts {
		for _, verdict := range verdicts {
			for _, id := range verdict.CallerIDs {
				contentIDs[id] = struct{}{}
			}
			for _, id := range verdict.OutlierIDs {
				contentIDs[id] = struct{}{}
			}
		}
	}
	byID := map[string]codedivergence.Member{}
	if len(contentIDs) > 0 {
		ids := make([]string, 0, len(contentIDs))
		for id := range contentIDs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		byID, err = reader.DivergenceMembersByEntityID(ctx, repoID, ids)
		if err != nil {
			return nil, nil, err
		}
	}
	findings := []codedivergence.Finding{}
	for i, cohort := range cohorts {
		for _, verdict := range cohortVerdicts[i] {
			withMediation := verdict
			withMediation.Mediated = nil
			for _, outlier := range verdict.OutlierIDs {
				if mediation, ok := mediated[verdict.CalleeID][outlier]; ok {
					withMediation.Mediated = append(withMediation.Mediated, mediation)
				}
			}
			members := make([]codedivergence.Member, 0, len(verdict.CallerIDs)+len(verdict.OutlierIDs))
			for _, id := range verdict.CallerIDs {
				if member, ok := byID[id]; ok {
					members = append(members, member)
				}
			}
			for _, id := range verdict.OutlierIDs {
				if member, ok := byID[id]; ok {
					members = append(members, member)
				}
			}
			finding, ok := codedivergence.AssembleOutlierFinding(repoID, cohort, members, edges, withMediation, includeTests, params)
			for rule, count := range finding.Suppressions {
				suppressions[rule] += count
			}
			if !ok {
				continue
			}
			findings = append(findings, finding)
		}
	}
	return findings, suppressions, nil
}

// investigateOutlierCohort is the point lookup behind investigate
// kind=convention_outlier, where the fingerprint carries the cohort address
// plus the majority callee id. It runs the same selection the findings track
// runs over the addressed cohort alone: enumerate that source, filter to the
// key, fan out, mediate, assemble. Anything unqualified is a 404, never an
// ad-hoc shape.
func (h *CodeHandler) investigateOutlierCohort(
	ctx context.Context,
	repoID, fingerprint string,
	includeTests bool,
) (codedivergence.Finding, error) {
	params := codedivergence.DefaultOutlierParams()
	reader, ok := h.Content.(divergenceStore)
	if !ok {
		return codedivergence.Finding{}, errDivergenceFindingsUnavailable
	}
	source, key, calleeID, ok := codedivergence.ParseOutlierFingerprint(fingerprint)
	if !ok {
		return codedivergence.Finding{}, errOutlierFindingNotFound
	}
	seeds, err := h.readOutlierCohortSeeds(ctx, repoID, []codedivergence.CohortSource{source})
	if err != nil {
		return codedivergence.Finding{}, err
	}
	cohorts, _ := codedivergence.GroupOutlierCohorts(seeds, params)
	var cohort *codedivergence.OutlierCohort
	for i := range cohorts {
		if cohorts[i].Source == source && cohorts[i].Key == key {
			cohort = &cohorts[i]
			break
		}
	}
	if cohort == nil {
		return codedivergence.Finding{}, errOutlierFindingNotFound
	}
	edges, names, err := h.readOutlierCalleeEdges(ctx, cohort.Members, repoID)
	if err != nil {
		return codedivergence.Finding{}, err
	}
	var verdict *codedivergence.OutlierVerdict
	for _, candidate := range codedivergence.SelectOutliers(*cohort, edges, nil, params) {
		if candidate.CalleeID == calleeID {
			verdict = &codedivergence.OutlierVerdict{
				CalleeID:   candidate.CalleeID,
				CalleeName: candidate.CalleeName,
				CallerIDs:  candidate.CallerIDs,
				OutlierIDs: candidate.OutlierIDs,
				Share:      candidate.Share,
				Confidence: candidate.Confidence,
				Inferred:   candidate.Inferred,
				Mediated:   candidate.Mediated,
			}
			break
		}
	}
	if verdict == nil {
		return codedivergence.Finding{}, errOutlierFindingNotFound
	}
	mediated, err := h.mediateOutlierVerdicts(ctx, repoID, []codedivergence.OutlierVerdict{*verdict}, edges, names)
	if err != nil {
		return codedivergence.Finding{}, err
	}
	for _, outlier := range verdict.OutlierIDs {
		if mediation, ok := mediated[verdict.CalleeID][outlier]; ok {
			verdict.Mediated = append(verdict.Mediated, mediation)
		}
	}
	memberIDs := append(append([]string(nil), verdict.CallerIDs...), verdict.OutlierIDs...)
	byID, err := reader.DivergenceMembersByEntityID(ctx, repoID, memberIDs)
	if err != nil {
		return codedivergence.Finding{}, err
	}
	members := make([]codedivergence.Member, 0, len(memberIDs))
	for _, id := range memberIDs {
		if member, ok := byID[id]; ok {
			members = append(members, member)
		}
	}
	finding, ok := codedivergence.AssembleOutlierFinding(repoID, *cohort, members, edges, *verdict, includeTests, params)
	if !ok {
		return codedivergence.Finding{}, errOutlierFindingNotFound
	}
	return finding, nil
}
