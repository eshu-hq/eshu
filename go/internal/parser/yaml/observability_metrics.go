// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import "strings"

func appendPrometheusObservabilityFromDocument(
	payload map[string]any,
	document map[string]any,
	ctx grafanaSourceContext,
) {
	switch {
	case isPrometheusMonitorResource(ctx.resourceAPI, ctx.resourceKind, "ServiceMonitor"):
		ctx.declarationKind = "prometheus_service_monitor"
		appendPrometheusMonitorScrapeConfig(payload, nestedMap(document, "spec"), ctx, "endpoints")
	case isPrometheusMonitorResource(ctx.resourceAPI, ctx.resourceKind, "PodMonitor"):
		ctx.declarationKind = "prometheus_pod_monitor"
		appendPrometheusMonitorScrapeConfig(payload, nestedMap(document, "spec"), ctx, "podMetricsEndpoints")
	case isPrometheusMonitorResource(ctx.resourceAPI, ctx.resourceKind, "ScrapeConfig"):
		ctx.declarationKind = "prometheus_scrape_config"
		appendPrometheusScrapeConfig(payload, nestedMap(document, "spec"), ctx)
	case isPrometheusMonitorResource(ctx.resourceAPI, ctx.resourceKind, "PrometheusRule"):
		ctx.declarationKind = "prometheus_rule"
		appendPrometheusRuleRows(payload, nestedMap(document, "spec"), ctx)
	}
}

func appendPrometheusMonitorScrapeConfig(
	payload map[string]any,
	spec map[string]any,
	ctx grafanaSourceContext,
	endpointsKey string,
) {
	row := baseObservabilityRow(ctx, "scrape_config."+ctx.resourceKind+"."+firstNonEmpty(ctx.resourceName, "resource"))
	row["declaration_kind"] = ctx.declarationKind
	selector := nestedMap(spec, "selector")
	applySelectorMetadata(row, selector)
	if releaseLabelPresent(ctx.labels) || selectorHasReleaseLabel(selector) {
		row["release_label_present"] = true
	}
	row["namespace_selector_state"] = namespaceSelectorState(nestedMap(spec, "namespaceSelector"))
	endpoints, _ := spec[endpointsKey].([]any)
	row["endpoint_count"] = len(endpoints)
	if ports := endpointPortNames(endpoints); len(ports) > 0 {
		row["port_names"] = strings.Join(ports, ",")
	}
	if len(selector) == 0 {
		row["outcome"] = "unresolved"
		appendObservabilityWarning(payload, ctx, "missing_selector", "unresolved")
	} else {
		row["outcome"] = "exact"
	}
	appendBucketRow(payload, observabilityScrapeBucket, row)
}

func appendPrometheusScrapeConfig(payload map[string]any, spec map[string]any, ctx grafanaSourceContext) {
	row := baseObservabilityRow(ctx, "scrape_config.ScrapeConfig."+firstNonEmpty(ctx.resourceName, "resource"))
	row["declaration_kind"] = ctx.declarationKind
	if fingerprint := fingerprintValue(cleanString(spec["jobName"])); fingerprint != "" {
		row["job_name_fingerprint"] = fingerprint
	}
	if modes := scrapeConfigDiscoveryModes(spec); len(modes) > 0 {
		row["discovery_modes"] = strings.Join(modes, ",")
	}
	targetCount := staticConfigTargetCount(spec)
	if targetCount > 0 {
		row["target_count"] = targetCount
		row["redacted_fields"] = "staticConfigs.targets"
		row["redaction_state"] = "redacted"
	}
	if cleanString(spec["jobName"]) != "" {
		if current := cleanString(row["redacted_fields"]); current != "" {
			row["redacted_fields"] = current + ",jobName"
		} else {
			row["redacted_fields"] = "jobName"
		}
		row["redaction_state"] = "redacted"
	}
	if targetCount == 0 && cleanString(spec["jobName"]) == "" {
		row["outcome"] = "derived"
	} else {
		row["outcome"] = "exact"
	}
	appendBucketRow(payload, observabilityScrapeBucket, row)
}

func appendPrometheusRuleRows(payload map[string]any, spec map[string]any, ctx grafanaSourceContext) {
	groups, ok := spec["groups"].([]any)
	if !ok {
		appendObservabilityWarning(payload, ctx, "malformed_metric_rule", "rejected")
		return
	}
	for _, groupItem := range groups {
		group, ok := groupItem.(map[string]any)
		if !ok {
			appendObservabilityWarning(payload, ctx, "malformed_metric_rule", "rejected")
			continue
		}
		rules, ok := group["rules"].([]any)
		if !ok {
			appendObservabilityWarning(payload, ctx, "malformed_metric_rule", "rejected")
			continue
		}
		for _, ruleItem := range rules {
			rule, ok := ruleItem.(map[string]any)
			if !ok {
				appendObservabilityWarning(payload, ctx, "malformed_metric_rule", "rejected")
				continue
			}
			appendPrometheusRuleRow(payload, group, rule, ctx)
		}
	}
}

