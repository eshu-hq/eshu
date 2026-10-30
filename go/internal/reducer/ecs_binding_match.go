// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
)

// appendECSBindingRows runs the declaration-binding section of the
// workload-cloud handler: extract declaration rows, merge their tally,
// resolve them through the shared handle ladder, and deduplicate against
// the sibling anchor rows on resolved workload ids (mechanism 1a keeps
// the anchor row; pre-ladder spellings like "workload:api" vs "api"
// would miss). It returns the merged rows, the merged quarantined facts,
// and the evidence-summary suffix ("" when the census is unwired).
func (h WorkloadCloudRelationshipMaterializationHandler) appendECSBindingRows(
	ctx context.Context,
	intent reducercontract.Intent,
	envelopes []facts.Envelope,
	rows []map[string]any,
	tally *workloadCloudRelationshipTally,
	quarantined []factdecode.QuarantinedFact,
) ([]map[string]any, []factdecode.QuarantinedFact, string, error) {
	ecsRows, ecsTally, ecsQuarantined, ecsTruncated, err := h.extractECSBindingRows(ctx, envelopes)
	if err != nil {
		return nil, nil, "", err
	}
	for key, count := range ecsTally.skipped {
		tally.skipped[key] += count
	}
	quarantined = append(quarantined, ecsQuarantined...)
	ecsRows, err = h.resolveWorkloadHandles(ctx, intent, ecsRows, tally)
	if err != nil {
		return nil, nil, "", err
	}
	rows = ecsBindingDeduplicateRows(rows, ecsRows, tally)
	if h.ECSDeclarationCensus == nil {
		return rows, quarantined, "", nil
	}
	summary := fmt.Sprintf("; %d ECS declaration-bound edge(s)", ecsBindingCommittedRows(rows))
	if ecsTruncated {
		summary += " (declaration census truncated)"
	}
	return rows, quarantined, summary, nil
}

// ecsBindingTfstateClaim is one applied-state candidate for an
// aws_ecs_service: the terraform_state_resource fact names the managed
// type with concrete applied attribute values (phase-2 scaffold; the
// producer emits no such facts today, so this path is proven by a
// synthetic envelope, not live data).
type ecsBindingTfstateClaim struct {
	name    string
	cluster string
	repoID  string
	path    string
	address string
}

// ecsBindingTfstateCandidates reads applied ECS service identity from
// terraform_state_resource facts. The payload shape is provisional
// (resource_type/type naming the managed type, attributes carrying the
// applied name/cluster): producer coverage is zero, so anything
// unshaped is ignored rather than rejected.
func ecsBindingTfstateCandidates(envelopes []facts.Envelope) []ecsBindingTfstateClaim {
	var claims []ecsBindingTfstateClaim
	for _, env := range envelopes {
		if env.FactKind != "terraform_state_resource" || env.IsTombstone {
			continue
		}
		managedType := anyToString(env.Payload["resource_type"])
		if managedType == "" {
			managedType = anyToString(env.Payload["type"])
		}
		if managedType != "aws_ecs_service" {
			continue
		}
		attrs, _ := env.Payload["attributes"].(map[string]any)
		name := anyToString(attrs["name"])
		cluster := anyToString(attrs["cluster"])
		if name == "" || cluster == "" {
			continue
		}
		claims = append(claims, ecsBindingTfstateClaim{
			name:    name,
			cluster: cluster,
			repoID:  anyToString(env.Payload["repo_id"]),
			path:    anyToString(env.Payload["path"]),
			address: anyToString(env.Payload["address"]),
		})
	}
	return claims
}

// ecsBindingBindService binds one live service or returns nil with the
// named skip tallied. Precedence: anchor path, applied state, resolved
// declarations; every ambiguity fails closed.
func ecsBindingBindService(
	service ecsBindingService,
	ctx ecsBindingResolutionContext,
	resolved []ecsBindingResolvedDeclaration,
	tasks ecsBindingTaskIndex,
	tfstate []ecsBindingTfstateClaim,
	instances map[string][]string,
	tally *workloadCloudRelationshipTally,
) map[string]any {
	if len(service.anchorIDs) > 0 {
		tally.skipped[ecsBindingSkipAnchorOwned]++
		return nil
	}
	for _, claim := range tfstate {
		if claim.name != service.name || !ecsBindingClusterMatches(claim.cluster, service.clusterARN) {
			continue
		}
		return ecsBindingEmitRow(service, ecsBindingBasisTfstate, ecsBindingReasonTfstateApplied,
			claim.repoID, claim.path, "", ctx, instances, tally)
	}
	matched, repeat, nameUnresolved := ecsBindingDeclarationClaims(resolved, service)
	switch {
	case repeat:
		tally.skipped[ecsBindingSkipRepeatUnresolved]++
		return nil
	case len(matched) > 1:
		tally.skipped[ecsBindingSkipDuplicateDeclaration]++
		return nil
	case len(matched) == 0:
		if nameUnresolved {
			tally.skipped[ecsBindingSkipDeclarationUnresolved]++
		} else {
			tally.skipped[ecsBindingSkipDeclarationMissing]++
		}
		return nil
	}
	if ecsBindingNameClaimedElsewhere(resolved, service) {
		tally.skipped[ecsBindingSkipSharedServiceName]++
		return nil
	}
	if !tasks[ecsBindingTaskKey{cluster: service.clusterARN, group: "service:" + service.name}] {
		tally.skipped[ecsBindingSkipUncorroborated]++
		return nil
	}
	claim := matched[0]
	return ecsBindingEmitRow(service, ecsBindingBasisDeclaration, claim.reason,
		claim.repoID, claim.path, path.Dir(claim.path), ctx, instances, tally)
}

