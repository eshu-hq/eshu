package json

import (
	"fmt"
	"strings"
)

func applyJSONReplayDocument(
	payload map[string]any,
	document map[string]any,
	filename string,
	lineageExtractor LineageExtractor,
) bool {
	switch {
	case isDBTManifestDocument(document, filename):
		applyDBTManifestDocument(payload, document, lineageExtractor)
		return true
	case isWarehouseReplayDocument(document, filename):
		applyWarehouseReplayDocument(payload, document)
		return true
	case isBIReplayDocument(document, filename):
		applyBIReplayDocument(payload, document)
		return true
	case isSemanticReplayDocument(document, filename):
		applySemanticReplayDocument(payload, document)
		return true
	case isQualityReplayDocument(document, filename):
		applyQualityReplayDocument(payload, document)
		return true
	case isGovernanceReplayDocument(document, filename):
		applyGovernanceReplayDocument(payload, document)
		return true
	default:
		return false
	}
}

func isWarehouseReplayDocument(document map[string]any, filename string) bool {
	metadata, _ := document["metadata"].(map[string]any)
	assets, assetsOK := document["assets"].([]any)
	queryHistory, queriesOK := document["query_history"].([]any)
	return strings.EqualFold(filename, "warehouse_replay.json") && metadata != nil && assetsOK && queriesOK && len(assets) >= 0 && len(queryHistory) >= 0
}

func isBIReplayDocument(document map[string]any, filename string) bool {
	metadata, _ := document["metadata"].(map[string]any)
	_, dashboardsOK := document["dashboards"].([]any)
	return strings.EqualFold(filename, "bi_replay.json") && metadata != nil && dashboardsOK
}

func isSemanticReplayDocument(document map[string]any, filename string) bool {
	metadata, _ := document["metadata"].(map[string]any)
	_, modelsOK := document["models"].([]any)
	return strings.EqualFold(filename, "semantic_replay.json") && metadata != nil && modelsOK
}

func isQualityReplayDocument(document map[string]any, filename string) bool {
	metadata, _ := document["metadata"].(map[string]any)
	_, checksOK := document["checks"].([]any)
	return strings.EqualFold(filename, "quality_replay.json") && metadata != nil && checksOK
}

func isGovernanceReplayDocument(document map[string]any, filename string) bool {
	metadata, _ := document["metadata"].(map[string]any)
	_, ownersOK := document["owners"].([]any)
	_, contractsOK := document["contracts"].([]any)
	return strings.EqualFold(filename, "governance_replay.json") && metadata != nil && ownersOK && contractsOK
}

func applyWarehouseReplayDocument(payload map[string]any, document map[string]any) {
	assetsByName := make(map[string]map[string]any)
	columnsByName := make(map[string]map[string]any)
	queryExecutions := make([]map[string]any, 0)
	relationships := make([]map[string]any, 0)

	for _, rawAsset := range jsonObjectSlice(document["assets"]) {
		assetRecord := warehouseAssetRecord(rawAsset)
		assetName, _ := assetRecord["name"].(string)
		if strings.TrimSpace(assetName) == "" {
			continue
		}
		assetsByName[assetName] = assetRecord
		for _, columnRecord := range warehouseColumnRecords(rawAsset, assetName) {
			columnName, _ := columnRecord["name"].(string)
			columnsByName[columnName] = columnRecord
		}
	}

	for _, rawQuery := range jsonObjectSlice(document["query_history"]) {
		queryRecord := warehouseQueryExecutionRecord(rawQuery)
		queryExecutions = append(queryExecutions, queryRecord)
		sourceID, _ := queryRecord["id"].(string)
		sourceName, _ := queryRecord["name"].(string)
		for _, assetName := range jsonStringSlice(rawQuery["touched_assets"]) {
			relationships = append(relationships, map[string]any{
				"type":        "RUNS_QUERY_AGAINST",
				"source_id":   sourceID,
				"source_name": sourceName,
				"target_id":   "data-asset:" + assetName,
				"target_name": assetName,
				"confidence":  1.0,
			})
		}
	}

	payload["data_assets"] = sortedJSONRecords(assetsByName)
	payload["data_columns"] = sortedJSONRecords(columnsByName)
	payload["query_executions"] = sortJSONRecords(queryExecutions)
	payload["data_relationships"] = sortRelationships(relationships)
	payload["data_intelligence_coverage"] = completeCoverage()
}

