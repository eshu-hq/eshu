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
	"github.com/zclconf/go-cty/cty"
)

type pagerDutyInputSpec struct {
	attr  string
	field string
}

type pagerDutyInputState struct {
	unresolved []string
	redacted   []string
	malformed  []string
}

type pagerDutyObjectItem struct {
	key  string
	expr hclsyntax.Expression
	line int
}

var pagerDutyModuleInputSpecs = []pagerDutyInputSpec{
	{attr: "name", field: "service_name"},
	{attr: "description", field: "description"},
	{attr: "escalation_policy", field: "escalation_policy"},
	{attr: "incident_urgency", field: "incident_urgency"},
	{attr: "acknowledgement_timeout", field: "acknowledgement_timeout"},
	{attr: "auto_resolve_timeout", field: "auto_resolve_timeout"},
	{attr: "event_orchestration", field: "event_orchestration"},
	{attr: "enable_slack_connection", field: "enable_slack_connection"},
}

var pagerDutyTFVarsFieldNames = map[string]string{
	"pagerduty_service_name":                 "service_name",
	"pagerduty_service_description":          "description",
	"pagerduty_escalation_policy":            "escalation_policy",
	"pagerduty_incident_urgency":             "incident_urgency",
	"pagerduty_acknowledgement_timeout":      "acknowledgement_timeout",
	"pagerduty_auto_resolve_timeout":         "auto_resolve_timeout",
	"pagerduty_event_orchestration":          "event_orchestration",
	"slack_enable_pagerduty":                 "enable_slack_connection",
	"enable_slack_connection":                "enable_slack_connection",
	"pagerduty_enable_slack_connection":      "enable_slack_connection",
	"pagerduty_enable_slack_notifications":   "enable_slack_connection",
	"pagerduty_enable_chatbot_notifications": "enable_slack_connection",
}

func parsePagerDutyDeclarations(body *hclsyntax.Body, source []byte, path string) []map[string]any {
	rows := make([]map[string]any, 0)
	rows = append(rows, parsePagerDutyModuleDeclarations(body, source, path)...)
	if isTerraformVarsFile(path) {
		rows = append(rows, parsePagerDutyTFVarsDeclarations(body, source, path)...)
	}
	markDuplicatePagerDutyServiceNames(rows)
	return rows
}

func parsePagerDutyModuleDeclarations(body *hclsyntax.Body, source []byte, path string) []map[string]any {
	rows := make([]map[string]any, 0)
	for _, block := range body.Blocks {
		if block.Type != "module" || len(block.Labels) == 0 {
			continue
		}
		sourceValue := attributeValue(block.Body.Attributes["source"], source)
		if !isSupportedPagerDutyModuleSource(sourceValue) && !strings.Contains(strings.ToLower(sourceValue), "pagerduty") {
			continue
		}

		row := newPagerDutyDeclarationRow("module."+block.Labels[0], "terraform_module", path, block.TypeRange.Start.Line)
		row["module_name"] = block.Labels[0]
		if fingerprint := fingerprintPagerDutySource(sourceValue); fingerprint != "" {
			row["module_source_fingerprint"] = fingerprint
			row["module_source_redacted"] = true
		}
		setPagerDutyEnvironment(row, path)

		if !isSupportedPagerDutyModuleSource(sourceValue) {
			row["outcome"] = "unsupported"
			row["unsupported_reason"] = "unsupported_module_source"
			finalizePagerDutyDeclaration(row, pagerDutyInputState{})
			rows = append(rows, row)
			continue
		}

		state := pagerDutyInputState{}
		for _, spec := range pagerDutyModuleInputSpecs {
			attr := block.Body.Attributes[spec.attr]
			if attr == nil {
				continue
			}
			recordPagerDutyExpression(row, spec.field, attr.Expr, source, &state)
		}
		for _, item := range sortedAttributes(block.Body.Attributes) {
			if isSensitivePagerDutyInput(item.name) {
				state.redacted = append(state.redacted, item.name)
			}
		}
		finalizePagerDutyDeclaration(row, state)
		rows = append(rows, row)
	}
	return rows
}

