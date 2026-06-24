// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"fmt"
	"strings"
)

func appendHelmMetricObservability(payload map[string]any, path string, root map[string]any) {
	ctx := grafanaSourceContext{
		path:        path,
		sourceKind:  "helm",
		lineNumber:  1,
		environment: environmentFromPath(path),
	}
	appendHelmPrometheusEvidence(payload, root, ctx)
	appendHelmServiceMonitorEvidence(payload, root, ctx)
	appendOtelPrometheusReceiverScrapes(payload, nestedMap(root, "config"), ctx)
	appendOtelMetricRoutes(payload, nestedMap(root, "config"), ctx)
	appendMimirMetricRoutes(payload, root, ctx)
}

func appendHelmPrometheusEvidence(payload map[string]any, root map[string]any, ctx grafanaSourceContext) {
	prometheusSpec := nestedMap(root, "prometheus", "prometheusSpec")
	if len(prometheusSpec) == 0 {
		prometheusSpec = nestedMap(root, "prometheusSpec")
	}
	if selector := nestedMap(prometheusSpec, "serviceMonitorSelector"); len(selector) > 0 {
		child := ctx
		child.declarationKind = "helm_prometheus_service_monitor_selector"
		appendHelmSelectorScrapeConfig(payload, "scrape_config.helm.service_monitor_selector", selector, child)
	}
	remoteWrites, _ := prometheusSpec["remoteWrite"].([]any)
	for index, item := range remoteWrites {
		remoteWrite, ok := item.(map[string]any)
		if !ok {
			continue
		}
		child := ctx
		child.declarationKind = "helm_prometheus_remote_write"
		appendPrometheusRemoteWriteRoute(payload, remoteWrite, index, child)
	}
}

func appendHelmServiceMonitorEvidence(payload map[string]any, root map[string]any, ctx grafanaSourceContext) {
	serviceMonitor := nestedMap(root, "serviceMonitor")
	if len(serviceMonitor) == 0 || !truthy(serviceMonitor["enabled"]) {
		return
	}
	child := ctx
	child.declarationKind = "helm_service_monitor_values"
	row := baseObservabilityRow(child, "scrape_config.helm.service_monitor")
	row["declaration_kind"] = child.declarationKind
	labels := nestedMap(serviceMonitor, "labels")
	if len(labels) == 0 {
		labels = nestedMap(serviceMonitor, "additionalLabels")
	}
	if keys := sortedMapKeysAny(labels); len(keys) > 0 {
		row["selector_label_keys"] = strings.Join(keys, ",")
		row["selector_identity_fingerprint"] = fingerprintObject(labels)
	}
	if releaseLabelPresent(labels) {
		row["release_label_present"] = true
	}
	if !releaseLabelPresent(labels) {
		row["outcome"] = "unresolved"
		appendObservabilityWarning(payload, child, "missing_discovery_label", "unresolved")
	} else {
		row["outcome"] = firstNonEmpty(outcomeForSelector(labels), "exact")
	}
	appendBucketRow(payload, observabilityScrapeBucket, row)
}

func appendHelmSelectorScrapeConfig(
	payload map[string]any,
	name string,
	selector map[string]any,
	ctx grafanaSourceContext,
) {
	row := baseObservabilityRow(ctx, name)
	row["declaration_kind"] = ctx.declarationKind
	applySelectorMetadata(row, selector)
	if selectorHasReleaseLabel(selector) {
		row["release_label_present"] = true
	}
	row["outcome"] = firstNonEmpty(outcomeForSelector(selector), "exact")
	appendBucketRow(payload, observabilityScrapeBucket, row)
}

func appendPrometheusRemoteWriteRoute(
	payload map[string]any,
	remoteWrite map[string]any,
	index int,
	ctx grafanaSourceContext,
) {
	row := baseObservabilityRow(ctx, fmt.Sprintf("metric_route.prometheus.remote_write.%d", index))
	row["declaration_kind"] = ctx.declarationKind
	row["backend_kind"] = metricBackendKind(cleanString(remoteWrite["url"]), nil)
	if fingerprint := fingerprintValue(cleanString(remoteWrite["url"])); fingerprint != "" {
		row["endpoint_fingerprint"] = fingerprint
	}
	redacted := []string{}
	if cleanString(remoteWrite["url"]) != "" {
		redacted = append(redacted, "url")
	}
	if headers := nestedMap(remoteWrite, "headers"); len(headers) > 0 {
		row["header_keys"] = strings.Join(sortedMapKeysAny(headers), ",")
		row["tenant_scope_state"] = tenantScopeState(headers)
		redacted = append(redacted, "headers")
	}
	if len(redacted) > 0 {
		row["redacted_fields"] = strings.Join(redacted, ",")
		row["redaction_state"] = "redacted"
	}
	row["outcome"] = "exact"
	appendBucketRow(payload, observabilityMetricRouteBucket, row)
}

