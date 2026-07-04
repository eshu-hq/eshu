// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

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

// cloudResourceServiceAnchorFields returns reducer-owned service anchor
// metadata for an aws_resource row. Only exact, single-target anchors are
// promotable by the service story read model; ambiguous anchors remain visible
// as drift candidates without becoming canonical dependencies.
//
// attributes is the decoded aws_resource struct's untyped Attributes
// pass-through: the service-anchor keys (workload_id/workload_ids,
// service_name/service_names) and any nested "attributes" object are
// service-specific fields that live there, not in a named struct field.
// resourceType is the typed resource_type, passed explicitly so the anchor
// admission gate need not read it back out of the pass-through.
func cloudResourceServiceAnchorFields(attributes map[string]any, resourceType string) map[string]any {
	decision := cloudResourceServiceAnchorDecisionForPayload(attributes, resourceType)
	if decision.Status == "" {
		return nil
	}
	fields := map[string]any{
		"service_anchor_status": decision.Status,
		"service_anchor_source": decision.Source,
		"service_anchor_reason": decision.Reason,
	}
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
	return fields
}

func cloudResourceServiceAnchorDecisionForPayload(attributes map[string]any, resourceType string) cloudResourceServiceAnchorDecision {
	workloadIDs := payloadStrings(attributes, "workload_id", "workload_ids")
	serviceNames := payloadStrings(attributes, "service_name", "service_names")
	source := explicitServiceAnchorSource(workloadIDs, serviceNames, "payload")

	if len(workloadIDs) == 0 && len(serviceNames) == 0 {
		nested := payloadMap(attributes, "attributes")
		if shouldAdmitAWSAttributeServiceAnchor(resourceType) {
			serviceNames = payloadStrings(nested, "service_name", "service_names")
			source = explicitServiceAnchorSource(nil, serviceNames, "attributes")
		}
	}

	workloadIDs = uniqueSortedStrings(workloadIDs)
	serviceNames = uniqueSortedStrings(serviceNames)
	if len(workloadIDs) == 0 && len(serviceNames) == 0 {
		return cloudResourceServiceAnchorDecision{}
	}
	if len(workloadIDs) > 1 || len(serviceNames) > 1 {
		return cloudResourceServiceAnchorDecision{
			Status:       cloudResourceServiceAnchorStatusAmbiguous,
			Source:       source,
			Reason:       "multiple_service_anchors",
			ServiceNames: serviceNames,
		}
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
	return decision
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
