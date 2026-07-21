// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

const (
	observabilityFolderBucket      = "observability_declared_folders"
	observabilityDashboardBucket   = "observability_declared_dashboards"
	observabilityDatasourceBucket  = "observability_declared_datasources"
	observabilityAlertRuleBucket   = "observability_declared_alert_rules"
	observabilityScrapeBucket      = "observability_declared_scrape_configs"
	observabilityMetricRuleBucket  = "observability_declared_metric_rules"
	observabilityMetricRouteBucket = "observability_declared_metric_routes"
	observabilityLogRouteBucket    = "observability_declared_log_routes"
	observabilityTraceRouteBucket  = "observability_declared_trace_routes"
	observabilityAppliedBucket     = "observability_applied_resources"
	observabilityAppliedSyncBucket = "observability_applied_sync_states"
	observabilityWarningBucket     = "observability_coverage_warnings"
)

var observabilityBuckets = []string{
	observabilityFolderBucket,
	observabilityDashboardBucket,
	observabilityDatasourceBucket,
	observabilityAlertRuleBucket,
	observabilityScrapeBucket,
	observabilityMetricRuleBucket,
	observabilityMetricRouteBucket,
	observabilityLogRouteBucket,
	observabilityTraceRouteBucket,
	observabilityAppliedBucket,
	observabilityAppliedSyncBucket,
	observabilityWarningBucket,
}

var supportedGrafanaDatasourceTypes = map[string]struct{}{
	"cloudwatch": {},
	"loki":       {},
	"mimir":      {},
	"prometheus": {},
	"tempo":      {},
}

type grafanaSourceContext struct {
	path            string
	sourceKind      string
	declarationKind string
	lineNumber      int
	namespace       string
	resourceKind    string
	resourceName    string
	resourceAPI     string
	configKey       string
	folder          string
	folderUID       string
	environment     string
	labels          map[string]any
}

func initObservabilityBuckets(payload map[string]any) {
	for _, bucket := range observabilityBuckets {
		payload[bucket] = []map[string]any{}
	}
}

func sortObservabilityBuckets(payload map[string]any) {
	markDuplicateDashboardRows(payload[observabilityDashboardBucket].([]map[string]any))
	markDuplicateMetricRuleRows(payload[observabilityMetricRuleBucket].([]map[string]any))
	markDuplicateLogRouteRows(payload[observabilityLogRouteBucket].([]map[string]any))
	markDuplicateTraceRouteRows(payload[observabilityTraceRouteBucket].([]map[string]any))
	for _, bucket := range observabilityBuckets {
		shared.SortNamedBucket(payload, bucket)
	}
}

func appendGrafanaObservabilityFromDocument(
	payload map[string]any,
	path string,
	document map[string]any,
	metadata map[string]any,
	apiVersion string,
	kind string,
	lineNumber int,
) {
	name := cleanYAMLString(metadata["name"])
	namespace := cleanYAMLString(metadata["namespace"])
	ctx := grafanaSourceContext{
		path:         path,
		sourceKind:   "kubernetes",
		lineNumber:   lineNumber,
		namespace:    namespace,
		resourceKind: kind,
		resourceName: name,
		resourceAPI:  apiVersion,
		environment:  environmentFromPath(path),
	}
	if labels, ok := metadata["labels"].(map[string]any); ok {
		ctx.labels = labels
	}

	switch {
	case isGrafanaFolderResource(apiVersion, kind):
		ctx.declarationKind = "grafana_folder_resource"
		appendFolderFromGrafanaResource(payload, document, ctx)
	case isGrafanaDashboardResource(apiVersion, kind):
		ctx.declarationKind = "grafana_dashboard_resource"
		appendDashboardFromGrafanaResource(payload, document, ctx)
	case strings.EqualFold(kind, "ConfigMap"):
		ctx.declarationKind = "grafana_configmap"
		appendGrafanaFromConfigMap(payload, document, ctx)
	case strings.Contains(strings.ToLower(kind), "grafanadatasource"):
		ctx.declarationKind = "grafana_datasource_resource"
		appendDatasourcesFromObject(payload, nestedMap(document, "spec"), ctx)
	}
	appendPrometheusObservabilityFromDocument(payload, document, ctx)
}

