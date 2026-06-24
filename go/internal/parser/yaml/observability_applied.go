// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import "strings"

var appliedObservabilityNames = []string{
	"grafana",
	"loki",
	"mimir",
	"opentelemetry",
	"otel",
	"prometheus",
	"promtail",
	"tempo",
}

func appendAppliedObservabilityFromDocument(
	payload map[string]any,
	path string,
	document map[string]any,
	metadata map[string]any,
	apiVersion string,
	kind string,
	lineNumber int,
) bool {
	if isArgoCDApplication(apiVersion, kind) {
		return appendAppliedArgoApplication(payload, path, document, metadata, lineNumber)
	}
	if !hasAppliedKubernetesState(document, metadata) {
		return false
	}
	class, ok := appliedKubernetesResourceClass(apiVersion, kind, metadata, document)
	if !ok {
		return false
	}
	appendAppliedKubernetesResource(payload, path, document, metadata, apiVersion, kind, lineNumber, class)
	return true
}

func appendAppliedArgoApplication(
	payload map[string]any,
	path string,
	document map[string]any,
	metadata map[string]any,
	lineNumber int,
) bool {
	status := nestedMap(document, "status")
	if len(status) == 0 {
		return false
	}
	spec := nestedMap(document, "spec")
	destination := nestedMap(spec, "destination")
	appName := cleanString(metadata["name"])
	appNamespace := cleanString(metadata["namespace"])
	clusterName := cleanString(destination["name"])
	clusterServerFingerprint := fingerprintValue(cleanString(destination["server"]))
	targetNamespace := cleanString(destination["namespace"])
	sync := nestedMap(status, "sync")
	health := nestedMap(status, "health")
	operation := nestedMap(status, "operationState")
	sourceRevision := cleanString(sync["revision"])
	conditionHints := argoConditionHints(status)

	syncRow := baseAppliedObservabilityRow(path, lineNumber, "argocd", "applied_sync_state."+firstNonEmpty(appName, "application"))
	syncRow["app_name"] = appName
	setIfNotEmpty(syncRow, "app_namespace", appNamespace)
	setIfNotEmpty(syncRow, "cluster_name", clusterName)
	setIfNotEmpty(syncRow, "cluster_server_fingerprint", clusterServerFingerprint)
	setIfNotEmpty(syncRow, "target_namespace", targetNamespace)
	setIfNotEmpty(syncRow, "source_revision", sourceRevision)
	setIfNotEmpty(syncRow, "sync_status", cleanString(sync["status"]))
	setIfNotEmpty(syncRow, "health_status", cleanString(health["status"]))
	setIfNotEmpty(syncRow, "operation_phase", cleanString(operation["phase"]))
	setIfNotEmpty(syncRow, "reconciled_at", cleanString(status["reconciledAt"]))
	if len(conditionHints) > 0 {
		syncRow["condition_hints"] = strings.Join(conditionHints, ",")
	}
	resources, _ := status["resources"].([]any)
	syncRow["resource_count"] = len(resources)
	syncRow["outcome"] = appliedSyncOutcome(syncRow, conditionHints)
	appendBucketRow(payload, observabilityAppliedSyncBucket, syncRow)

	for _, item := range resources {
		resource, ok := item.(map[string]any)
		if !ok {
			continue
		}
		class, ok := appliedStatusResourceClass(resource)
		if !ok {
			continue
		}
		row := baseAppliedObservabilityRow(path, lineNumber, "argocd", appliedResourceName(resource))
		row["app_name"] = appName
		setIfNotEmpty(row, "app_namespace", appNamespace)
		setIfNotEmpty(row, "cluster_name", clusterName)
		setIfNotEmpty(row, "cluster_server_fingerprint", clusterServerFingerprint)
		setIfNotEmpty(row, "target_namespace", targetNamespace)
		setIfNotEmpty(row, "source_revision", sourceRevision)
		setIfNotEmpty(row, "resource_group", cleanString(resource["group"]))
		setIfNotEmpty(row, "resource_kind", cleanString(resource["kind"]))
		setIfNotEmpty(row, "resource_namespace", cleanString(resource["namespace"]))
		setIfNotEmpty(row, "resource_name", cleanString(resource["name"]))
		row["observability_resource_class"] = class
		identity := appliedStatusResourceIdentity(resource)
		setIfNotEmpty(row, "resource_identity", identity)
		setIfNotEmpty(row, "resource_identity_fingerprint", fingerprintValue(identity))
		setIfNotEmpty(row, "sync_status", cleanString(resource["status"]))
		setIfNotEmpty(row, "health_status", appliedResourceHealthStatus(resource))
		row["outcome"] = appliedResourceOutcome(cleanString(resource["status"]), appliedResourceHealthStatus(resource))
		appendBucketRow(payload, observabilityAppliedBucket, row)
	}
	return true
}

