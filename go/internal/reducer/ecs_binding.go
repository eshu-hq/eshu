// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/reducer/cloudjoin"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
)

// ECS→workload binding edge metadata. Rows reuse the shared
// WorkloadCloudRelationship writer port (family workload_cloud_relationship)
// with a distinct resolution mode and basis so the anchor path and the
// declaration path stay distinguishable on the same USES edge shape.
const ecsBindingResolutionMode = "ecs_declaration"

const (
	// ecsBindingBasisDeclaration marks a link minted from a resolved
	// Terraform/Terragrunt declaration (mechanism 1c).
	ecsBindingBasisDeclaration = "ecs_declaration_resolved"
	// ecsBindingBasisTfstate marks a link minted from applied Terraform
	// state (mechanism 1b, phase-2 scaffold: producer coverage is zero).
	ecsBindingBasisTfstate = "ecs_tfstate_applied"
)

// Skip reasons for the ECS binding tally. Every non-binding outcome has a
// named reason so "why is service X unbound" is answerable from the
// evaluation summary without a per-edge log line.
const (
	ecsBindingSkipDeclarationMissing    = "ecs_declaration_missing"
	ecsBindingSkipDeclarationUnresolved = "ecs_declaration_unresolved"
	ecsBindingSkipRepeatUnresolved      = "ecs_declaration_repeat_unresolved"
	ecsBindingSkipDuplicateDeclaration  = "ecs_declaration_duplicate"
	ecsBindingSkipSharedServiceName     = "ecs_service_name_shared_across_clusters"
	ecsBindingSkipServiceUnobserved     = "ecs_service_unobserved"
	ecsBindingSkipServiceMalformed      = "ecs_service_evidence_malformed"
	ecsBindingSkipUncorroborated        = "ecs_service_uncorroborated"
	ecsBindingSkipTaskMalformed         = "ecs_task_evidence_malformed"
	ecsBindingSkipAnchorOwned           = "ecs_anchor_path_owns_service"
	ecsBindingSkipUsesEdgeExists        = "ecs_uses_edge_exists"
	ecsBindingSkipEnvironmentMissing    = "ecs_service_environment_missing"
	ecsBindingSkipEnvironmentConflict   = "ecs_service_environment_conflict"
)

// Resolution-path reasons recorded on each bound row.
const (
	ecsBindingReasonLiteral         = "literal"
	ecsBindingReasonVariableDefault = "variable_default"
	ecsBindingReasonLocal           = "local"
	ecsBindingReasonTerragruntInput = "terragrunt_input"
	ecsBindingReasonModuleArgument  = "module_argument"
	ecsBindingReasonTfstateApplied  = "tfstate_applied"
)

// ecsBindingCensusCap bounds one global declaration census read. Context
// truncation only fails closed (unknown references never bind), so the cap
// bounds work, never truth.
const ecsBindingCensusCap = 5000

// ECSBindingCensusCap returns the default global declaration census row
// cap for store implementations outside this package.
func ECSBindingCensusCap() int { return ecsBindingCensusCap }

// ecsBindingService is one in-scope ECS service fact read at payload level.
// Payload-level (not factschema-decoded) keeps this extractor independent
// from the sibling anchor extractor: a malformed ECS fact is tallied here
// and quarantined there, never double-counted in one surface.
type ecsBindingService struct {
	factID         string
	stableFactKey  string
	sourceSystem   string
	sourceRecordID string
	collectorKind  string
	accountID      string
	region         string
	arn            string
	name           string
	clusterARN     string
	environment    string
	anchorIDs      []string
	cloudUID       string
}

// ecsBindingClaim is one resolved declaration claiming (name, cluster).
type ecsBindingClaim struct {
	name       string
	clusterARN string
	repoID     string
	path       string
	reason     string
}

