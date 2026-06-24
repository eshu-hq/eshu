// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import "strings"

func appendDashboardRow(payload map[string]any, dashboard map[string]any, ctx grafanaSourceContext) {
	uid := cleanString(dashboard["uid"])
	titleFingerprint := fingerprintValue(cleanString(dashboard["title"]))
	row := baseObservabilityRow(ctx, "dashboard."+firstNonEmpty(uid, ctx.resourceName, ctx.configKey, titleFingerprint))
	row["declaration_kind"] = firstNonEmpty(ctx.declarationKind, "grafana_dashboard")
	if uid != "" {
		row["dashboard_uid"] = uid
	}
	if titleFingerprint != "" {
		row["dashboard_title_fingerprint"] = titleFingerprint
	}
	if folder := firstNonEmpty(ctx.folder, cleanString(dashboard["folder"])); folder != "" {
		row["folder"] = folder
	}
	if ctx.folderUID != "" {
		row["folder_uid"] = ctx.folderUID
	}
	if refs := collectDashboardDatasourceRefs(dashboard); len(refs) > 0 {
		row["datasource_refs"] = strings.Join(refs, ",")
	}
	if hints := collectDashboardServiceHints(dashboard, ctx.labels); len(hints) > 0 {
		row["service_hints"] = strings.Join(hints, ",")
	}
	if uid != "" || titleFingerprint != "" {
		row["outcome"] = "exact"
	} else {
		row["outcome"] = "derived"
	}
	appendBucketRow(payload, observabilityDashboardBucket, row)
}

func appendDatasourcesFromObject(payload map[string]any, object map[string]any, ctx grafanaSourceContext) {
	items, ok := object["datasources"].([]any)
	if !ok {
		if datasource := nestedMap(object, "datasource"); len(datasource) > 0 {
			items = []any{datasource}
		}
	}
	for _, item := range items {
		datasource, ok := item.(map[string]any)
		if !ok {
			continue
		}
		appendDatasourceRow(payload, datasource, ctx)
	}
}

func appendDatasourceRow(payload map[string]any, datasource map[string]any, ctx grafanaSourceContext) {
	uid := cleanString(datasource["uid"])
	datasourceType := strings.ToLower(cleanString(datasource["type"]))
	nameFingerprint := fingerprintValue(cleanString(datasource["name"]))
	row := baseObservabilityRow(ctx, "datasource."+firstNonEmpty(uid, nameFingerprint, ctx.configKey))
	row["declaration_kind"] = firstNonEmpty(ctx.declarationKind, "grafana_datasource")
	if uid != "" {
		row["datasource_uid"] = uid
	}
	if datasourceType != "" {
		row["datasource_type"] = datasourceType
	}
	if nameFingerprint != "" {
		row["datasource_name_fingerprint"] = nameFingerprint
	}
	redacted := datasourceRedactedFields(datasource)
	if len(redacted) > 0 {
		row["redacted_fields"] = strings.Join(redacted, ",")
		row["redaction_state"] = "redacted"
	}
	if _, ok := supportedGrafanaDatasourceTypes[datasourceType]; datasourceType != "" && !ok {
		row["outcome"] = "unsupported"
		appendObservabilityWarning(payload, ctx, "unsupported_datasource_type", "unsupported")
	} else {
		row["outcome"] = "exact"
	}
	appendBucketRow(payload, observabilityDatasourceBucket, row)
	appendLogRouteFromGrafanaDatasource(payload, datasource, ctx, uid, datasourceType, nameFingerprint)
	appendTraceRouteFromGrafanaDatasource(payload, datasource, ctx, uid, datasourceType, nameFingerprint)
}

func walkGrafanaAlertDocuments(payload map[string]any, object map[string]any, ctx grafanaSourceContext) {
	groups, _ := object["groups"].([]any)
	for _, groupItem := range groups {
		group, ok := groupItem.(map[string]any)
		if !ok {
			continue
		}
		rules, _ := group["rules"].([]any)
		for _, ruleItem := range rules {
			rule, ok := ruleItem.(map[string]any)
			if !ok {
				continue
			}
			appendAlertRuleRow(payload, group, rule, ctx)
		}
	}
}

func appendAlertRuleRow(payload map[string]any, group map[string]any, rule map[string]any, ctx grafanaSourceContext) {
	uid := cleanString(rule["uid"])
	titleFingerprint := fingerprintValue(cleanString(rule["title"]))
	row := baseObservabilityRow(ctx, "alert."+firstNonEmpty(uid, titleFingerprint, cleanString(group["name"]), ctx.configKey))
	row["declaration_kind"] = firstNonEmpty(ctx.declarationKind, "grafana_alert_rule")
	if uid != "" {
		row["alert_rule_uid"] = uid
	}
	if titleFingerprint != "" {
		row["alert_rule_title_fingerprint"] = titleFingerprint
	}
	if groupName := cleanString(group["name"]); groupName != "" {
		row["rule_group"] = groupName
	}
	if folder := firstNonEmpty(cleanString(group["folder"]), ctx.folder); folder != "" {
		row["folder"] = folder
	}
	if condition := cleanString(rule["condition"]); condition != "" {
		row["condition_ref"] = condition
	}
	if refs := collectAlertDatasourceRefs(rule); len(refs) > 0 {
		row["datasource_refs"] = strings.Join(refs, ",")
	}
	if redacted := alertRedactedFields(rule); len(redacted) > 0 {
		row["redacted_fields"] = strings.Join(redacted, ",")
		row["redaction_state"] = "redacted"
	}
	row["outcome"] = firstNonEmpty(cleanString(row["outcome"]), "exact")
	appendBucketRow(payload, observabilityAlertRuleBucket, row)
}