func appendAppliedKubernetesResource(
	payload map[string]any,
	path string,
	document map[string]any,
	metadata map[string]any,
	apiVersion string,
	kind string,
	lineNumber int,
	class string,
) {
	name := cleanString(metadata["name"])
	namespace := cleanString(metadata["namespace"])
	row := baseAppliedObservabilityRow(path, lineNumber, "kubernetes", "applied_resource."+kind+"."+firstNonEmpty(name, "resource"))
	setIfNotEmpty(row, "resource_api_version", apiVersion)
	setIfNotEmpty(row, "resource_kind", kind)
	setIfNotEmpty(row, "resource_namespace", namespace)
	setIfNotEmpty(row, "resource_name", name)
	setIfNotEmpty(row, "resource_generation", cleanString(metadata["generation"]))
	setIfNotEmpty(row, "resource_uid_fingerprint", fingerprintValue(cleanString(metadata["uid"])))
	row["observability_resource_class"] = class
	identity := strings.Join([]string{apiVersion, kind, namespace, name}, "/")
	setIfNotEmpty(row, "resource_identity", identity)
	setIfNotEmpty(row, "resource_identity_fingerprint", fingerprintValue(identity))
	row["outcome"] = appliedKubernetesOutcome(document)
	appendBucketRow(payload, observabilityAppliedBucket, row)
}

func baseAppliedObservabilityRow(path string, lineNumber int, sourceKind string, name string) map[string]any {
	row := map[string]any{
		"name":            name,
		"line_number":     lineNumber,
		"path":            path,
		"lang":            "yaml",
		"source_class":    "applied",
		"source_kind":     sourceKind,
		"redaction_state": "none",
		"source_revision": "unknown",
	}
	if environment := environmentFromPath(path); environment != "" {
		row["environment"] = environment
		row["overlay"] = environment
	}
	return row
}

func hasAppliedKubernetesState(document map[string]any, metadata map[string]any) bool {
	if len(nestedMap(document, "status")) > 0 {
		return true
	}
	for _, key := range []string{"managedFields", "resourceVersion", "uid"} {
		if cleanString(metadata[key]) != "" {
			return true
		}
	}
	return false
}

func appliedKubernetesResourceClass(
	apiVersion string,
	kind string,
	metadata map[string]any,
	document map[string]any,
) (string, bool) {
	if class, ok := appliedResourceClass("", apiVersion, kind, cleanString(metadata["name"])); ok {
		return class, true
	}
	if strings.EqualFold(kind, "ConfigMap") && configMapLooksObservability(metadata, document) {
		return "configmap", true
	}
	return "", false
}

func appliedStatusResourceClass(resource map[string]any) (string, bool) {
	group := cleanString(resource["group"])
	kind := cleanString(resource["kind"])
	name := cleanString(resource["name"])
	return appliedResourceClass(group, "", kind, name)
}

func appliedResourceClass(group string, apiVersion string, kind string, name string) (string, bool) {
	lowerKind := strings.ToLower(kind)
	switch {
	case strings.Contains(lowerKind, "grafanafolder"):
		return "folder", true
	case strings.Contains(lowerKind, "grafanadashboard"):
		return "dashboard", true
	case strings.Contains(lowerKind, "grafanadatasource"):
		return "datasource", true
	case strings.EqualFold(kind, "ServiceMonitor"), strings.EqualFold(kind, "PodMonitor"), strings.EqualFold(kind, "ScrapeConfig"):
		return "scrape_config", true
	case strings.EqualFold(kind, "PrometheusRule"):
		return "metric_rule", true
	case strings.EqualFold(kind, "AlertmanagerConfig"):
		return "alert_rule", true
	case strings.EqualFold(kind, "OpenTelemetryCollector"):
		return "collector_pipeline", true
	case isObservabilityWorkloadKind(kind) && appliedNameLooksObservability(name):
		return "deployment", true
	case strings.EqualFold(kind, "Service") && appliedNameLooksObservability(name):
		return "service", true
	case strings.Contains(strings.ToLower(group), "grafana") || strings.Contains(strings.ToLower(apiVersion), "grafana"):
		return "grafana_resource", true
	}
	return "", false
}

func configMapLooksObservability(metadata map[string]any, document map[string]any) bool {
	if appliedNameLooksObservability(cleanString(metadata["name"])) {
		return true
	}
	for _, key := range sortedMapKeysAny(nestedMap(document, "data")) {
		if appliedNameLooksObservability(key) || strings.HasSuffix(strings.ToLower(key), ".json") {
			return true
		}
	}
	return false
}

