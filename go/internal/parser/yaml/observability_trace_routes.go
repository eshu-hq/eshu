// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"fmt"
	"strings"
)

func appendHelmTraceObservability(payload map[string]any, path string, root map[string]any) {
	ctx := grafanaSourceContext{
		path:        path,
		sourceKind:  "helm",
		lineNumber:  1,
		environment: environmentFromPath(path),
	}
	config := nestedMap(root, "config")
	appendOtelTraceRoutes(payload, config, ctx)
	appendTempoGatewayTraceRoute(payload, root, ctx)
}

func appendOtelTraceRoutes(payload map[string]any, config map[string]any, ctx grafanaSourceContext) {
	if len(config) == 0 {
		return
	}
	pipelines := nestedMap(config, "service", "pipelines")
	exporters := nestedMap(config, "exporters")
	processors := nestedMap(config, "processors")
	connectors := nestedMap(config, "connectors")
	for _, pipelineName := range sortedMapKeysAny(pipelines) {
		if !strings.HasPrefix(pipelineName, "traces") {
			continue
		}
		pipeline, ok := pipelines[pipelineName].(map[string]any)
		if !ok {
			appendObservabilityWarning(payload, ctx, "malformed_trace_route", "rejected")
			continue
		}
		appendOtelTraceRoute(payload, pipelineName, pipeline, exporters, processors, connectors, ctx)
	}
}

func appendOtelTraceRoute(
	payload map[string]any,
	pipelineName string,
	pipeline map[string]any,
	exporters map[string]any,
	processors map[string]any,
	connectors map[string]any,
	ctx grafanaSourceContext,
) {
	exporterRefs := cleanStringList(asAnySlice(pipeline["exporters"]))
	receiverRefs := cleanStringList(asAnySlice(pipeline["receivers"]))
	processorRefs := cleanStringList(asAnySlice(pipeline["processors"]))
	connectorRefs := traceConnectorRefs(connectors, receiverRefs, exporterRefs)
	row := baseObservabilityRow(ctx, "trace_route.otel."+safeNamePart(pipelineName))
	row["declaration_kind"] = "otel_trace_pipeline"
	row["pipeline_name"] = pipelineName
	if len(receiverRefs) > 0 {
		row["receiver_refs"] = strings.Join(receiverRefs, ",")
		row["receiver_kinds"] = strings.Join(componentKinds(receiverRefs), ",")
	}
	if len(processorRefs) > 0 {
		row["processor_refs"] = strings.Join(processorRefs, ",")
	}
	if len(exporterRefs) > 0 {
		row["exporter_refs"] = strings.Join(exporterRefs, ",")
	}
	if len(connectorRefs) > 0 {
		row["connector_refs"] = strings.Join(connectorRefs, ",")
	}
	backendKind := traceBackendKind(exporters, exporterRefs)
	destination := firstExporterEndpoint(exporters, exporterRefs)
	row["backend_kind"] = backendKind
	if destination != "" {
		row["route_destination_fingerprint"] = fingerprintValue(destination)
	}
	if otelTenantHeaderPresent(exporters, exporterRefs) {
		row["tenant_scope_state"] = "configured"
	} else {
		row["tenant_scope_state"] = "unknown"
	}
	applyLogRouteRedaction(row, otelTraceRouteRedactedFields(exporters, processors, exporterRefs, processorRefs))
	if len(exporterRefs) == 0 || (backendKind == "tempo" && destination == "") {
		row["outcome"] = "unresolved"
		appendObservabilityWarning(payload, ctx, "missing_trace_route_endpoint", "unresolved")
	} else {
		row["outcome"] = "exact"
	}
	appendBucketRow(payload, observabilityTraceRouteBucket, row)
}

func appendTempoGatewayTraceRoute(payload map[string]any, root map[string]any, ctx grafanaSourceContext) {
	gateway := nestedMap(root, "gateway")
	if len(gateway) == 0 {
		gateway = nestedMap(root, "tempo", "gateway")
	}
	if len(gateway) == 0 || !looksLikeTempoValues(root, gateway) {
		return
	}
	row := baseObservabilityRow(ctx, "trace_route.tempo.gateway")
	row["declaration_kind"] = "tempo_gateway_route"
	row["backend_kind"] = "tempo"
	row["tenant_scope_state"] = tempoTenantScopeState(root)
	redacted := []string{}
	if snippet := cleanString(nestedMapValue(gateway, "nginx", "config", "serverSnippet")); snippet != "" {
		row["route_config_fingerprint"] = fingerprintValue(snippet)
		redacted = append(redacted, "serverSnippet")
	}
	if hosts := asAnySlice(nestedMapValue(gateway, "ingress", "hosts")); len(hosts) > 0 {
		row["route_destination_fingerprint"] = fingerprintObject(hosts)
		redacted = append(redacted, "ingress.hosts")
	}
	applyLogRouteRedaction(row, redacted)
	row["outcome"] = "exact"
	appendBucketRow(payload, observabilityTraceRouteBucket, row)
}

