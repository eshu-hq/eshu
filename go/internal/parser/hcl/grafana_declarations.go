// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hcl

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2/hclsyntax"
)

func parseGrafanaDeclarations(body *hclsyntax.Body, source []byte, path string) map[string][]map[string]any {
	rows := map[string][]map[string]any{
		"observability_declared_folders":     {},
		"observability_declared_dashboards":  {},
		"observability_declared_datasources": {},
		"observability_declared_alert_rules": {},
		"observability_coverage_warnings":    {},
	}
	for _, block := range body.Blocks {
		if block.Type != "resource" || len(block.Labels) < 2 {
			continue
		}
		resourceType := block.Labels[0]
		resourceName := block.Labels[1]
		switch resourceType {
		case "grafana_folder":
			row := grafanaBaseRow(block, path, resourceType, resourceName)
			row["name"] = "folder." + firstHCLNonEmpty(
				literalGrafanaAttribute(block.Body.Attributes["uid"]),
				resourceName,
			)
			row["declaration_kind"] = "terraform_resource"
			if uid := literalGrafanaAttribute(block.Body.Attributes["uid"]); uid != "" {
				row["folder_uid"] = uid
			}
			if title := literalGrafanaAttribute(block.Body.Attributes["title"]); title != "" {
				row["folder_title_fingerprint"] = fingerprintHCLValue(title)
			}
			if row["folder_uid"] != nil || row["folder_title_fingerprint"] != nil {
				row["outcome"] = "exact"
			} else {
				row["outcome"] = "derived"
			}
			rows["observability_declared_folders"] = append(rows["observability_declared_folders"], row)
		case "grafana_data_source":
			row := grafanaBaseRow(block, path, resourceType, resourceName)
			datasourceUID := literalGrafanaAttribute(block.Body.Attributes["uid"])
			row["name"] = "datasource." + firstHCLNonEmpty(
				datasourceUID,
				resourceName,
			)
			row["declaration_kind"] = "terraform_resource"
			if datasourceUID != "" {
				row["datasource_uid"] = datasourceUID
			} else if block.Body.Attributes["uid"] != nil {
				row["datasource_uid_resolution"] = "unresolved"
			}
			if block.Body.Attributes["type"] != nil {
				if dsType := strings.ToLower(literalGrafanaAttribute(block.Body.Attributes["type"])); dsType != "" {
					row["datasource_type"] = dsType
					if !supportedGrafanaDatasourceType(dsType) {
						row["outcome"] = "unsupported"
						rows["observability_coverage_warnings"] = append(
							rows["observability_coverage_warnings"],
							grafanaWarningRow(block, path, resourceType, resourceName, "unsupported_datasource_type", "unsupported"),
						)
					}
				} else {
					row["datasource_type_resolution"] = "unresolved"
					row["outcome"] = "derived"
				}
			}
			if fingerprint := fingerprintHCLValue(literalGrafanaAttribute(block.Body.Attributes["name"])); fingerprint != "" {
				row["datasource_name_fingerprint"] = fingerprint
			}
			if redacted := redactedGrafanaResourceAttributes(block.Body.Attributes); len(redacted) > 0 {
				row["redacted_fields"] = strings.Join(redacted, ",")
				row["redaction_state"] = "redacted"
			}
			if row["outcome"] == nil {
				row["outcome"] = "exact"
			}
			rows["observability_declared_datasources"] = append(rows["observability_declared_datasources"], row)
		case "grafana_dashboard":
			row := grafanaBaseRow(block, path, resourceType, resourceName)
			row["name"] = "dashboard." + resourceName
			row["declaration_kind"] = "terraform_resource"
			metadata := collectGrafanaDashboardMetadata(block.Body.Attributes["config_json"], source)
			if metadata.uid != "" {
				row["dashboard_uid"] = metadata.uid
			}
			if metadata.titleFingerprint != "" {
				row["dashboard_title_fingerprint"] = metadata.titleFingerprint
			}
			if len(metadata.datasourceRefs) > 0 {
				row["datasource_refs"] = strings.Join(metadata.datasourceRefs, ",")
			}
			if metadata.uid != "" || metadata.titleFingerprint != "" {
				row["outcome"] = "exact"
			} else {
				row["outcome"] = "derived"
			}
			if folder := attributeValue(block.Body.Attributes["folder"], source); folder != "" {
				row["folder_ref"] = folder
			}
			if redacted := redactedGrafanaResourceAttributes(block.Body.Attributes); len(redacted) > 0 {
				row["redacted_fields"] = strings.Join(redacted, ",")
				row["redaction_state"] = "redacted"
			}
			rows["observability_declared_dashboards"] = append(rows["observability_declared_dashboards"], row)
		case "grafana_rule_group":
			row := grafanaBaseRow(block, path, resourceType, resourceName)
			row["name"] = "alert_group." + resourceName
			row["declaration_kind"] = "terraform_resource"
			row["outcome"] = "derived"
			if groupName := attributeValue(block.Body.Attributes["name"], source); groupName != "" {
				row["rule_group"] = groupName
			}
			if folderUID := attributeValue(block.Body.Attributes["folder_uid"], source); folderUID != "" {
				row["folder_uid_ref"] = folderUID
			}
			if refs := collectGrafanaRuleDatasourceRefs(block); len(refs) > 0 {
				row["datasource_refs"] = strings.Join(refs, ",")
			}
			if redacted := collectGrafanaRuleRedactions(block); len(redacted) > 0 {
				row["redacted_fields"] = strings.Join(redacted, ",")
				row["redaction_state"] = "redacted"
			}
			rows["observability_declared_alert_rules"] = append(rows["observability_declared_alert_rules"], row)
		}
	}
	for _, bucket := range rows {
		sort.Slice(bucket, func(i, j int) bool {
			left, _ := bucket[i]["name"].(string)
			right, _ := bucket[j]["name"].(string)
			return left < right
		})
	}
	return rows
}