func ecsBindingNameClaimedElsewhere(
	resolved []ecsBindingResolvedDeclaration,
	service ecsBindingService,
) bool {
	for _, entry := range resolved {
		if entry.repeat || !entry.nameOK || entry.name != service.name {
			continue
		}
		if !entry.clusterOK {
			continue
		}
		if !ecsBindingClusterMatches(entry.cluster, service.clusterARN) {
			return true
		}
	}
	return false
}

// ecsBindingClusterMatches compares a declared cluster (full ARN or bare
// name) against a live cluster ARN. A malformed ARN-shaped declaration
// never matches: fail closed.
func ecsBindingClusterMatches(declCluster, factClusterARN string) bool {
	declCluster = strings.TrimSpace(declCluster)
	factClusterARN = strings.TrimSpace(factClusterARN)
	if declCluster == "" || factClusterARN == "" {
		return false
	}
	if declCluster == factClusterARN {
		return true
	}
	short, factShort := ecsBindingClusterShortName(declCluster), ecsBindingClusterShortName(factClusterARN)
	return short != "" && short == factShort
}

// ecsBindingClusterShortName takes the trailing segment of a cluster ARN.
// A colon-bearing value without a slash is malformed (fail closed);
// anything else is already a bare name.
func ecsBindingClusterShortName(cluster string) string {
	if i := strings.LastIndex(cluster, "/"); i >= 0 {
		return cluster[i+1:]
	}
	if strings.Contains(cluster, ":") {
		return ""
	}
	return cluster
}

// ecsBindingEmitRow determines one environment and builds the USES row.
// The workload stays a bare name for the shared handle ladder, which
// resolves it globally (AWS scopes carry no repository allow-list) and
// drops same-name workloads as ambiguous.
func ecsBindingEmitRow(
	service ecsBindingService,
	basis, reason, declRepo, declPath, declDir string,
	ctx ecsBindingResolutionContext,
	instances map[string][]string,
	tally *workloadCloudRelationshipTally,
) map[string]any {
	env, ok := ecsBindingDetermineEnvironment(service, ctx, declRepo, declDir, instances)
	if !ok {
		if len(ecsBindingEnvironmentCandidates(service, ctx, declRepo, declDir, instances)) > 1 {
			tally.skipped[ecsBindingSkipEnvironmentConflict]++
		} else {
			tally.skipped[ecsBindingSkipEnvironmentMissing]++
		}
		return nil
	}
	row := map[string]any{
		"workload_id":           service.name,
		"cloud_resource_uid":    service.cloudUID,
		"relationship_type":     string(edgetype.Uses),
		"resolution_mode":       ecsBindingResolutionMode,
		"environment":           env,
		"relationship_basis":    basis,
		"service_anchor_source": "ecs_declaration",
		"service_anchor_reason": reason,
		"source_fact_id":        service.factID,
		"stable_fact_key":       service.stableFactKey,
		"source_system":         service.sourceSystem,
		"source_record_id":      service.sourceRecordID,
		"collector_kind":        service.collectorKind,
		"declaration_repo_id":   declRepo,
		"declaration_path":      declPath,
	}
	return row
}

// ecsBindingDetermineEnvironment collects the fact-grounded environment
// candidates: the service fact's anchor tag, a literal terragrunt
// environment input bridging into the declaration directory, and the
// workload's sole instance environment. Exactly one wins; zero or
// disagreement fails closed.
func ecsBindingDetermineEnvironment(
	service ecsBindingService,
	ctx ecsBindingResolutionContext,
	declRepo, declDir string,
	instances map[string][]string,
) (string, bool) {
	candidates := ecsBindingEnvironmentCandidates(service, ctx, declRepo, declDir, instances)
	if len(candidates) != 1 {
		return "", false
	}
	for env := range candidates {
		return env, true
	}
	return "", false
}

func ecsBindingEnvironmentCandidates(
	service ecsBindingService,
	ctx ecsBindingResolutionContext,
	declRepo, declDir string,
	instances map[string][]string,
) map[string]struct{} {
	candidates := make(map[string]struct{})
	if service.environment != "" {
		candidates[service.environment] = struct{}{}
	}
	if declRepo != "" && declDir != "" {
		if env, ok := ecsBindingTerragruntEnv(ctx, declRepo, declDir); ok {
			candidates[env] = struct{}{}
		}
	}
	if envs := instances[service.name]; len(envs) == 1 && strings.TrimSpace(envs[0]) != "" {
		candidates[strings.TrimSpace(envs[0])] = struct{}{}
	}
	return candidates
}

