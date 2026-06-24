// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"fmt"
	"strings"
)

func appendHelmLogObservability(payload map[string]any, path string, root map[string]any) {
	ctx := grafanaSourceContext{
		path:        path,
		sourceKind:  "helm",
		lineNumber:  1,
		environment: environmentFromPath(path),
	}
	config := nestedMap(root, "config")
	appendPromtailLogRoutes(payload, config, ctx)
	appendOtelLogRoutes(payload, config, ctx)
	appendLokiGatewayLogRoute(payload, root, ctx)
}

func appendPromtailLogRoutes(payload map[string]any, config map[string]any, ctx grafanaSourceContext) {
	if len(config) == 0 {
		return
	}
	clientsValue, hasClients := config["clients"]
	if !hasClients {
		return
	}
	clients := asAnySlice(clientsValue)
	if !looksLikePromtailConfig(config, clients) {
		return
	}
	if len(clients) == 0 {
		appendObservabilityWarning(payload, ctx, "malformed_log_route", "rejected")
		return
	}
	for index, item := range clients {
		client, ok := item.(map[string]any)
		if !ok {
			appendObservabilityWarning(payload, ctx, "malformed_log_route", "rejected")
			continue
		}
		appendPromtailClientRoute(payload, config, client, index, ctx)
	}
}

func appendPromtailClientRoute(
	payload map[string]any,
	config map[string]any,
	client map[string]any,
	index int,
	ctx grafanaSourceContext,
) {
	row := baseObservabilityRow(ctx, fmt.Sprintf("log_route.promtail.client.%d", index))
	row["declaration_kind"] = "promtail_client_route"
	row["backend_kind"] = "loki"
	redacted := []string{}
	if url := cleanString(client["url"]); url != "" {
		row["route_destination_fingerprint"] = fingerprintValue(url)
		redacted = append(redacted, "clients.url")
	} else {
		row["outcome"] = "unresolved"
		appendObservabilityWarning(payload, ctx, "missing_log_route_endpoint", "unresolved")
	}
	if tenantID := cleanString(client["tenant_id"]); tenantID != "" {
		row["tenant_scope_state"] = "configured"
		row["tenant_id_fingerprint"] = fingerprintValue(tenantID)
		redacted = append(redacted, "clients.tenant_id")
	} else {
		row["tenant_scope_state"] = "unknown"
	}
	scrapes := asAnySlice(config["scrape_configs"])
	if len(scrapes) > 0 {
		row["scrape_config_count"] = len(scrapes)
	}
	if targetCount := promtailTargetCount(scrapes); targetCount > 0 {
		row["target_count"] = targetCount
		redacted = append(redacted, "scrape_configs.static_configs.targets")
	}
	if modes := promtailDiscoveryModes(scrapes); len(modes) > 0 {
		row["discovery_modes"] = strings.Join(modes, ",")
	}
	labelIdentity, labelKeys := promtailLabelIdentity(client, scrapes)
	if len(labelKeys) > 0 {
		row["label_keys"] = strings.Join(labelKeys, ",")
		row["label_identity_fingerprint"] = fingerprintObject(labelIdentity)
		redacted = append(redacted, "clients.external_labels", "scrape_configs.static_configs.labels")
		appendObservabilityWarning(payload, ctx, "high_cardinality_log_label_values_redacted", "rejected")
	}
	applyLogRouteRedaction(row, redacted)
	if cleanString(row["outcome"]) == "" {
		row["outcome"] = "exact"
	}
	appendBucketRow(payload, observabilityLogRouteBucket, row)
}