func literalGrafanaAttribute(attribute *hclsyntax.Attribute) string {
	value, ok := literalAttributeValue(attribute)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func grafanaBaseRow(block *hclsyntax.Block, path string, resourceType string, resourceName string) map[string]any {
	row := map[string]any{
		"name":            resourceType + "." + resourceName,
		"line_number":     block.TypeRange.Start.Line,
		"path":            path,
		"lang":            "hcl",
		"source_class":    "declared",
		"source_kind":     "terraform",
		"source_revision": "unknown",
		"resource_type":   resourceType,
		"resource_name":   resourceName,
		"redaction_state": "none",
	}
	if env := grafanaEnvironmentFromPath(path); env != "" {
		row["environment"] = env
		row["workspace"] = env
	}
	return row
}

func grafanaWarningRow(
	block *hclsyntax.Block,
	path string,
	resourceType string,
	resourceName string,
	warningKind string,
	outcome string,
) map[string]any {
	row := grafanaBaseRow(block, path, resourceType, resourceName)
	row["name"] = "warning." + warningKind + "." + resourceName
	row["warning_kind"] = warningKind
	row["outcome"] = outcome
	return row
}

func collectGrafanaRuleDatasourceRefs(block *hclsyntax.Block) []string {
	var refs []string
	for _, rule := range block.Body.Blocks {
		if rule.Type != "rule" {
			continue
		}
		for _, data := range rule.Body.Blocks {
			if data.Type != "data" {
				continue
			}
			if attr := data.Body.Attributes["datasource_uid"]; attr != nil {
				if value, ok := literalAttributeValue(attr); ok && value != "" {
					refs = append(refs, "uid:"+value)
				}
			}
		}
	}
	return sortedHCLUnique(refs)
}

func collectGrafanaRuleRedactions(block *hclsyntax.Block) []string {
	var redacted []string
	for _, rule := range block.Body.Blocks {
		if rule.Type != "rule" {
			continue
		}
		for _, data := range rule.Body.Blocks {
			if data.Type != "data" {
				continue
			}
			if _, ok := data.Body.Attributes["model"]; ok {
				redacted = append(redacted, "rule.data.model")
			}
		}
	}
	return sortedHCLUnique(redacted)
}

func redactedGrafanaResourceAttributes(attributes map[string]*hclsyntax.Attribute) []string {
	var redacted []string
	for _, item := range sortedAttributes(attributes) {
		lower := strings.ToLower(item.name)
		if lower == "url" || lower == "config_json" || strings.Contains(lower, "secret") ||
			strings.Contains(lower, "password") || strings.Contains(lower, "token") ||
			strings.Contains(lower, "secure_json_data") {
			redacted = append(redacted, item.name)
		}
	}
	return sortedHCLUnique(redacted)
}

func supportedGrafanaDatasourceType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "cloudwatch", "loki", "mimir", "prometheus", "tempo":
		return true
	default:
		return false
	}
}

func firstHCLNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func fingerprintHCLValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.ToLower(value)))
	return hex.EncodeToString(sum[:])[:16]
}

func sortedHCLUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if cleaned := strings.TrimSpace(value); cleaned != "" {
			seen[cleaned] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func grafanaEnvironmentFromPath(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for index, part := range parts {
		if part == "environments" && index+1 < len(parts) {
			return strings.TrimSpace(parts[index+1])
		}
	}
	return ""
}