func isObservabilityWorkloadKind(kind string) bool {
	switch strings.ToLower(kind) {
	case "daemonset", "deployment", "statefulset":
		return true
	default:
		return false
	}
}

func appliedNameLooksObservability(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range appliedObservabilityNames {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func appliedSyncOutcome(row map[string]any, conditionHints []string) string {
	if containsAppliedHint(conditionHints, "permission") {
		return "permission_hidden"
	}
	status := strings.ToLower(cleanString(row["sync_status"]))
	health := strings.ToLower(cleanString(row["health_status"]))
	phase := strings.ToLower(cleanString(row["operation_phase"]))
	switch {
	case status == "unknown" || health == "unknown":
		return "unresolved"
	case strings.Contains(status, "outofsync"), strings.Contains(health, "degraded"), phase == "failed", phase == "error":
		return "drifted"
	case strings.Contains(health, "stale"):
		return "stale"
	case status == "synced" && (health == "" || health == "healthy") && (phase == "" || phase == "succeeded"):
		return "exact"
	default:
		return "derived"
	}
}

func appliedResourceOutcome(status string, health string) string {
	lowerStatus := strings.ToLower(status)
	lowerHealth := strings.ToLower(health)
	switch {
	case strings.Contains(lowerHealth, "stale"):
		return "stale"
	case strings.Contains(lowerStatus, "pruned"):
		return "pruned"
	case strings.Contains(lowerStatus, "missing"):
		return "missing"
	case strings.Contains(lowerStatus, "outofsync") || strings.Contains(lowerHealth, "degraded"):
		return "drifted"
	case lowerStatus == "synced" || lowerHealth == "healthy":
		return "exact"
	default:
		return "derived"
	}
}

func appliedKubernetesOutcome(document map[string]any) string {
	status := nestedMap(document, "status")
	if len(status) == 0 {
		return "derived"
	}
	conditions, _ := status["conditions"].([]any)
	if appliedConditionsHidden(conditions) {
		return "permission_hidden"
	}
	if appliedConditionsContain(conditions, "stale") {
		return "stale"
	}
	for _, item := range conditions {
		condition, ok := item.(map[string]any)
		if !ok || !strings.EqualFold(cleanString(condition["type"]), "Ready") {
			continue
		}
		if strings.EqualFold(cleanString(condition["status"]), "True") {
			return "exact"
		}
		if strings.EqualFold(cleanString(condition["status"]), "False") {
			return "drifted"
		}
	}
	return "exact"
}

func argoConditionHints(status map[string]any) []string {
	conditions, _ := status["conditions"].([]any)
	var hints []string
	for _, item := range conditions {
		condition, ok := item.(map[string]any)
		if !ok {
			continue
		}
		joined := strings.ToLower(strings.Join([]string{
			cleanString(condition["type"]),
			cleanString(condition["reason"]),
			cleanString(condition["message"]),
		}, " "))
		switch {
		case strings.Contains(joined, "permission") || strings.Contains(joined, "forbidden") || strings.Contains(joined, "unauthorized"):
			hints = append(hints, "permission")
		case strings.Contains(joined, "stale"):
			hints = append(hints, "stale")
		}
	}
	return sortedUniqueStrings(hints)
}

func containsAppliedHint(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

func appliedConditionsContain(conditions []any, needle string) bool {
	for _, item := range conditions {
		condition, ok := item.(map[string]any)
		if !ok {
			continue
		}
		joined := strings.ToLower(strings.Join([]string{
			cleanString(condition["type"]),
			cleanString(condition["reason"]),
			cleanString(condition["message"]),
		}, " "))
		if strings.Contains(joined, needle) {
			return true
		}
	}
	return false
}

func appliedConditionsHidden(conditions []any) bool {
	for _, needle := range []string{"permission", "forbidden", "unauthorized"} {
		if appliedConditionsContain(conditions, needle) {
			return true
		}
	}
	return false
}

func appliedResourceHealthStatus(resource map[string]any) string {
	return cleanString(nestedMap(resource, "health")["status"])
}

func appliedResourceName(resource map[string]any) string {
	return "applied_resource." + firstNonEmpty(cleanString(resource["kind"]), "resource") + "." + firstNonEmpty(cleanString(resource["name"]), "unknown")
}

func appliedStatusResourceIdentity(resource map[string]any) string {
	return strings.Join([]string{
		cleanString(resource["group"]),
		cleanString(resource["kind"]),
		cleanString(resource["namespace"]),
		cleanString(resource["name"]),
	}, "/")
}

func setIfNotEmpty(row map[string]any, key string, value string) {
	if strings.TrimSpace(value) != "" {
		row[key] = strings.TrimSpace(value)
	}
}