func parsePagerDutyTFVarsDeclarations(body *hclsyntax.Body, source []byte, path string) []map[string]any {
	rows := make([]map[string]any, 0)
	for _, attr := range sortedAttributes(body.Attributes) {
		walkPagerDutyTFVarsObject(&rows, []string{attr.name}, attr.attribute.Expr, source, path)
	}
	return rows
}

func walkPagerDutyTFVarsObject(
	rows *[]map[string]any,
	pathParts []string,
	expr hclsyntax.Expression,
	source []byte,
	path string,
) {
	items := sortedPagerDutyObjectItems(expr, source)
	if len(items) == 0 {
		return
	}

	if objectHasPagerDutyFields(items) {
		name := "tfvars." + strings.Join(pathParts, ".")
		row := newPagerDutyDeclarationRow(name, "tfvars", path, expr.Range().Start.Line)
		row["tfvars_path"] = strings.Join(pathParts, ".")
		setPagerDutyEnvironment(row, path)

		state := pagerDutyInputState{}
		for _, item := range items {
			field, ok := pagerDutyTFVarsFieldNames[item.key]
			if !ok {
				if isSensitivePagerDutyInput(item.key) {
					state.redacted = append(state.redacted, item.key)
				}
				continue
			}
			recordPagerDutyExpression(row, field, item.expr, source, &state)
		}
		finalizePagerDutyDeclaration(row, state)
		*rows = append(*rows, row)
	}

	for _, item := range items {
		if _, ok := item.expr.(*hclsyntax.ObjectConsExpr); !ok {
			continue
		}
		walkPagerDutyTFVarsObject(rows, append(pathParts, item.key), item.expr, source, path)
	}
}

func newPagerDutyDeclarationRow(name string, kind string, path string, line int) map[string]any {
	return map[string]any{
		"name":             name,
		"line_number":      line,
		"path":             path,
		"lang":             "hcl",
		"source_class":     "declared",
		"declaration_kind": kind,
		"source_revision":  "unknown",
	}
}

func recordPagerDutyExpression(
	row map[string]any,
	field string,
	expr hclsyntax.Expression,
	source []byte,
	state *pagerDutyInputState,
) {
	resolution := pagerDutyExpressionResolution(expr, source)
	if field == "event_orchestration" {
		row["event_orchestration_declared"] = true
		row["event_orchestration_resolution"] = resolution
		if resolution == "unresolved" {
			state.unresolved = append(state.unresolved, field)
		}
		return
	}

	if resolution == "object" || resolution == "list" {
		state.malformed = append(state.malformed, field)
		row[field+"_resolution"] = resolution
		return
	}
	if isSensitivePagerDutyInput(field) {
		state.redacted = append(state.redacted, field)
		return
	}

	if resolution == "unresolved" {
		row[field+"_resolution"] = resolution
		state.unresolved = append(state.unresolved, field)
		return
	}

	value := pagerDutyExpressionValue(expr, source)
	if strings.TrimSpace(value) != "" {
		row[field] = value
	}
	row[field+"_resolution"] = resolution
}

func finalizePagerDutyDeclaration(row map[string]any, state pagerDutyInputState) {
	setSortedJoined(row, "unresolved_inputs", state.unresolved)
	setSortedJoined(row, "redacted_inputs", state.redacted)
	setSortedJoined(row, "malformed_inputs", state.malformed)
	if len(state.redacted) > 0 {
		row["redaction_state"] = "redacted"
	} else {
		row["redaction_state"] = "none"
	}
	if len(state.malformed) > 0 {
		row["outcome"] = "rejected"
	} else if _, exists := row["outcome"]; !exists {
		row["outcome"] = "declared"
	}
}