// ExtractWorkloadECSBindingRows builds canonical USES edge rows binding
// workloads to live ECS services from resolved declarations. Precedence:
//
//  1. Facts carrying an explicit workload anchor stay owned by the anchor
//     path (skipped here); rows the sibling extractor emitted for the
//     same service are deduplicated by the caller.
//  2. Applied Terraform state (phase-2 scaffold) outranks config.
//  3. Bounded static resolution (literals, variable defaults, locals,
//     terragrunt inputs; module-call arguments when a future parser
//     emits them) mints declaration candidates. Non-literal stays
//     unknown and fails closed; there is no general HCL evaluator.
//
// A candidate binds only when exactly one declaration claims its
// (name, cluster) corpus-wide, no declaration claims the name on another
// cluster, a live service with that identity exists in scope, running
// tasks with observed ECR digests corroborate it (digests never mint
// identity: they only confirm the selected target is observed), and one
// environment is determined. The workload side stays a bare name: the
// shared handle ladder resolves it globally (AWS scopes carry no
// repository allow-list), dropping same-name workloads as ambiguous.
//
// envelopes carries this scope's facts; census carries the global
// declaration census (consulted only when services exist); instances maps
// workload names to materialized instance environments (nil disables the
// single-instance leg).
func ExtractWorkloadECSBindingRows(
	envelopes []facts.Envelope,
	census ECSBindingCensusResult,
	instances map[string][]string,
) ([]map[string]any, workloadCloudRelationshipTally, []factdecode.QuarantinedFact, error) {
	tally := newWorkloadCloudRelationshipTally()
	services := ecsBindingLiveServices(envelopes, &tally)
	if len(services) == 0 {
		return nil, tally, nil, nil
	}
	// Duplicate service facts (rescan overlap) bind once: the writer MERGE
	// is idempotent, but the tally and evidence must not double-count.
	seenServices := make(map[string]struct{}, len(services))
	unique := services[:0]
	for _, service := range services {
		if _, ok := seenServices[service.cloudUID]; ok {
			continue
		}
		seenServices[service.cloudUID] = struct{}{}
		unique = append(unique, service)
	}
	services = unique
	tasks := ecsBindingTaskCorroboration(envelopes, &tally)
	tfstate := ecsBindingTfstateCandidates(envelopes)
	ctx := ecsBindingResolutionContext{Declarations: census.Declarations}
	resolved := ecsBindingResolveDeclarations(ctx)

	rows := make([]map[string]any, 0, len(services))
	for _, service := range services {
		row := ecsBindingBindService(service, ctx, resolved, tasks, tfstate, instances, &tally)
		if row != nil {
			rows = append(rows, row)
		}
	}
	ecsBindingNoteUnattributableRepeats(resolved, &tally)
	if len(rows) == 0 {
		return nil, tally, nil, nil
	}
	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["workload_id"]) + "@" + anyToString(rows[a]["environment"]) + "->" + anyToString(rows[a]["cloud_resource_uid"])
		right := anyToString(rows[b]["workload_id"]) + "@" + anyToString(rows[b]["environment"]) + "->" + anyToString(rows[b]["cloud_resource_uid"])
		return left < right
	})
	return rows, tally, nil, nil
}

// ecsBindingLiveServices reads in-scope aws_ecs_service facts at payload
// level. Anything that is not an ECS service passes through untouched for
// the sibling extractor.
func ecsBindingLiveServices(envelopes []facts.Envelope, tally *workloadCloudRelationshipTally) []ecsBindingService {
	services := make([]ecsBindingService, 0, len(envelopes))
	for _, env := range envelopes {
		if env.FactKind != cloud.AWSResourceFactKind || env.IsTombstone {
			continue
		}
		if anyToString(env.Payload["resource_type"]) != "aws_ecs_service" {
			continue
		}
		service, ok := ecsBindingReadService(env)
		if !ok {
			tally.skipped[ecsBindingSkipServiceMalformed]++
			continue
		}
		services = append(services, service)
	}
	return services
}

// ecsBindingReadService extracts the binding identity of one service fact.
// The uid rule mirrors cloudResourceUIDForResource (resource_id with ARN
// fallback, empty identity rejected) without decoding the envelope.
func ecsBindingReadService(env facts.Envelope) (ecsBindingService, bool) {
	accountID := anyToString(env.Payload["account_id"])
	region := anyToString(env.Payload["region"])
	resourceID := anyToString(env.Payload["resource_id"])
	arn := anyToString(env.Payload["arn"])
	if resourceID == "" {
		resourceID = arn
	}
	if accountID == "" || region == "" || resourceID == "" {
		return ecsBindingService{}, false
	}
	attrs, _ := env.Payload["attributes"].(map[string]any)
	name := anyToString(env.Payload["name"])
	if name == "" {
		name = ecsBindingShortServiceName(arn)
	}
	if name == "" {
		return ecsBindingService{}, false
	}
	return ecsBindingService{
		factID:         env.FactID,
		stableFactKey:  env.StableFactKey,
		sourceSystem:   env.SourceRef.SourceSystem,
		sourceRecordID: env.SourceRef.SourceRecordID,
		collectorKind:  env.CollectorKind,
		accountID:      accountID,
		region:         region,
		arn:            arn,
		name:           name,
		clusterARN:     anyToString(attrs["cluster_arn"]),
		environment:    anyToString(env.Payload["environment"]),
		anchorIDs:      ecsBindingPayloadWorkloadIDs(env.Payload),
		cloudUID:       cloudjoin.CloudResourceUID(accountID, region, "aws_ecs_service", resourceID),
	}, true
}