// appendHelmGrafanaObservability extracts Grafana provisioning evidence
// (datasources, dashboards, alerting) and metric/log/trace observability
// evidence from one Helm values file. documents is the caller's
// already-decoded DecodeDocuments result for the file source; callers share
// one decode between this and parseHelmValues rather than each decoding the
// source independently (issue #4847). An empty documents slice — whether
// from a decode error or an empty file — is a no-op, matching the prior
// per-call swallow-and-return behavior.
func appendHelmGrafanaObservability(payload map[string]any, path string, documents []any) {
	if len(documents) == 0 {
		return
	}
	root, ok := documents[0].(map[string]any)
	if !ok {
		return
	}
	delete(root, "__eshu_line_number")
	grafana := nestedMap(root, "grafana")
	if len(grafana) > 0 {
		ctx := grafanaSourceContext{
			path:            path,
			sourceKind:      "helm",
			declarationKind: "helm_values",
			lineNumber:      1,
			environment:     environmentFromPath(path),
		}
		walkHelmGrafanaDatasources(payload, nestedMap(grafana, "datasources"), ctx)
		walkHelmGrafanaDashboards(payload, nestedMap(grafana, "dashboards"), ctx)
		walkGrafanaAlertDocuments(payload, nestedMap(grafana, "alerting"), ctx)
	}
	appendHelmMetricObservability(payload, path, root)
	appendHelmLogObservability(payload, path, root)
	appendHelmTraceObservability(payload, path, root)
}

func appendDashboardFromGrafanaResource(payload map[string]any, document map[string]any, ctx grafanaSourceContext) {
	spec := nestedMap(document, "spec")
	ctx.folder = cleanString(spec["folder"])
	ctx.folderUID = cleanString(spec["folderUID"])
	for _, key := range []string{"json", "dashboardJson", "model"} {
		if raw := cleanString(spec[key]); raw != "" {
			dashboard, err := decodeJSONObject(raw)
			if err != nil {
				appendObservabilityWarning(payload, ctx, "malformed_dashboard_json", "rejected")
				return
			}
			appendDashboardRow(payload, dashboard, ctx)
			return
		}
	}
	if dashboard := nestedMap(spec, "dashboard"); len(dashboard) > 0 {
		appendDashboardRow(payload, dashboard, ctx)
		return
	}
	appendObservabilityWarning(payload, ctx, "missing_dashboard_json", "rejected")
}

func appendGrafanaFromConfigMap(payload map[string]any, document map[string]any, ctx grafanaSourceContext) {
	data, ok := document["data"].(map[string]any)
	if !ok {
		return
	}
	for _, key := range sortedMapKeysAny(data) {
		value := cleanString(data[key])
		if value == "" {
			continue
		}
		child := ctx
		child.configKey = key
		child.lineNumber = ctx.lineNumber
		lower := strings.ToLower(key)
		switch {
		case strings.HasSuffix(lower, ".json"):
			dashboard, err := decodeJSONObject(value)
			if err != nil {
				appendObservabilityWarning(payload, child, "malformed_dashboard_json", "rejected")
				continue
			}
			if looksLikeGrafanaDashboard(dashboard) {
				child.declarationKind = "configmap_dashboard_json"
				appendDashboardRow(payload, dashboard, child)
			}
		case strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml"):
			appendGrafanaProvisioningYAML(payload, value, child)
		}
	}
}

func appendGrafanaProvisioningYAML(payload map[string]any, value string, ctx grafanaSourceContext) {
	documents, err := DecodeDocuments(value)
	if err != nil {
		appendObservabilityWarning(payload, ctx, "malformed_provisioning_yaml", "rejected")
		return
	}
	for _, document := range documents {
		object, ok := document.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := object["datasources"]; ok {
			child := ctx
			child.declarationKind = "grafana_datasource_provisioning"
			appendDatasourcesFromObject(payload, object, child)
		}
		if _, ok := object["providers"]; ok {
			child := ctx
			child.declarationKind = "grafana_folder_provisioning"
			appendFoldersFromProvisioning(payload, object, child)
		}
		if _, ok := object["folders"]; ok {
			child := ctx
			child.declarationKind = "grafana_folder_provisioning"
			appendFoldersFromProvisioning(payload, object, child)
		}
		if _, ok := object["groups"]; ok {
			child := ctx
			child.declarationKind = "grafana_alert_provisioning"
			walkGrafanaAlertDocuments(payload, object, child)
		}
	}
}