func markDuplicatePagerDutyServiceNames(rows []map[string]any) {
	counts := make(map[string]int)
	for _, row := range rows {
		if row["outcome"] == "rejected" {
			continue
		}
		serviceName, _ := row["service_name"].(string)
		serviceName = strings.TrimSpace(serviceName)
		if serviceName != "" {
			counts[strings.ToLower(serviceName)]++
		}
	}
	for _, row := range rows {
		serviceName, _ := row["service_name"].(string)
		if counts[strings.ToLower(strings.TrimSpace(serviceName))] > 1 {
			row["duplicate_service_name"] = true
			if row["outcome"] == "declared" {
				row["outcome"] = "ambiguous"
			}
		}
	}
}

func sortedPagerDutyObjectItems(expr hclsyntax.Expression, source []byte) []pagerDutyObjectItem {
	objectExpr, ok := expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil
	}
	items := make([]pagerDutyObjectItem, 0, len(objectExpr.Items))
	for _, item := range objectExpr.Items {
		key := strings.Trim(expressionText(item.KeyExpr, source), `"`)
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		items = append(items, pagerDutyObjectItem{
			key:  key,
			expr: item.ValueExpr,
			line: item.ValueExpr.Range().Start.Line,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].key != items[j].key {
			return items[i].key < items[j].key
		}
		return items[i].line < items[j].line
	})
	return items
}

func objectHasPagerDutyFields(items []pagerDutyObjectItem) bool {
	for _, item := range items {
		if _, ok := pagerDutyTFVarsFieldNames[item.key]; ok {
			return true
		}
		if strings.HasPrefix(item.key, "pagerduty_") {
			return true
		}
	}
	return false
}

func pagerDutyExpressionResolution(expr hclsyntax.Expression, source []byte) string {
	switch typed := expr.(type) {
	case *hclsyntax.LiteralValueExpr:
		return "literal"
	case *hclsyntax.ScopeTraversalExpr:
		return "reference"
	case *hclsyntax.ObjectConsExpr:
		return "object"
	case *hclsyntax.TupleConsExpr:
		return "list"
	case *hclsyntax.TemplateExpr:
		if strings.Contains(sourceRange(source, typed.Range()), "${") {
			return "unresolved"
		}
		return "literal"
	default:
		return "unresolved"
	}
}

func pagerDutyExpressionValue(expr hclsyntax.Expression, source []byte) string {
	if literal, ok := expr.(*hclsyntax.LiteralValueExpr); ok && literal.Val.Type() == cty.String {
		return literal.Val.AsString()
	}
	return strings.Trim(strings.TrimSpace(sourceRange(source, expr.Range())), `"`)
}

func isSupportedPagerDutyModuleSource(source string) bool {
	normalized := strings.ToLower(strings.TrimSpace(source))
	return strings.Contains(normalized, "pagerduty-service")
}

func isSensitivePagerDutyInput(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	for _, marker := range []string{"integration_key", "routing_key", "webhook_secret", "token", "secret", "endpoint", "private_url"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	if strings.Contains(normalized, "url") &&
		(strings.Contains(normalized, "pagerduty") || strings.Contains(normalized, "webhook")) {
		return true
	}
	return false
}

func isTerraformVarsFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(base, ".tfvars") || strings.HasSuffix(base, ".tfvars.json")
}

func setPagerDutyEnvironment(row map[string]any, path string) {
	normalized := filepath.ToSlash(path)
	parts := strings.Split(normalized, "/")
	for index, part := range parts {
		if part == "environments" && index+1 < len(parts) {
			env := strings.TrimSpace(parts[index+1])
			if env != "" {
				row["environment"] = env
				row["workspace"] = env
			}
			return
		}
	}
}

func fingerprintPagerDutySource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.ToLower(source)))
	return hex.EncodeToString(sum[:])[:16]
}

func setSortedJoined(row map[string]any, key string, values []string) {
	if len(values) == 0 {
		return
	}
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			unique[value] = struct{}{}
		}
	}
	if len(unique) == 0 {
		return
	}
	sorted := make([]string, 0, len(unique))
	for value := range unique {
		sorted = append(sorted, value)
	}
	sort.Strings(sorted)
	row[key] = strings.Join(sorted, ",")
}
