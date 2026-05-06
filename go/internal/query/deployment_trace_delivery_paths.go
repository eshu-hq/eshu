package query

import "strings"

func buildNormalizedDeliveryPaths(
	deploymentSources []map[string]any,
	cloudResources []map[string]any,
	k8sResources []map[string]any,
	imageRefs []string,
	k8sRelationships []map[string]any,
	deploymentEvidence map[string]any,
) []map[string]any {
	canonical := buildDeliveryPaths(deploymentSources, cloudResources, k8sResources, imageRefs, k8sRelationships)
	rows := make([]map[string]any, 0, len(canonical)+len(mapSliceValue(deploymentEvidence, "delivery_paths")))
	rows = append(rows, canonical...)
	for _, row := range mapSliceValue(deploymentEvidence, "delivery_paths") {
		entry := cloneAnyMap(row)
		if StringVal(entry, "type") == "" {
			entry["type"] = "repository_delivery_artifact"
		}
		rows = append(rows, entry)
	}

	seen := make(map[string]struct{}, len(rows))
	normalized := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entry, ok := normalizeDeliveryPathRow(row)
		if !ok {
			continue
		}
		key := normalizedDeliveryPathKey(entry)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, entry)
	}
	return normalized
}

func normalizeDeliveryPathRow(row map[string]any) (map[string]any, bool) {
	entry := cloneAnyMap(row)
	pathType := strings.TrimSpace(StringVal(entry, "type"))
	if pathType == "" {
		return nil, false
	}
	entry["type"] = pathType

	switch pathType {
	case "deployment_source":
		if StringVal(entry, "target") == "" && StringVal(entry, "target_id") == "" {
			return nil, false
		}
	case "cloud_resource":
		if StringVal(entry, "target") == "" && StringVal(entry, "target_id") == "" {
			return nil, false
		}
	case "k8s_resource":
		if StringVal(entry, "target") == "" && StringVal(entry, "target_id") == "" && StringVal(entry, "kind") == "" {
			return nil, false
		}
	case "image_ref":
		if StringVal(entry, "target") == "" {
			return nil, false
		}
	case "k8s_relationship":
		if StringVal(entry, "target") == "" && StringVal(entry, "source_name") == "" && StringVal(entry, "kind") == "" {
			return nil, false
		}
	case "repository_delivery_artifact":
		if !repositoryDeliveryArtifactHasIdentity(entry) {
			return nil, false
		}
	default:
		if !genericDeliveryPathHasIdentity(entry) {
			return nil, false
		}
	}

	return entry, true
}

func repositoryDeliveryArtifactHasIdentity(entry map[string]any) bool {
	return StringVal(entry, "path") != "" ||
		StringVal(entry, "relative_path") != "" ||
		StringVal(entry, "kind") != "" ||
		StringVal(entry, "artifact_type") != "" ||
		StringVal(entry, "evidence_kind") != "" ||
		StringVal(entry, "source_repo") != "" ||
		StringVal(entry, "service_name") != "" ||
		StringVal(entry, "workflow_name") != ""
}

func genericDeliveryPathHasIdentity(entry map[string]any) bool {
	return StringVal(entry, "target") != "" ||
		StringVal(entry, "target_id") != "" ||
		StringVal(entry, "path") != "" ||
		StringVal(entry, "relative_path") != "" ||
		StringVal(entry, "kind") != "" ||
		StringVal(entry, "artifact_type") != "" ||
		StringVal(entry, "evidence_kind") != ""
}

func normalizedDeliveryPathKey(entry map[string]any) string {
	pathType := StringVal(entry, "type")
	switch pathType {
	case "deployment_source", "cloud_resource":
		targetIdentity := StringVal(entry, "target")
		if targetIdentity == "" {
			targetIdentity = StringVal(entry, "target_id")
		}
		return pathType + "|" + targetIdentity
	case "k8s_resource":
		targetID := StringVal(entry, "target_id")
		if targetID == "" {
			targetID = StringVal(entry, "target")
		}
		return pathType + "|" + targetID + "|" + StringVal(entry, "kind")
	case "image_ref":
		return pathType + "|" + StringVal(entry, "target")
	case "k8s_relationship":
		return strings.Join([]string{
			pathType,
			StringVal(entry, "source_name"),
			StringVal(entry, "target"),
			StringVal(entry, "kind"),
		}, "|")
	case "repository_delivery_artifact":
		return strings.Join([]string{
			pathType,
			StringVal(entry, "path"),
			StringVal(entry, "relative_path"),
			StringVal(entry, "kind"),
			StringVal(entry, "artifact_type"),
			StringVal(entry, "evidence_kind"),
			StringVal(entry, "source_repo"),
			StringVal(entry, "service_name"),
		}, "|")
	}

	path := StringVal(entry, "path")
	relativePath := StringVal(entry, "relative_path")
	workflowName := StringVal(entry, "workflow_name")
	if path != "" || relativePath != "" {
		workflowName = ""
	}
	return strings.Join([]string{
		StringVal(entry, "type"),
		StringVal(entry, "target"),
		StringVal(entry, "target_id"),
		path,
		relativePath,
		StringVal(entry, "kind"),
		StringVal(entry, "artifact_type"),
		StringVal(entry, "evidence_kind"),
		StringVal(entry, "source_repo"),
		StringVal(entry, "service_name"),
		workflowName,
	}, "|")
}