func appendTraceRouteFromGrafanaDatasource(
	payload map[string]any,
	datasource map[string]any,
	ctx grafanaSourceContext,
	uid string,
	datasourceType string,
	nameFingerprint string,
) {
	if datasourceType != "tempo" {
		return
	}
	row := baseObservabilityRow(ctx, "trace_route.grafana_datasource."+firstNonEmpty(uid, nameFingerprint, ctx.configKey))
	row["declaration_kind"] = "grafana_tempo_datasource"
	row["backend_kind"] = "tempo"
	if uid != "" {
		row["datasource_uid"] = uid
	}
	if nameFingerprint != "" {
		row["datasource_name_fingerprint"] = nameFingerprint
	}
	redacted := datasourceRedactedFields(datasource)
	if url := cleanString(datasource["url"]); url != "" {
		row["route_destination_fingerprint"] = fingerprintValue(url)
		row["outcome"] = "exact"
	} else {
		row["outcome"] = "unresolved"
		appendObservabilityWarning(payload, ctx, "missing_trace_route_endpoint", "unresolved")
	}
	if jsonData := nestedMap(datasource, "jsonData"); len(jsonData) > 0 {
		redacted = append(redacted, appendTempoDatasourceLinks(payload, row, jsonData, ctx)...)
	}
	applyLogRouteRedaction(row, redacted)
	appendBucketRow(payload, observabilityTraceRouteBucket, row)
}

func appendTempoDatasourceLinks(
	payload map[string]any,
	row map[string]any,
	jsonData map[string]any,
	ctx grafanaSourceContext,
) []string {
	redacted := []string{}
	if tracesToLogs := nestedMap(jsonData, "tracesToLogsV2"); len(tracesToLogs) > 0 {
		if uid := cleanString(tracesToLogs["datasourceUid"]); uid != "" {
			row["traces_to_logs_datasource_uid"] = uid
		}
		keys, highCardinality, merged := mergeTraceTagIdentity(row, tracesToLogs["tags"])
		if merged {
			row["trace_tag_keys"] = strings.Join(keys, ",")
			redacted = append(redacted, "jsonData.tracesToLogsV2.tags")
		}
		if cleanString(tracesToLogs["query"]) != "" {
			redacted = append(redacted, "jsonData.tracesToLogsV2.query")
		}
		if highCardinality {
			appendObservabilityWarning(payload, ctx, "high_cardinality_trace_tag_values_redacted", "rejected")
		}
	}
	if tracesToMetrics := nestedMap(jsonData, "tracesToMetrics"); len(tracesToMetrics) > 0 {
		if uid := cleanString(tracesToMetrics["datasourceUid"]); uid != "" {
			row["traces_to_metrics_datasource_uid"] = uid
		}
		keys, highCardinality, merged := mergeTraceTagIdentity(row, tracesToMetrics["tags"])
		if merged {
			row["trace_tag_keys"] = strings.Join(keys, ",")
			redacted = append(redacted, "jsonData.tracesToMetrics.tags")
		}
		if len(asAnySlice(tracesToMetrics["queries"])) > 0 {
			redacted = append(redacted, "jsonData.tracesToMetrics.queries")
		}
		if highCardinality {
			appendObservabilityWarning(payload, ctx, "high_cardinality_trace_tag_values_redacted", "rejected")
		}
	}
	if serviceMap := nestedMap(jsonData, "serviceMap"); len(serviceMap) > 0 {
		if uid := cleanString(serviceMap["datasourceUid"]); uid != "" {
			row["service_map_datasource_uid"] = uid
		}
	}
	return sortedUniqueStrings(redacted)
}

func traceBackendKind(exporters map[string]any, refs []string) string {
	for _, ref := range refs {
		lower := strings.ToLower(ref)
		if strings.Contains(lower, "tempo") {
			return "tempo"
		}
		exporter, ok := exporters[ref].(map[string]any)
		if !ok {
			continue
		}
		if strings.Contains(strings.ToLower(cleanString(exporter["endpoint"])), "tempo") {
			return "tempo"
		}
	}
	return "unknown"
}

func traceConnectorRefs(connectors map[string]any, receiverRefs []string, exporterRefs []string) []string {
	if len(connectors) == 0 {
		return nil
	}
	var refs []string
	for _, ref := range append(receiverRefs, exporterRefs...) {
		if _, ok := connectors[ref]; ok {
			refs = append(refs, ref)
		}
	}
	return sortedUniqueStrings(refs)
}