// ecsBindingTerragruntEnv reads a literal environment/env input bridging
// into the declaration directory: the stack that names the service also
// names its environment.
func ecsBindingTerragruntEnv(ctx ecsBindingResolutionContext, repoID, dir string) (string, bool) {
	for _, key := range []string{"environment", "env"} {
		if value, ok := ecsBindingResolveInDir("var."+key, ctx, repoID, dir); ok {
			return value, true
		}
	}
	return "", false
}

// extractECSBindingRows runs the declaration-binding section of the
// workload-cloud handler. A nil census keeps the pre-#9661 behavior. The
// census (the expensive global read) loads only when this scope holds ECS
// service facts; a census or lookup failure returns before any commit.
func (h WorkloadCloudRelationshipMaterializationHandler) extractECSBindingRows(
	ctx context.Context,
	envelopes []facts.Envelope,
) ([]map[string]any, workloadCloudRelationshipTally, []factdecode.QuarantinedFact, bool, error) {
	tally := newWorkloadCloudRelationshipTally()
	if h.ECSDeclarationCensus == nil {
		return nil, tally, nil, false, nil
	}
	serviceNames := make([]string, 0, len(envelopes))
	seen := make(map[string]struct{})
	for _, env := range envelopes {
		if env.FactKind != cloud.AWSResourceFactKind || env.IsTombstone {
			continue
		}
		if anyToString(env.Payload["resource_type"]) != "aws_ecs_service" {
			continue
		}
		name := anyToString(env.Payload["name"])
		if name == "" {
			name = ecsBindingShortServiceName(anyToString(env.Payload["arn"]))
		}
		if name == "" {
			continue
		}
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			serviceNames = append(serviceNames, name)
		}
	}
	if len(serviceNames) == 0 {
		return nil, tally, nil, false, nil
	}
	census, err := h.ECSDeclarationCensus.ListECSBindingDeclarations(ctx)
	if err != nil {
		return nil, tally, nil, false, fmt.Errorf("load ECS declaration census: %w", err)
	}
	var instances map[string][]string
	if h.ECSInstanceEnvironments != nil {
		instances, err = h.ECSInstanceEnvironments.ListECSBindingInstanceEnvironments(ctx, serviceNames)
		if err != nil {
			return nil, tally, nil, false, fmt.Errorf("load ECS binding instance environments: %w", err)
		}
	}
	rows, extractTally, quarantined, err := ExtractWorkloadECSBindingRows(envelopes, census, instances)
	if err != nil {
		return nil, tally, nil, false, err
	}
	return rows, extractTally, quarantined, census.Truncated, nil
}

// ecsBindingDeduplicateRows drops declaration rows the anchor path already
// covers: the same (workload, environment, service) triple keeps the
// anchor row (precedence mechanism 1a) and tallies the declaration one
// as ecs_uses_edge_exists. Distinct workloads sharing one service keep
// both rows: the writer MERGEs per (instance, resource) pair, so only an
// identical triple would clobber the anchor basis.
func ecsBindingDeduplicateRows(
	siblingRows []map[string]any,
	ecsRows []map[string]any,
	tally *workloadCloudRelationshipTally,
) []map[string]any {
	if len(ecsRows) == 0 {
		return siblingRows
	}
	owned := make(map[string]struct{}, len(siblingRows))
	for _, row := range siblingRows {
		owned[anyToString(row["workload_id"])+"\x00"+anyToString(row["environment"])+"\x00"+anyToString(row["cloud_resource_uid"])] = struct{}{}
	}
	merged := make([]map[string]any, 0, len(siblingRows)+len(ecsRows))
	merged = append(merged, siblingRows...)
	for _, row := range ecsRows {
		key := anyToString(row["workload_id"]) + "\x00" + anyToString(row["environment"]) + "\x00" + anyToString(row["cloud_resource_uid"])
		if _, ok := owned[key]; ok {
			tally.skipped[ecsBindingSkipUsesEdgeExists]++
			continue
		}
		owned[key] = struct{}{}
		merged = append(merged, row)
	}
	return merged
}

// ecsBindingCommittedRows counts declaration-bound rows surviving the
// handle ladder for the evidence summary.
func ecsBindingCommittedRows(rows []map[string]any) int {
	count := 0
	for _, row := range rows {
		if anyToString(row["resolution_mode"]) == ecsBindingResolutionMode {
			count++
		}
	}
	return count
}

// ecsBindingNoteUnattributableRepeats records one extraction-level tally
// point when for_each/count or otherwise unresolvable declarations exist
// that cannot be attributed to any single service: a visible reminder
// that unresolvable multiplicity is present, never a per-service verdict.
func ecsBindingNoteUnattributableRepeats(resolved []ecsBindingResolvedDeclaration, tally *workloadCloudRelationshipTally) {
	for _, entry := range resolved {
		if !entry.repeat || entry.nameOK {
			continue
		}
		tally.skipped[ecsBindingSkipRepeatUnresolved]++
		return
	}
}
