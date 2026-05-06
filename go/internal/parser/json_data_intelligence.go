package parser

import (
	"fmt"
	"sort"
	"strings"
)

func applyJSONReplayDocument(payload map[string]any, document map[string]any, filename string) bool {
	switch {
	case isDBTManifestDocument(document, filename):
		applyDBTManifestDocument(payload, document)
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

func applyGovernanceReplayDocument(payload map[string]any, document map[string]any) {
	workspace := defaultString(metadataField(document, "workspace"), "default")
	owners := make([]map[string]any, 0)
	contracts := make([]map[string]any, 0)
	relationships := make([]map[string]any, 0)
	annotationsByTarget := make(map[string]map[string]any)

	for _, owner := range jsonObjectSlice(document["owners"]) {
		record := governanceOwnerRecord(owner, workspace)
		owners = append(owners, record)
		recordName, _ := record["name"].(string)
		recordTeam, _ := record["team"].(string)
		for _, assetName := range jsonStringSlice(owner["owns_assets"]) {
			relationships = append(relationships, governanceRelationship("OWNS", recordName, assetName, "", ""))
			updateGovernanceAnnotation(annotationsByTarget, assetName, "DataAsset", recordName, recordTeam, "", "", "", "", false, "")
		}
		for _, columnName := range jsonStringSlice(owner["owns_columns"]) {
			relationships = append(relationships, governanceRelationship("OWNS", recordName, columnName, "", ""))
			updateGovernanceAnnotation(annotationsByTarget, columnName, "DataColumn", recordName, recordTeam, "", "", "", "", false, "")
		}
	}

	for _, contract := range jsonObjectSlice(document["contracts"]) {
		record := governanceContractRecord(contract, workspace)
		contracts = append(contracts, record)
		recordName, _ := record["name"].(string)
		contractLevel, _ := record["contract_level"].(string)
		changePolicy, _ := record["change_policy"].(string)
		for _, assetName := range jsonStringSlice(contract["targets_assets"]) {
			relationships = append(relationships, governanceRelationship("DECLARES_CONTRACT_FOR", recordName, assetName, "", ""))
			updateGovernanceAnnotation(annotationsByTarget, assetName, "DataAsset", "", "", recordName, contractLevel, changePolicy, "", false, "")
		}
		for _, column := range governanceTargetColumns(contract["targets_columns"]) {
			targetName, _ := column["name"].(string)
			sensitivity, _ := column["sensitivity"].(string)
			isProtected, _ := column["is_protected"].(bool)
			protectionKind, _ := column["protection_kind"].(string)
			relationships = append(relationships, governanceRelationship("DECLARES_CONTRACT_FOR", recordName, targetName, "", ""))
			updateGovernanceAnnotation(annotationsByTarget, targetName, "DataColumn", "", "", recordName, contractLevel, changePolicy, sensitivity, isProtected, protectionKind)
			if isProtected {
				relationships = append(relationships, governanceRelationship("MASKS", recordName, targetName, sensitivity, protectionKind))
			}
		}
	}

	payload["data_owners"] = sortJSONRecords(owners)
	payload["data_contracts"] = sortJSONRecords(contracts)
	payload["data_relationships"] = sortRelationships(relationships)
	payload["data_governance_annotations"] = finalizeGovernanceAnnotations(annotationsByTarget)
	payload["data_intelligence_coverage"] = completeCoverage()
}

func governanceOwnerRecord(owner map[string]any, workspace string) map[string]any {
	ownerID := strings.TrimSpace(fmt.Sprint(owner["owner_id"]))
	if ownerID == "" {
		ownerID = "data-owner"
	}
	return map[string]any{
		"name":        defaultString(owner["name"], ownerID),
		"line_number": 1,
		"path":        fmt.Sprint(owner["path"]),
		"workspace":   workspace,
		"team":        strings.TrimSpace(fmt.Sprint(owner["team"])),
	}
}

func governanceContractRecord(contract map[string]any, workspace string) map[string]any {
	contractID := strings.TrimSpace(fmt.Sprint(contract["contract_id"]))
	if contractID == "" {
		contractID = "data-contract"
	}
	return map[string]any{
		"name":           defaultString(contract["name"], contractID),
		"line_number":    1,
		"path":           fmt.Sprint(contract["path"]),
		"workspace":      workspace,
		"contract_level": defaultString(contract["contract_level"], "unspecified"),
		"change_policy":  defaultString(contract["change_policy"], "unknown"),
	}
}

func governanceTargetColumns(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	columns := make([]map[string]any, 0, len(items))
	for _, item := range items {
		switch typed := item.(type) {
		case string:
			if strings.TrimSpace(typed) == "" {
				continue
			}
			columns = append(columns, map[string]any{"name": strings.TrimSpace(typed)})
		case map[string]any:
			name := strings.TrimSpace(fmt.Sprint(typed["name"]))
			if name == "" {
				continue
			}
			columns = append(columns, map[string]any{
				"name":            name,
				"sensitivity":     optionalString(typed["sensitivity"]),
				"is_protected":    jsonBool(typed["is_protected"]),
				"protection_kind": optionalString(typed["protection_kind"]),
			})
		}
	}
	return columns
}

func governanceRelationship(kind string, sourceName string, targetName string, sensitivity string, protectionKind string) map[string]any {
	record := map[string]any{
		"type":        kind,
		"source_name": sourceName,
		"target_name": targetName,
		"confidence":  1.0,
	}
	if sensitivity != "" {
		record["sensitivity"] = sensitivity
	}
	if protectionKind != "" {
		record["protection_kind"] = protectionKind
	}
	return record
}

func updateGovernanceAnnotation(
	annotations map[string]map[string]any,
	targetName string,
	targetKind string,
	ownerName string,
	ownerTeam string,
	contractName string,
	contractLevel string,
	changePolicy string,
	sensitivity string,
	isProtected bool,
	protectionKind string,
) {
	annotation, ok := annotations[targetName]
	if !ok {
		annotation = map[string]any{
			"target_name":      targetName,
			"target_kind":      targetKind,
			"_owner_names":     map[string]struct{}{},
			"_owner_teams":     map[string]struct{}{},
			"_contract_names":  map[string]struct{}{},
			"_contract_levels": map[string]struct{}{},
			"_change_policies": map[string]struct{}{},
			"sensitivity":      nil,
			"is_protected":     false,
			"protection_kind":  nil,
		}
		annotations[targetName] = annotation
	}
	if ownerName != "" {
		annotation["_owner_names"].(map[string]struct{})[ownerName] = struct{}{}
	}
	if ownerTeam != "" {
		annotation["_owner_teams"].(map[string]struct{})[ownerTeam] = struct{}{}
	}
	if contractName != "" {
		annotation["_contract_names"].(map[string]struct{})[contractName] = struct{}{}
	}
	if contractLevel != "" {
		annotation["_contract_levels"].(map[string]struct{})[contractLevel] = struct{}{}
	}
	if changePolicy != "" {
		annotation["_change_policies"].(map[string]struct{})[changePolicy] = struct{}{}
	}
	if sensitivity != "" {
		annotation["sensitivity"] = sensitivity
	}
	if isProtected {
		annotation["is_protected"] = true
	}
	if protectionKind != "" {
		annotation["protection_kind"] = protectionKind
	}
}

func finalizeGovernanceAnnotations(annotations map[string]map[string]any) []map[string]any {
	items := make([]map[string]any, 0, len(annotations))
	for _, annotation := range annotations {
		items = append(items, map[string]any{
			"target_name":     annotation["target_name"],
			"target_kind":     annotation["target_kind"],
			"owner_names":     sortedSetValues(annotation["_owner_names"].(map[string]struct{})),
			"owner_teams":     sortedSetValues(annotation["_owner_teams"].(map[string]struct{})),
			"contract_names":  sortedSetValues(annotation["_contract_names"].(map[string]struct{})),
			"contract_levels": sortedSetValues(annotation["_contract_levels"].(map[string]struct{})),
			"change_policies": sortedSetValues(annotation["_change_policies"].(map[string]struct{})),
			"sensitivity":     annotation["sensitivity"],
			"is_protected":    annotation["is_protected"],
			"protection_kind": annotation["protection_kind"],
		})
	}
	sort.Slice(items, func(i, j int) bool {
		leftKind, _ := items[i]["target_kind"].(string)
		rightKind, _ := items[j]["target_kind"].(string)
		if leftKind != rightKind {
			return leftKind < rightKind
		}
		leftName, _ := items[i]["target_name"].(string)
		rightName, _ := items[j]["target_name"].(string)
		return leftName < rightName
	})
	return items
}

func completeCoverage() map[string]any {
	return map[string]any{
		"confidence":            1.0,
		"state":                 "complete",
		"unresolved_references": []string{},
	}
}

func metadataField(document map[string]any, key string) any {
	metadata, _ := document["metadata"].(map[string]any)
	if metadata == nil {
		return nil
	}
	return metadata[key]
}

func jsonObjectSlice(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	results := make([]map[string]any, 0, len(items))
	for _, item := range items {
		mapping, ok := item.(map[string]any)
		if ok {
			results = append(results, mapping)
		}
	}
	return results
}

func jsonStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	results := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(fmt.Sprint(item))
		if trimmed != "" {
			results = append(results, trimmed)
		}
	}
	return results
}