func otelTraceRouteRedactedFields(
	exporters map[string]any,
	processors map[string]any,
	exporterRefs []string,
	processorRefs []string,
) []string {
	redacted := []string{}
	for _, ref := range exporterRefs {
		exporter, ok := exporters[ref].(map[string]any)
		if !ok {
			continue
		}
		if cleanString(exporter["endpoint"]) != "" {
			redacted = append(redacted, "exporters.endpoint")
		}
		if headers := nestedMap(exporter, "headers"); len(headers) > 0 {
			redacted = append(redacted, "exporters.headers")
		}
	}
	for _, ref := range processorRefs {
		processor, ok := processors[ref].(map[string]any)
		if !ok {
			continue
		}
		if len(asAnySlice(processor["actions"])) > 0 {
			kind := componentKind(ref)
			if kind == "" {
				kind = safeNamePart(ref)
			}
			redacted = append(redacted, fmt.Sprintf("processors.%s.actions", kind))
		}
	}
	return sortedUniqueStrings(redacted)
}

func looksLikeTempoValues(root map[string]any, gateway map[string]any) bool {
	if len(nestedMap(root, "tempo")) > 0 {
		return true
	}
	snippet := strings.ToLower(cleanString(nestedMapValue(gateway, "nginx", "config", "serverSnippet")))
	for _, value := range asAnySlice(nestedMapValue(gateway, "ingress", "hosts")) {
		if strings.Contains(strings.ToLower(cleanString(value)), "tempo") {
			return true
		}
	}
	return strings.Contains(snippet, "tempo")
}

func tempoTenantScopeState(root map[string]any) string {
	if truthy(nestedMapValue(root, "tempo", "structuredConfig", "multitenancy_enabled")) ||
		truthy(nestedMapValue(root, "tempo", "structuredConfig", "multitenancyEnabled")) ||
		truthy(nestedMapValue(root, "tempo", "multitenancy_enabled")) ||
		truthy(nestedMapValue(root, "tempo", "auth_enabled")) ||
		truthy(root["auth_enabled"]) {
		return "configured"
	}
	return "unknown"
}

func mergeTraceTagIdentity(row map[string]any, value any) ([]string, bool, bool) {
	keys, highCardinality := traceTagKeys(value)
	if len(keys) == 0 {
		return csvStrings(cleanString(row["trace_tag_keys"])), highCardinality, false
	}
	merged := sortedUniqueStrings(append(csvStrings(cleanString(row["trace_tag_keys"])), keys...))
	identity := map[string]any{}
	for _, key := range merged {
		identity[key] = "present"
	}
	row["trace_tag_identity_fingerprint"] = fingerprintObject(identity)
	return merged, highCardinality, true
}

func traceTagKeys(value any) ([]string, bool) {
	tags := asAnySlice(value)
	if len(tags) == 0 {
		return nil, false
	}
	identity := map[string]any{}
	highCardinality := false
	for _, item := range tags {
		switch typed := item.(type) {
		case map[string]any:
			key := cleanString(typed["key"])
			if key == "" {
				continue
			}
			identity[key] = "present"
			if cleanString(typed["value"]) != "" || highCardinalityTraceTagKey(key) {
				highCardinality = true
			}
		case string:
			if cleaned := strings.TrimSpace(typed); cleaned != "" {
				identity[cleaned] = "present"
				if highCardinalityTraceTagKey(cleaned) {
					highCardinality = true
				}
			}
		}
	}
	return sortedMapKeysAny(identity), highCardinality
}

func highCardinalityTraceTagKey(key string) bool {
	lower := strings.ToLower(key)
	return strings.Contains(lower, "trace") ||
		strings.Contains(lower, "span") ||
		strings.Contains(lower, "uid") ||
		strings.HasSuffix(lower, ".id") ||
		strings.HasSuffix(lower, "_id")
}

func markDuplicateTraceRouteRows(rows []map[string]any) {
	counts := map[string]int{}
	for _, row := range rows {
		key := traceRouteIdentity(row)
		if key != "" {
			counts[key]++
		}
	}
	for _, row := range rows {
		if counts[traceRouteIdentity(row)] > 1 {
			row["duplicate_trace_route_identity"] = true
			row["outcome"] = "ambiguous"
		}
	}
}

func traceRouteIdentity(row map[string]any) string {
	destination := firstNonEmpty(
		cleanString(row["route_destination_fingerprint"]),
		cleanString(row["datasource_uid"]),
		cleanString(row["pipeline_name"]),
	)
	if destination == "" {
		return ""
	}
	return strings.Join([]string{
		cleanString(row["declaration_kind"]),
		cleanString(row["backend_kind"]),
		destination,
		cleanString(row["tenant_id_fingerprint"]),
		cleanString(row["trace_tag_identity_fingerprint"]),
		cleanString(row["exporter_refs"]),
		cleanString(row["connector_refs"]),
	}, "|")
}

func csvStrings(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if cleaned := strings.TrimSpace(part); cleaned != "" {
			result = append(result, cleaned)
		}
	}
	return result
}