// ecsBindingPayloadWorkloadIDs reads the scalar+array workload union at
// payload level, mirroring DecodeResourceAnchorAttributes without its
// strict shape errors (shape ownership stays with the sibling extractor).
func ecsBindingPayloadWorkloadIDs(payload map[string]any) []string {
	var ids []string
	if id := anyToString(payload["workload_id"]); id != "" {
		ids = append(ids, id)
	}
	if list, ok := payload["workload_ids"].([]any); ok {
		for _, item := range list {
			if id := anyToString(item); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return uniqueSortedStrings(ids)
}

// ecsBindingShortServiceName takes the trailing segment of a long-form
// service ARN (service/cluster/name). Short-form ARNs resolve through the
// fact name instead.
func ecsBindingShortServiceName(arn string) string {
	arn = strings.TrimSpace(arn)
	idx := strings.Index(arn, ":service/")
	if idx < 0 {
		return ""
	}
	rest := arn[idx+len(":service/"):]
	slash := strings.Index(rest, "/")
	if slash < 0 {
		return rest
	}
	return rest[slash+1:]
}

// ecsBindingTaskCorroboration indexes running tasks with observed ECR
// digests by (cluster, group). A digest never selects a service; the
// index only confirms the selected target is observed.
type ecsBindingTaskIndex map[ecsBindingTaskKey]bool

type ecsBindingTaskKey struct {
	cluster string
	group   string
}

func ecsBindingTaskCorroboration(envelopes []facts.Envelope, tally *workloadCloudRelationshipTally) ecsBindingTaskIndex {
	index := make(ecsBindingTaskIndex)
	for _, env := range envelopes {
		if env.FactKind != cloud.AWSResourceFactKind || env.IsTombstone {
			continue
		}
		if anyToString(env.Payload["resource_type"]) != "aws_ecs_task" {
			continue
		}
		if anyToString(env.Payload["state"]) != "RUNNING" {
			continue
		}
		key, ok := ecsBindingReadTask(env)
		if !ok {
			tally.skipped[ecsBindingSkipTaskMalformed]++
			continue
		}
		if key.observed {
			index[ecsBindingTaskKey{cluster: key.cluster, group: key.group}] = true
		}
	}
	return index
}

type ecsBindingTaskRead struct {
	cluster  string
	group    string
	observed bool
}

// ecsBindingReadTask reads one task fact at payload level. Malformed task
// evidence is tallied (visible, fail closed) rather than quarantined:
// tasks are corroboration input, not edge-row input.
func ecsBindingReadTask(env facts.Envelope) (ecsBindingTaskRead, bool) {
	if anyToString(env.Payload["state"]) != "RUNNING" {
		return ecsBindingTaskRead{}, false
	}
	attrs, ok := env.Payload["attributes"].(map[string]any)
	if !ok {
		return ecsBindingTaskRead{}, false
	}
	group := anyToString(attrs["group"])
	cluster := anyToString(attrs["cluster_arn"])
	if group == "" || cluster == "" {
		return ecsBindingTaskRead{}, false
	}
	containers, ok := attrs["containers"].([]any)
	if !ok {
		return ecsBindingTaskRead{}, false
	}
	observed := false
	for _, item := range containers {
		container, ok := item.(map[string]any)
		if !ok {
			return ecsBindingTaskRead{}, false
		}
		digest := anyToString(container["image_digest"])
		if digest == "" {
			continue
		}
		if ecsBindingIsECRImage(anyToString(container["image"])) {
			observed = true
		}
	}
	return ecsBindingTaskRead{cluster: cluster, group: group, observed: observed}, true
}

// ecsBindingIsECRImage mirrors the query-side ecsECRHost gate
// (deployment/ecs_task_summary.go): only ECR-hosted images ground
// digest corroboration.
func ecsBindingIsECRImage(image string) bool {
	host, _, hasPath := strings.Cut(strings.TrimSpace(image), "/")
	if !hasPath {
		return false
	}
	if !strings.HasPrefix(host, ".") && len(host) > len(".dkr.ecr..amazonaws.com") {
		rest := strings.TrimSuffix(host, ".amazonaws.com")
		account, middle, ok := strings.Cut(rest, ".dkr.ecr.")
		if !ok || len(account) != 12 {
			return false
		}
		for _, digit := range account {
			if digit < '0' || digit > '9' {
				return false
			}
		}
		return middle != "" && !strings.Contains(middle, ".")
	}
	return false
}