func appendOtelPrometheusReceiverScrapes(payload map[string]any, config map[string]any, ctx grafanaSourceContext) {
	if len(config) == 0 {
		return
	}
	scrapes, _ := nestedMapValue(config, "receivers", "prometheus", "config", "scrape_configs").([]any)
	for index, item := range scrapes {
		scrapeConfig, ok := item.(map[string]any)
		if !ok {
			continue
		}
		child := ctx
		child.declarationKind = "otel_prometheus_receiver_scrape_config"
		row := baseObservabilityRow(child, fmt.Sprintf("scrape_config.otel.prometheus.%d", index))
		row["declaration_kind"] = child.declarationKind
		if fingerprint := fingerprintValue(cleanString(scrapeConfig["job_name"])); fingerprint != "" {
			row["job_name_fingerprint"] = fingerprint
		}
		targetCount := staticConfigTargetCount(scrapeConfig)
		if targetCount > 0 {
			row["target_count"] = targetCount
			row["redacted_fields"] = "scrape_configs.static_configs.targets"
			row["redaction_state"] = "redacted"
		}
		if modes := scrapeConfigDiscoveryModes(scrapeConfig); len(modes) > 0 {
			row["discovery_modes"] = strings.Join(modes, ",")
		}
		if targetCount == 0 && cleanString(scrapeConfig["job_name"]) == "" {
			row["outcome"] = "derived"
		} else {
			row["outcome"] = "exact"
		}
		appendBucketRow(payload, observabilityScrapeBucket, row)
	}
}

func appendOtelMetricRoutes(payload map[string]any, config map[string]any, ctx grafanaSourceContext) {
	if len(config) == 0 {
		return
	}
	pipelines := nestedMap(config, "service", "pipelines")
	exporters := nestedMap(config, "exporters")
	for _, pipelineName := range sortedMapKeysAny(pipelines) {
		if !strings.HasPrefix(pipelineName, "metrics") {
			continue
		}
		pipeline, ok := pipelines[pipelineName].(map[string]any)
		if !ok {
			continue
		}
		exporterRefs := cleanStringList(asAnySlice(pipeline["exporters"]))
		receiverRefs := cleanStringList(asAnySlice(pipeline["receivers"]))
		child := ctx
		child.declarationKind = "otel_metric_pipeline"
		row := baseObservabilityRow(child, "metric_route.otel."+safeNamePart(pipelineName))
		row["declaration_kind"] = child.declarationKind
		row["pipeline_name"] = pipelineName
		if len(receiverRefs) > 0 {
			row["receiver_refs"] = strings.Join(receiverRefs, ",")
		}
		if len(exporterRefs) > 0 {
			row["exporter_refs"] = strings.Join(exporterRefs, ",")
		}
		row["backend_kind"] = metricBackendKind("", exporterRefs)
		redacted := otelExporterRedactedFields(exporters, exporterRefs)
		if len(redacted) > 0 {
			row["redacted_fields"] = strings.Join(redacted, ",")
			row["redaction_state"] = "redacted"
		}
		if otelTenantHeaderPresent(exporters, exporterRefs) {
			row["tenant_scope_state"] = "configured"
		}
		row["outcome"] = "exact"
		appendBucketRow(payload, observabilityMetricRouteBucket, row)
	}
}

func appendMimirMetricRoutes(payload map[string]any, root map[string]any, ctx grafanaSourceContext) {
	gateway := nestedMap(root, "gateway")
	if len(gateway) == 0 {
		gateway = nestedMap(root, "mimir", "gateway")
	}
	if len(gateway) == 0 {
		return
	}
	if !looksLikeMimirValues(root, gateway) {
		return
	}
	row := baseObservabilityRow(ctx, "metric_route.mimir.gateway")
	row["declaration_kind"] = "mimir_gateway_route"
	row["backend_kind"] = "mimir"
	row["tenant_scope_state"] = "configured"
	if snippet := cleanString(nestedMapValue(gateway, "nginx", "config", "serverSnippet")); snippet != "" {
		row["redacted_fields"] = "serverSnippet"
		row["redaction_state"] = "redacted"
		row["route_config_fingerprint"] = fingerprintValue(snippet)
	}
	row["outcome"] = "exact"
	appendBucketRow(payload, observabilityMetricRouteBucket, row)
}

func looksLikeMimirValues(root map[string]any, gateway map[string]any) bool {
	if len(nestedMap(root, "mimir")) > 0 {
		return true
	}
	snippet := strings.ToLower(cleanString(nestedMapValue(gateway, "nginx", "config", "serverSnippet")))
	return strings.Contains(snippet, "mimir")
}

func metricBackendKind(endpoint string, exporterRefs []string) string {
	lowerEndpoint := strings.ToLower(endpoint)
	if strings.Contains(lowerEndpoint, "mimir") {
		return "mimir"
	}
	for _, ref := range exporterRefs {
		lower := strings.ToLower(ref)
		if strings.Contains(lower, "mimir") {
			return "mimir"
		}
		if strings.Contains(lower, "prometheus") {
			return "prometheus"
		}
	}
	return "unknown"
}

func otelExporterRedactedFields(exporters map[string]any, refs []string) []string {
	var redacted []string
	for _, ref := range refs {
		exporter, ok := exporters[ref].(map[string]any)
		if !ok {
			continue
		}
		if cleanString(exporter["endpoint"]) != "" {
			redacted = append(redacted, "endpoint")
		}
		if headers := nestedMap(exporter, "headers"); len(headers) > 0 {
			redacted = append(redacted, "headers")
		}
	}
	return sortedUniqueStrings(redacted)
}

func otelTenantHeaderPresent(exporters map[string]any, refs []string) bool {
	for _, ref := range refs {
		exporter, ok := exporters[ref].(map[string]any)
		if !ok {
			continue
		}
		if tenantScopeState(nestedMap(exporter, "headers")) == "configured" {
			return true
		}
	}
	return false
}

func tenantScopeState(headers map[string]any) string {
	for key := range headers {
		if strings.EqualFold(key, "X-Scope-OrgID") {
			return "configured"
		}
	}
	return "unknown"
}