func warehouseAssetRecord(asset map[string]any) map[string]any {
	assetName := strings.Join(nonEmptyStrings(
		fmt.Sprint(asset["database"]),
		fmt.Sprint(asset["schema"]),
		fmt.Sprint(asset["name"]),
	), ".")
	return map[string]any{
		"id":          "data-asset:" + assetName,
		"name":        assetName,
		"line_number": 1,
		"database":    fmt.Sprint(asset["database"]),
		"schema":      fmt.Sprint(asset["schema"]),
		"kind":        defaultString(asset["kind"], "table"),
	}
}

func warehouseColumnRecords(asset map[string]any, assetName string) []map[string]any {
	records := make([]map[string]any, 0)
	for _, rawColumn := range jsonObjectSlice(asset["columns"]) {
		columnName := strings.TrimSpace(fmt.Sprint(rawColumn["name"]))
		if columnName == "" {
			continue
		}
		qualifiedName := assetName + "." + columnName
		records = append(records, map[string]any{
			"id":          "data-column:" + qualifiedName,
			"asset_name":  assetName,
			"name":        qualifiedName,
			"line_number": 1,
		})
	}
	return records
}

func warehouseQueryExecutionRecord(query map[string]any) map[string]any {
	queryID := strings.TrimSpace(fmt.Sprint(query["query_id"]))
	queryName := strings.TrimSpace(fmt.Sprint(query["name"]))
	if queryName == "" {
		queryName = queryID
	}
	return map[string]any{
		"id":          "query-execution:" + queryID,
		"name":        queryName,
		"line_number": 1,
		"statement":   fmt.Sprint(query["statement"]),
		"status":      defaultString(query["status"], "unknown"),
		"executed_by": fmt.Sprint(query["executed_by"]),
		"started_at":  fmt.Sprint(query["started_at"]),
	}
}

func applyBIReplayDocument(payload map[string]any, document map[string]any) {
	workspace := defaultString(metadataField(document, "workspace"), "default")
	dashboards := make([]map[string]any, 0)
	relationships := make([]map[string]any, 0)

	for _, dashboard := range jsonObjectSlice(document["dashboards"]) {
		record := dashboardAssetRecord(dashboard, workspace)
		dashboards = append(dashboards, record)
		targetID, _ := record["id"].(string)
		targetName, _ := record["name"].(string)
		for _, assetName := range jsonStringSlice(dashboard["consumes_assets"]) {
			relationships = append(relationships, map[string]any{
				"type":        "POWERS",
				"source_id":   "data-asset:" + assetName,
				"source_name": assetName,
				"target_id":   targetID,
				"target_name": targetName,
				"confidence":  1.0,
			})
		}
		for _, columnName := range jsonStringSlice(dashboard["consumes_columns"]) {
			relationships = append(relationships, map[string]any{
				"type":        "POWERS",
				"source_id":   "data-column:" + columnName,
				"source_name": columnName,
				"target_id":   targetID,
				"target_name": targetName,
				"confidence":  1.0,
			})
		}
	}

	payload["dashboard_assets"] = sortJSONRecords(dashboards)
	payload["data_relationships"] = sortRelationships(relationships)
	payload["data_intelligence_coverage"] = completeCoverage()
}

func dashboardAssetRecord(dashboard map[string]any, workspace string) map[string]any {
	dashboardID := strings.TrimSpace(fmt.Sprint(dashboard["dashboard_id"]))
	if dashboardID == "" {
		dashboardID = strings.ToLower(strings.TrimSpace(fmt.Sprint(dashboard["name"])))
	}
	return map[string]any{
		"id":          "dashboard-asset:" + workspace + ":" + dashboardID,
		"name":        defaultString(dashboard["name"], dashboardID),
		"line_number": 1,
		"path":        fmt.Sprint(dashboard["path"]),
		"workspace":   workspace,
	}
}