func appendOtelLogRoutes(payload map[string]any, config map[string]any, ctx grafanaSourceContext) {
	if len(config) == 0 {
		return
	}
	pipelines := nestedMap(config, "service", "pipelines")
	exporters := nestedMap(config, "exporters")
	receivers := nestedMap(config, "receivers")
	for _, pipelineName := range sortedMapKeysAny(pipelines) {
		if !strings.HasPrefix(pipelineName, "logs") {
			continue
		}
		pipeline, ok := pipelines[pipelineName].(map[string]any)
		if !ok {
			appendObservabilityWarning(payload, ctx, "malformed_log_route", "rejected")
			continue
		}
		exporterRefs := cleanStringList(asAnySlice(pipeline["exporters"]))
		receiverRefs := cleanStringList(asAnySlice(pipeline["receivers"]))
		processorRefs := cleanStringList(asAnySlice(pipeline["processors"]))
		row := baseObservabilityRow(ctx, "log_route.otel."+safeNamePart(pipelineName))
		row["declaration_kind"] = "otel_log_pipeline"
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
		row["backend_kind"] = logBackendKind(exporters, exporterRefs)
		if destination := firstExporterEndpoint(exporters, exporterRefs); destination != "" {
			row["route_destination_fingerprint"] = fingerprintValue(destination)
		}
		if otelTenantHeaderPresent(exporters, exporterRefs) {
			row["tenant_scope_state"] = "configured"
		} else {
			row["tenant_scope_state"] = "unknown"
		}
		applyLogRouteRedaction(row, otelLogRouteRedactedFields(exporters, receivers, exporterRefs, receiverRefs))
		row["outcome"] = "exact"
		appendBucketRow(payload, observabilityLogRouteBucket, row)
	}
}

func appendLokiGatewayLogRoute(payload map[string]any, root map[string]any, ctx grafanaSourceContext) {
	gateway := nestedMap(root, "gateway")
	if len(gateway) == 0 {
		gateway = nestedMap(root, "loki", "gateway")
	}
	if len(gateway) == 0 || !looksLikeLokiValues(root, gateway) {
		return
	}
	row := baseObservabilityRow(ctx, "log_route.loki.gateway")
	row["declaration_kind"] = "loki_gateway_route"
	row["backend_kind"] = "loki"
	row["tenant_scope_state"] = lokiTenantScopeState(root)
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
	appendBucketRow(payload, observabilityLogRouteBucket, row)
}

func appendLogRouteFromGrafanaDatasource(
	payload map[string]any,
	datasource map[string]any,
	ctx grafanaSourceContext,
	uid string,
	datasourceType string,
	nameFingerprint string,
) {
	if datasourceType != "loki" {
		return
	}
	row := baseObservabilityRow(ctx, "log_route.grafana_datasource."+firstNonEmpty(uid, nameFingerprint, ctx.configKey))
	row["declaration_kind"] = "grafana_loki_datasource"
	row["backend_kind"] = "loki"
	if uid != "" {
		row["datasource_uid"] = uid
	}
	if nameFingerprint != "" {
		row["datasource_name_fingerprint"] = nameFingerprint
	}
	if url := cleanString(datasource["url"]); url != "" {
		row["route_destination_fingerprint"] = fingerprintValue(url)
		row["outcome"] = "exact"
	} else {
		row["outcome"] = "unresolved"
		appendObservabilityWarning(payload, ctx, "missing_log_route_endpoint", "unresolved")
	}
	applyLogRouteRedaction(row, datasourceRedactedFields(datasource))
	appendBucketRow(payload, observabilityLogRouteBucket, row)
}

func looksLikePromtailConfig(config map[string]any, clients []any) bool {
	if len(asAnySlice(config["scrape_configs"])) > 0 {
		return true
	}
	if len(nestedMap(config, "snippets")) > 0 {
		return true
	}
	for _, item := range clients {
		client, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if strings.Contains(strings.ToLower(cleanString(client["url"])), "loki") {
			return true
		}
	}
	return false
}

func promtailTargetCount(scrapes []any) int {
	count := 0
	for _, item := range scrapes {
		scrape, ok := item.(map[string]any)
		if !ok {
			continue
		}
		count += staticConfigTargetCount(scrape)
	}
	return count
}

func promtailDiscoveryModes(scrapes []any) []string {
	var modes []string
	for _, item := range scrapes {
		scrape, ok := item.(map[string]any)
		if !ok {
			continue
		}
		modes = append(modes, scrapeConfigDiscoveryModes(scrape)...)
		if nested := nestedMap(scrape, "loki_push_api"); len(nested) > 0 {
			modes = append(modes, "push_api")
		}
	}
	return sortedUniqueStrings(modes)
}