func walkHelmGrafanaDatasources(payload map[string]any, value map[string]any, ctx grafanaSourceContext) {
	if len(value) == 0 {
		return
	}
	for _, key := range sortedMapKeysAny(value) {
		item := value[key]
		child := ctx
		child.configKey = key
		child.declarationKind = "helm_datasource_values"
		switch typed := item.(type) {
		case map[string]any:
			if _, ok := typed["datasources"]; ok {
				appendDatasourcesFromObject(payload, typed, child)
			} else {
				walkHelmGrafanaDatasources(payload, typed, child)
			}
		case string:
			appendGrafanaProvisioningYAML(payload, typed, child)
		}
	}
}

func walkHelmGrafanaDashboards(payload map[string]any, value map[string]any, ctx grafanaSourceContext) {
	if len(value) == 0 {
		return
	}
	for _, key := range sortedMapKeysAny(value) {
		item := value[key]
		child := ctx
		child.configKey = firstNonEmpty(ctx.configKey, key)
		child.declarationKind = "helm_dashboard_values"
		switch typed := item.(type) {
		case map[string]any:
			if raw := cleanString(typed["json"]); raw != "" {
				dashboard, err := decodeJSONObject(raw)
				if err != nil {
					appendObservabilityWarning(payload, child, "malformed_dashboard_json", "rejected")
					continue
				}
				appendDashboardRow(payload, dashboard, child)
				continue
			}
			walkHelmGrafanaDashboards(payload, typed, child)
		case string:
			if dashboard, err := decodeJSONObject(typed); err == nil {
				appendDashboardRow(payload, dashboard, child)
			}
		}
	}
}

func baseObservabilityRow(ctx grafanaSourceContext, name string) map[string]any {
	row := map[string]any{
		"name":            name,
		"line_number":     ctx.lineNumber,
		"path":            ctx.path,
		"lang":            "yaml",
		"source_class":    "declared",
		"source_kind":     ctx.sourceKind,
		"redaction_state": "none",
		"source_revision": "unknown",
	}
	if ctx.namespace != "" {
		row["namespace"] = ctx.namespace
	}
	if ctx.resourceKind != "" {
		row["resource_kind"] = ctx.resourceKind
	}
	if ctx.resourceName != "" {
		row["resource_name"] = ctx.resourceName
	}
	if ctx.resourceAPI != "" {
		row["resource_api_version"] = ctx.resourceAPI
	}
	if ctx.configKey != "" {
		row["config_key"] = ctx.configKey
	}
	if ctx.environment != "" {
		row["environment"] = ctx.environment
		row["overlay"] = ctx.environment
	}
	return row
}

func appendObservabilityWarning(payload map[string]any, ctx grafanaSourceContext, kind string, outcome string) {
	row := baseObservabilityRow(ctx, "warning."+kind+"."+firstNonEmpty(ctx.resourceName, ctx.configKey, "source"))
	row["warning_kind"] = kind
	row["outcome"] = outcome
	appendBucketRow(payload, observabilityWarningBucket, row)
}

func appendBucketRow(payload map[string]any, bucket string, row map[string]any) {
	payload[bucket] = append(payload[bucket].([]map[string]any), row)
}

func markDuplicateDashboardRows(rows []map[string]any) {
	counts := map[string]int{}
	for _, row := range rows {
		key := firstNonEmpty(cleanString(row["dashboard_uid"]), cleanString(row["dashboard_title_fingerprint"]))
		if key != "" {
			counts[key]++
		}
	}
	for _, row := range rows {
		key := firstNonEmpty(cleanString(row["dashboard_uid"]), cleanString(row["dashboard_title_fingerprint"]))
		if counts[key] > 1 {
			row["duplicate_dashboard_identity"] = true
			row["outcome"] = "ambiguous"
		}
	}
}

func isGrafanaDashboardResource(apiVersion string, kind string) bool {
	lowerAPI := strings.ToLower(apiVersion)
	return strings.EqualFold(kind, "GrafanaDashboard") ||
		(strings.Contains(lowerAPI, "grafana") && strings.EqualFold(kind, "Dashboard"))
}
