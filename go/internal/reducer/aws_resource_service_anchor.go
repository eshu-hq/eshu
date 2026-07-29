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
// value cloudResourceServiceAnchorFields returns for every one of the 7
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
var cloudResourceServiceAnchorFieldsAbsent = map[string]any{
	"workload_id":                "",
	"service_name":               "",
	"service_anchor_status":      "",
	"service_anchor_source":      "",
	"service_anchor_reason":      "",
	"service_anchor_names":       []string{},
	"service_anchor_name_tokens": "",
}

// cloudResourceServiceAnchorFields returns reducer-owned service anchor
// metadata for an aws_resource row. Only exact, single-target anchors are
// promotable by the service story read model; ambiguous anchors remain visible
// as drift candidates without becoming canonical dependencies. All 7 keys are
// ALWAYS present in the returned map (never omitted — see
// cloudResourceServiceAnchorFieldsAbsent): a resource with no decision, or an
// ambiguous decision that resolves no single workload_id/service_name, gets
// the explicit empty-value keys rather than missing ones.
//
// resource is the already-decoded aws_resource struct. The service-anchor
// keys (workload_id/workload_ids, service_name/service_names) and, for a small
// allow-listed set of resource types, the nested "attributes" object's own
// service_name/service_names are typed through
// awsv1.DecodeResourceAnchorAttributes / awsv1.DecodeResourceNestedAnchorAttributes
// (issue #4631) rather than read as a raw map lookup. A present-but-malformed
// value returns a non-nil error the caller must dead-letter, never a silently
// empty anchor.
func cloudResourceServiceAnchorFields(resource awsv1.Resource) (map[string]any, error) {
	decision, err := cloudResourceServiceAnchorDecisionForPayload(resource)
	if err != nil {
		return nil, err
	}
	fields := make(map[string]any, len(cloudResourceServiceAnchorFieldsAbsent))
	for key, value := range cloudResourceServiceAnchorFieldsAbsent {
		fields[key] = value
	}
	if decision.Status == "" {
		return fields, nil
	}
	fields["service_anchor_status"] = decision.Status
	fields["service_anchor_source"] = decision.Source
	fields["service_anchor_reason"] = decision.Reason
	if decision.WorkloadID != "" {
		fields["workload_id"] = decision.WorkloadID
	}
	if decision.ServiceName != "" {
		fields["service_name"] = decision.ServiceName
	}
	if len(decision.ServiceNames) > 0 {
		fields["service_anchor_names"] = append([]string(nil), decision.ServiceNames...)
		fields["service_anchor_name_tokens"] = strings.Join(decision.ServiceNames, " ")
	}
	return fields, nil
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
