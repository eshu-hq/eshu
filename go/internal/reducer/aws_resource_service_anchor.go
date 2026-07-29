// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

const (
	cloudResourceServiceAnchorStatusStrong    = "strong"
	cloudResourceServiceAnchorStatusAmbiguous = "ambiguous"
)

type cloudResourceServiceAnchorDecision struct {
	Status       string
	Source       string
	Reason       string
	WorkloadID   string
	ServiceName  string
	ServiceNames []string
}

// cloudResourceServiceAnchorFieldsAbsent is the explicit-empty parity-key
// value applyCloudResourceServiceAnchorFields writes for every one of the 7
// service-anchor keys canonicalCloudResourceUpsertCypher's SET clause reads
// (workload_id, service_name, service_anchor_status/source/reason/names/
// name_tokens) whenever a given key has no real decided value — never
// omitted (issue #5714/#5055, following the #4995 precedent — see
// gcpCloudResourceNodeRow's parity-key comment in
// gcp_resource_materialization.go and runningImageFieldsAbsent's doc in
// aws_resource_running_image.go). The pinned NornicDB backend does not
// evaluate a key MISSING from one row of a heterogeneous UNWIND $rows batch
// as null in a SET clause, it persists a stringified representation of the
// row expression instead — a plain AWS resource (no decision, or an
// ambiguous decision with no single workload/service name) batched alongside
// an anchor-bearing AWS resource previously corrupted these properties on
// the plain resource's node.
//
// It is a FIXED-ORDER SLICE, not a map, so both iteration sites insert the 7
// keys into their destination row in the same order on every run. Go map
// iteration order is randomized, and while that is functionally irrelevant here
// (the Cypher SET clause reads row.<key> by name, never by position), a
// non-deterministic row-map dump costs a future debugger real time when
// comparing two runs. This mirrors cloudResourceRowKeyDefaults in
// go/internal/storage/cypher/cloud_resource_node_writer.go, the shared-writer
// backstop for the same class of key, which is a fixed-order slice for exactly
// this reason (PR #5867 review).
var cloudResourceServiceAnchorFieldsAbsent = []struct {
	key   string
	value any
}{
	{"workload_id", ""},
	{"service_name", ""},
	{"service_anchor_status", ""},
	{"service_anchor_source", ""},
	{"service_anchor_reason", ""},
	{"service_anchor_names", []string{}},
	{"service_anchor_name_tokens", ""},
}

// applyCloudResourceServiceAnchorAbsentFields writes every no-anchor parity key
// into row in the fixed order above. Callers that have already built a row map
// use this instead of copying from an intermediate map, so no per-resource
// allocation is spent on the common no-decision path.
func applyCloudResourceServiceAnchorAbsentFields(row map[string]any) {
	for _, field := range cloudResourceServiceAnchorFieldsAbsent {
		row[field.key] = field.value
	}
}

// applyCloudResourceServiceAnchorFields writes reducer-owned service anchor
// metadata directly into an aws_resource node row. Only exact, single-target
// anchors are promotable by the service story read model; ambiguous anchors
// remain visible as drift candidates without becoming canonical dependencies.
// All 7 keys are ALWAYS set (never omitted — see
// cloudResourceServiceAnchorFieldsAbsent): a resource with no decision, or an
// ambiguous decision that resolves no single workload_id/service_name, gets
// the explicit empty-value keys rather than missing ones.
//
// It writes into the caller's row rather than returning a fresh map, so the
// common no-decision path — every ordinary AWS resource in a batch — spends no
// per-resource map allocation on parity keys the caller would immediately copy
// out again (PR #5867 review).
//
// resource is the already-decoded aws_resource struct. The service-anchor
// keys (workload_id/workload_ids, service_name/service_names) and, for a small
// allow-listed set of resource types, the nested "attributes" object's own
// service_name/service_names are typed through
// awsv1.DecodeResourceAnchorAttributes / awsv1.DecodeResourceNestedAnchorAttributes
// (issue #4631) rather than read as a raw map lookup. A present-but-malformed
// value returns a non-nil error the caller must dead-letter, never a silently
// empty anchor.
func applyCloudResourceServiceAnchorFields(row map[string]any, resource awsv1.Resource) error {
	decision, err := cloudResourceServiceAnchorDecisionForPayload(resource)
	if err != nil {
		return err
	}
	applyCloudResourceServiceAnchorAbsentFields(row)
	if decision.Status == "" {
		return nil
	}
	row["service_anchor_status"] = decision.Status
	row["service_anchor_source"] = decision.Source
	row["service_anchor_reason"] = decision.Reason
	if decision.WorkloadID != "" {
		row["workload_id"] = decision.WorkloadID
	}
	if decision.ServiceName != "" {
		row["service_name"] = decision.ServiceName
	}
	if len(decision.ServiceNames) > 0 {
		row["service_anchor_names"] = append([]string(nil), decision.ServiceNames...)
		row["service_anchor_name_tokens"] = strings.Join(decision.ServiceNames, " ")
	}
	return nil
}