func promtailLabelIdentity(client map[string]any, scrapes []any) (map[string]any, []string) {
	identity := map[string]any{}
	mergeMap(identity, nestedMap(client, "external_labels"))
	for _, item := range scrapes {
		scrape, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, staticItem := range asAnySlice(scrape["static_configs"]) {
			staticConfig, ok := staticItem.(map[string]any)
			if !ok {
				continue
			}
			mergeMap(identity, nestedMap(staticConfig, "labels"))
		}
		mergeMap(identity, nestedMap(scrape, "loki_push_api", "labels"))
	}
	if len(identity) == 0 {
		return nil, nil
	}
	return identity, sortedMapKeysAny(identity)
}

func mergeMap(dst map[string]any, src map[string]any) {
	for key, value := range src {
		if strings.TrimSpace(key) != "" {
			dst[key] = value
		}
	}
}

func componentKinds(refs []string) []string {
	kinds := make([]string, 0, len(refs))
	for _, ref := range refs {
		kinds = append(kinds, componentKind(ref))
	}
	return sortedUniqueStrings(kinds)
}

func componentKind(ref string) string {
	kind := strings.TrimSpace(ref)
	if index := strings.Index(kind, "/"); index >= 0 {
		kind = kind[:index]
	}
	return strings.TrimSpace(kind)
}

func logBackendKind(exporters map[string]any, refs []string) string {
	for _, ref := range refs {
		lower := strings.ToLower(ref)
		if strings.Contains(lower, "loki") {
			return "loki"
		}
		if exporter, ok := exporters[ref].(map[string]any); ok {
			if strings.Contains(strings.ToLower(cleanString(exporter["endpoint"])), "loki") {
				return "loki"
			}
		}
	}
	return "unknown"
}

func firstExporterEndpoint(exporters map[string]any, refs []string) string {
	for _, ref := range refs {
		exporter, ok := exporters[ref].(map[string]any)
		if !ok {
			continue
		}
		if endpoint := cleanString(exporter["endpoint"]); endpoint != "" {
			return endpoint
		}
	}
	return ""
}

func otelLogRouteRedactedFields(
	exporters map[string]any,
	receivers map[string]any,
	exporterRefs []string,
	receiverRefs []string,
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
	for _, ref := range receiverRefs {
		receiver, ok := receivers[ref].(map[string]any)
		if !ok {
			continue
		}
		if len(asAnySlice(receiver["include"])) > 0 {
			if kind := componentKind(ref); kind != "" {
				redacted = append(redacted, "receivers."+kind+".include")
			} else {
				redacted = append(redacted, "receivers.include")
			}
		}
	}
	return sortedUniqueStrings(redacted)
}

func looksLikeLokiValues(root map[string]any, gateway map[string]any) bool {
	if len(nestedMap(root, "loki")) > 0 {
		return true
	}
	snippet := strings.ToLower(cleanString(nestedMapValue(gateway, "nginx", "config", "serverSnippet")))
	return strings.Contains(snippet, "loki")
}

func lokiTenantScopeState(root map[string]any) string {
	if truthy(nestedMapValue(root, "loki", "auth_enabled")) || truthy(root["auth_enabled"]) {
		return "configured"
	}
	if limits := nestedMap(root, "loki", "limits_config"); len(limits) > 0 {
		return "configured"
	}
	return "unknown"
}

func applyLogRouteRedaction(row map[string]any, fields []string) {
	fields = sortedUniqueStrings(fields)
	if len(fields) == 0 {
		return
	}
	row["redacted_fields"] = strings.Join(fields, ",")
	row["redaction_state"] = "redacted"
}

func markDuplicateLogRouteRows(rows []map[string]any) {
	counts := map[string]int{}
	for _, row := range rows {
		key := logRouteIdentity(row)
		if key != "" {
			counts[key]++
		}
	}
	for _, row := range rows {
		if counts[logRouteIdentity(row)] > 1 {
			row["duplicate_log_route_identity"] = true
			row["outcome"] = "ambiguous"
		}
	}
}

func logRouteIdentity(row map[string]any) string {
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
		cleanString(row["label_identity_fingerprint"]),
		cleanString(row["exporter_refs"]),
	}, "|")
}