func appendPrometheusRuleRow(
	payload map[string]any,
	group map[string]any,
	rule map[string]any,
	ctx grafanaSourceContext,
) {
	groupName := cleanString(group["name"])
	alertFingerprint := fingerprintValue(cleanString(rule["alert"]))
	recordFingerprint := fingerprintValue(cleanString(rule["record"]))
	ruleKind := "unknown"
	ruleIdentity := ""
	if alertFingerprint != "" {
		ruleKind = "alert"
		ruleIdentity = alertFingerprint
	} else if recordFingerprint != "" {
		ruleKind = "record"
		ruleIdentity = recordFingerprint
	}
	row := baseObservabilityRow(ctx, "metric_rule."+firstNonEmpty(safeNamePart(groupName), "group")+"."+firstNonEmpty(ruleIdentity, "rule"))
	row["declaration_kind"] = firstNonEmpty(ctx.declarationKind, "prometheus_rule")
	row["rule_kind"] = ruleKind
	if groupName != "" {
		row["rule_group"] = groupName
	}
	if alertFingerprint != "" {
		row["alert_rule_name_fingerprint"] = alertFingerprint
	}
	if recordFingerprint != "" {
		row["record_rule_name_fingerprint"] = recordFingerprint
	}
	if cleanString(rule["expr"]) != "" {
		row["redacted_fields"] = "expr"
		row["redaction_state"] = "redacted"
	}
	if ruleKind == "unknown" {
		row["outcome"] = "derived"
	} else {
		row["outcome"] = "exact"
	}
	appendBucketRow(payload, observabilityMetricRuleBucket, row)
}

func markDuplicateMetricRuleRows(rows []map[string]any) {
	counts := map[string]int{}
	for _, row := range rows {
		key := metricRuleIdentity(row)
		if key != "" {
			counts[key]++
		}
	}
	for _, row := range rows {
		if counts[metricRuleIdentity(row)] > 1 {
			row["duplicate_metric_rule_identity"] = true
			row["outcome"] = "ambiguous"
		}
	}
}

func metricRuleIdentity(row map[string]any) string {
	ruleIdentity := firstNonEmpty(
		cleanString(row["alert_rule_name_fingerprint"]),
		cleanString(row["record_rule_name_fingerprint"]),
	)
	if ruleIdentity == "" {
		return ""
	}
	return strings.Join([]string{
		cleanString(row["rule_group"]),
		cleanString(row["rule_kind"]),
		ruleIdentity,
	}, "|")
}

func applySelectorMetadata(row map[string]any, selector map[string]any) {
	if len(selector) == 0 {
		return
	}
	keys := selectorLabelKeys(selector)
	if len(keys) > 0 {
		row["selector_label_keys"] = strings.Join(keys, ",")
	}
	if fingerprint := fingerprintObject(selector); fingerprint != "" {
		row["selector_identity_fingerprint"] = fingerprint
	}
}

func selectorLabelKeys(selector map[string]any) []string {
	var keys []string
	if matchLabels := nestedMap(selector, "matchLabels"); len(matchLabels) > 0 {
		keys = append(keys, sortedMapKeysAny(matchLabels)...)
	}
	if expressions, ok := selector["matchExpressions"].([]any); ok {
		for _, item := range expressions {
			expression, ok := item.(map[string]any)
			if !ok {
				continue
			}
			keys = append(keys, cleanString(expression["key"]))
		}
	}
	if len(keys) == 0 {
		_, hasMatchLabels := selector["matchLabels"]
		_, hasMatchExpressions := selector["matchExpressions"]
		if !hasMatchLabels && !hasMatchExpressions {
			keys = append(keys, sortedMapKeysAny(selector)...)
		}
	}
	return sortedUniqueStrings(keys)
}

func selectorHasReleaseLabel(selector map[string]any) bool {
	matchLabels := nestedMap(selector, "matchLabels")
	return releaseLabelPresent(matchLabels)
}

func releaseLabelPresent(labels map[string]any) bool {
	for key := range labels {
		if strings.EqualFold(key, "release") {
			return true
		}
	}
	return false
}

func namespaceSelectorState(selector map[string]any) string {
	if len(selector) == 0 {
		return "current"
	}
	if truthy(selector["any"]) {
		return "all"
	}
	if names := asAnySlice(selector["matchNames"]); len(names) > 0 {
		return "named"
	}
	return "current"
}

func endpointPortNames(endpoints []any) []string {
	var ports []string
	for _, item := range endpoints {
		endpoint, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ports = append(ports, cleanString(endpoint["port"]))
		ports = append(ports, cleanString(endpoint["targetPort"]))
	}
	return sortedUniqueStrings(ports)
}

func scrapeConfigDiscoveryModes(spec map[string]any) []string {
	candidates := map[string]string{
		"staticConfigs":         "static",
		"static_configs":        "static",
		"fileSDConfigs":         "file_sd",
		"file_sd_configs":       "file_sd",
		"httpSDConfigs":         "http_sd",
		"http_sd_configs":       "http_sd",
		"kubernetesSDConfigs":   "kubernetes_sd",
		"kubernetes_sd_configs": "kubernetes_sd",
		"consulSDConfigs":       "consul_sd",
		"consul_sd_configs":     "consul_sd",
	}
	var modes []string
	for key, mode := range candidates {
		if len(asAnySlice(spec[key])) > 0 {
			modes = append(modes, mode)
		}
	}
	return sortedUniqueStrings(modes)
}

func staticConfigTargetCount(spec map[string]any) int {
	count := 0
	for _, key := range []string{"staticConfigs", "static_configs"} {
		for _, item := range asAnySlice(spec[key]) {
			staticConfig, ok := item.(map[string]any)
			if !ok {
				continue
			}
			count += len(asAnySlice(staticConfig["targets"]))
		}
	}
	return count
}

func outcomeForSelector(selector map[string]any) string {
	if len(selectorLabelKeys(selector)) == 0 && !releaseLabelPresent(selector) {
		return "unresolved"
	}
	return "exact"
}

func truthy(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func asAnySlice(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []string:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	default:
		return nil
	}
}

func isPrometheusMonitorResource(apiVersion string, kind string, expectedKind string) bool {
	return strings.Contains(strings.ToLower(apiVersion), "monitoring.coreos.com") &&
		strings.EqualFold(kind, expectedKind)
}