func sortJSONRecords(items []map[string]any) []map[string]any {
	sort.Slice(items, func(i, j int) bool {
		leftName, _ := items[i]["name"].(string)
		rightName, _ := items[j]["name"].(string)
		return leftName < rightName
	})
	return items
}

func sortedJSONRecords(items map[string]map[string]any) []map[string]any {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	results := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		results = append(results, items[key])
	}
	return results
}

func sortRelationships(items []map[string]any) []map[string]any {
	sort.Slice(items, func(i, j int) bool {
		leftType, _ := items[i]["type"].(string)
		rightType, _ := items[j]["type"].(string)
		if leftType != rightType {
			return leftType < rightType
		}
		leftSource, _ := items[i]["source_name"].(string)
		rightSource, _ := items[j]["source_name"].(string)
		if leftSource != rightSource {
			return leftSource < rightSource
		}
		leftTarget, _ := items[i]["target_name"].(string)
		rightTarget, _ := items[j]["target_name"].(string)
		return leftTarget < rightTarget
	})
	return items
}

func sortedSetValues(values map[string]struct{}) []string {
	items := make([]string, 0, len(values))
	for value := range values {
		items = append(items, value)
	}
	sort.Strings(items)
	return items
}

func defaultString(value any, fallback string) string {
	trimmed := strings.TrimSpace(fmt.Sprint(value))
	if trimmed == "" || trimmed == "<nil>" {
		return fallback
	}
	return trimmed
}

func optionalString(value any) any {
	trimmed := strings.TrimSpace(fmt.Sprint(value))
	if trimmed == "" || trimmed == "<nil>" {
		return nil
	}
	return trimmed
}

func jsonBool(value any) bool {
	boolean, ok := value.(bool)
	return ok && boolean
}

func nonEmptyStrings(values ...string) []string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" && trimmed != "<nil>" {
			items = append(items, trimmed)
		}
	}
	return items
}