func applySemanticReplayDocument(payload map[string]any, document map[string]any) {
	assetsByName := make(map[string]map[string]any)
	columnsByName := make(map[string]map[string]any)
	relationships := make([]map[string]any, 0)

	for _, model := range jsonObjectSlice(document["models"]) {
		assetRecord := semanticAssetRecord(model)
		assetName, _ := assetRecord["name"].(string)
		if strings.TrimSpace(assetName) == "" {
			continue
		}
		assetsByName[assetName] = assetRecord
		for _, upstreamAsset := range jsonStringSlice(model["upstream_assets"]) {
			relationships = append(relationships, map[string]any{
				"type":        "ASSET_DERIVES_FROM",
				"source_id":   assetRecord["id"],
				"source_name": assetName,
				"target_id":   "data-asset:" + upstreamAsset,
				"target_name": upstreamAsset,
				"confidence":  1.0,
			})
		}
		for _, field := range jsonObjectSlice(model["fields"]) {
			columnRecord, ok := semanticColumnRecord(assetName, field)
			if !ok {
				continue
			}
			columnName, _ := columnRecord["name"].(string)
			columnsByName[columnName] = columnRecord
			sourceColumn := strings.TrimSpace(fmt.Sprint(field["source_column"]))
			if sourceColumn == "" {
				continue
			}
			relationships = append(relationships, map[string]any{
				"type":        "COLUMN_DERIVES_FROM",
				"source_id":   columnRecord["id"],
				"source_name": columnName,
				"target_id":   "data-column:" + sourceColumn,
				"target_name": sourceColumn,
				"confidence":  1.0,
			})
		}
	}

	payload["data_assets"] = sortedJSONRecords(assetsByName)
	payload["data_columns"] = sortedJSONRecords(columnsByName)
	payload["data_relationships"] = sortRelationships(relationships)
	payload["data_intelligence_coverage"] = completeCoverage()
}

func semanticAssetRecord(model map[string]any) map[string]any {
	assetName := strings.TrimSpace(fmt.Sprint(model["name"]))
	modelID := strings.TrimSpace(fmt.Sprint(model["model_id"]))
	if modelID == "" {
		modelID = assetName
	}
	return map[string]any{
		"id":          "data-asset:" + assetName,
		"name":        assetName,
		"line_number": 1,
		"path":        fmt.Sprint(model["path"]),
		"kind":        defaultString(model["kind"], "semantic_model"),
		"source_id":   modelID,
	}
}

func semanticColumnRecord(assetName string, field map[string]any) (map[string]any, bool) {
	fieldName := strings.TrimSpace(fmt.Sprint(field["name"]))
	if fieldName == "" {
		return nil, false
	}
	qualifiedName := assetName + "." + fieldName
	return map[string]any{
		"id":          "data-column:" + qualifiedName,
		"asset_name":  assetName,
		"name":        qualifiedName,
		"line_number": 1,
	}, true
}

func applyQualityReplayDocument(payload map[string]any, document map[string]any) {
	workspace := defaultString(metadataField(document, "workspace"), "default")
	checks := make([]map[string]any, 0)
	relationships := make([]map[string]any, 0)

	for _, check := range jsonObjectSlice(document["checks"]) {
		record := qualityCheckRecord(check, workspace)
		checks = append(checks, record)
		sourceID, _ := record["id"].(string)
		sourceName, _ := record["name"].(string)
		for _, assetName := range jsonStringSlice(check["targets_assets"]) {
			relationships = append(relationships, map[string]any{
				"type":        "ASSERTS_QUALITY_ON",
				"source_id":   sourceID,
				"source_name": sourceName,
				"target_id":   "data-asset:" + assetName,
				"target_name": assetName,
				"confidence":  1.0,
			})
		}
		for _, columnName := range jsonStringSlice(check["targets_columns"]) {
			relationships = append(relationships, map[string]any{
				"type":        "ASSERTS_QUALITY_ON",
				"source_id":   sourceID,
				"source_name": sourceName,
				"target_id":   "data-column:" + columnName,
				"target_name": columnName,
				"confidence":  1.0,
			})
		}
	}

	payload["data_quality_checks"] = sortJSONRecords(checks)
	payload["data_relationships"] = sortRelationships(relationships)
	payload["data_intelligence_coverage"] = completeCoverage()
}

func qualityCheckRecord(check map[string]any, workspace string) map[string]any {
	checkID := strings.TrimSpace(fmt.Sprint(check["check_id"]))
	if checkID == "" {
		checkID = strings.ToLower(strings.TrimSpace(fmt.Sprint(check["name"])))
	}
	return map[string]any{
		"id":          "data-quality-check:" + workspace + ":" + checkID,
		"name":        defaultString(check["name"], checkID),
		"line_number": 1,
		"path":        fmt.Sprint(check["path"]),
		"check_type":  defaultString(check["check_type"], "assertion"),
		"status":      defaultString(check["status"], "unknown"),
		"severity":    defaultString(check["severity"], "medium"),
	}
}