func cloudResourceServiceAnchorDecisionForPayload(resource awsv1.Resource) (cloudResourceServiceAnchorDecision, error) {
	anchor, err := awsv1.DecodeResourceAnchorAttributes(resource)
	if err != nil {
		return cloudResourceServiceAnchorDecision{}, err
	}
	workloadIDs := anchor.WorkloadIDs
	serviceNames := anchor.ServiceNames
	source := explicitServiceAnchorSource(workloadIDs, serviceNames, "payload")

	if len(workloadIDs) == 0 && len(serviceNames) == 0 && shouldAdmitAWSAttributeServiceAnchor(resource.ResourceType) {
		nested, err := awsv1.DecodeResourceNestedAnchorAttributes(resource)
		if err != nil {
			return cloudResourceServiceAnchorDecision{}, err
		}
		serviceNames = nested.ServiceNames
		source = explicitServiceAnchorSource(nil, serviceNames, "attributes")
	}

	workloadIDs = uniqueSortedStrings(workloadIDs)
	serviceNames = uniqueSortedStrings(serviceNames)
	if len(workloadIDs) == 0 && len(serviceNames) == 0 {
		return cloudResourceServiceAnchorDecision{}, nil
	}
	if len(workloadIDs) > 1 || len(serviceNames) > 1 {
		return cloudResourceServiceAnchorDecision{
			Status:       cloudResourceServiceAnchorStatusAmbiguous,
			Source:       source,
			Reason:       "multiple_service_anchors",
			ServiceNames: serviceNames,
		}, nil
	}

	decision := cloudResourceServiceAnchorDecision{
		Status:       cloudResourceServiceAnchorStatusStrong,
		Source:       source,
		Reason:       "explicit_service_anchor",
		ServiceNames: serviceNames,
	}
	if len(workloadIDs) == 1 {
		decision.WorkloadID = workloadIDs[0]
		decision.Reason = "explicit_workload_anchor"
	}
	if len(serviceNames) == 1 {
		decision.ServiceName = serviceNames[0]
		if decision.WorkloadID != "" {
			decision.Reason = "explicit_workload_and_service_anchor"
		}
	}
	return decision, nil
}

func explicitServiceAnchorSource(workloadIDs []string, serviceNames []string, prefix string) string {
	if len(workloadIDs) > 0 && len(serviceNames) > 0 {
		return prefix + ".workload_id+service_name"
	}
	if len(workloadIDs) > 0 {
		return prefix + ".workload_id"
	}
	if len(serviceNames) > 0 {
		return prefix + ".service_name"
	}
	return ""
}

func shouldAdmitAWSAttributeServiceAnchor(resourceType string) bool {
	switch strings.TrimSpace(resourceType) {
	case "aws_apprunner_service",
		"aws_ecs_service",
		"aws_proton_service",
		"aws_vpclattice_listener",
		"aws_vpclattice_service",
		"aws_xray_sampling_rule":
		return true
	default:
		return false
	}
}
