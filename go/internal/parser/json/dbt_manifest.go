package json

import (
	"fmt"
	"sort"
	"strings"
)

func isDBTManifestDocument(document map[string]any, filename string) bool {
	lowered := strings.ToLower(filename)
	metadata, _ := document["metadata"].(map[string]any)
	_, nodesOK := document["nodes"].(map[string]any)
	_, sourcesOK := document["sources"].(map[string]any)
	return (lowered == "manifest.json" || lowered == "dbt_manifest.json") && metadata != nil && nodesOK && sourcesOK
}

func applyDBTManifestDocument(payload map[string]any, document map[string]any, lineageExtractor LineageExtractor) {
	sources, _ := document["sources"].(map[string]any)
	nodes, _ := document["nodes"].(map[string]any)
	macros, _ := document["macros"].(map[string]any)
	outputAssets := make(map[string]map[string]any)
	sourceAssets := make(map[string]map[string]any)
	macroAssets := make(map[string]map[string]any)
	assetColumns := make(map[string][]string)
	dataColumns := make(map[string]map[string]any)
	analyticsModels := make([]map[string]any, 0)
	relationships := make([]map[string]any, 0)
	unresolved := make([]map[string]string, 0)

	for _, uniqueID := range sortedMapKeys(sources) {
		sourceNode, ok := sources[uniqueID].(map[string]any)
		if !ok {
			continue
		}
		asset := dbtAssetRecord(sourceNode)
		outputAssets[uniqueID] = asset
		sourceAssets[uniqueID] = asset
		assetName, _ := asset["name"].(string)
		assetColumns[assetName] = dbtColumnNamesForAsset(sourceNode)
		for _, column := range dbtColumnRecordsForAsset(sourceNode, assetName) {
			columnName, _ := column["name"].(string)
			dataColumns[columnName] = column
		}
	}

	for _, uniqueID := range sortedMapKeys(macros) {
		macroNode, ok := macros[uniqueID].(map[string]any)
		if !ok {
			continue
		}
		macroAsset := dbtAssetRecord(macroNode)
		outputAssets[uniqueID] = macroAsset
		macroAssets[uniqueID] = macroAsset
	}

	for _, uniqueID := range sortedMapKeys(nodes) {
		node, ok := nodes[uniqueID].(map[string]any)
		if !ok || fmt.Sprint(node["resource_type"]) != "model" {
			continue
		}
		modelAsset := dbtAssetRecord(node)
		outputAssets[uniqueID] = modelAsset
		modelAssetName, _ := modelAsset["name"].(string)
		modelColumnNames := dbtColumnNamesForAsset(node)
		assetColumns[modelAssetName] = modelColumnNames
		for _, column := range dbtColumnRecordsForAsset(node, modelAssetName) {
			columnName, _ := column["name"].(string)
			dataColumns[columnName] = column
		}

		modelName := defaultString(node["name"], uniqueID)
		relationships = append(relationships, map[string]any{
			"type":        "COMPILES_TO",
			"source_id":   "analytics-model:" + uniqueID,
			"source_name": modelName,
			"target_id":   modelAsset["id"],
			"target_name": modelAssetName,
			"confidence":  1.0,
		})

		for _, dependency := range dbtDependencyAssets(node, outputAssets, sourceAssets, nodes, sources) {
			relationships = append(relationships, map[string]any{
				"type":        "ASSET_DERIVES_FROM",
				"source_id":   modelAsset["id"],
				"source_name": modelAssetName,
				"target_id":   dependency["id"],
				"target_name": dependency["name"],
				"confidence":  0.99,
			})
		}

		lineage := CompiledModelLineage{}
		if lineageExtractor != nil {
			lineage = lineageExtractor(fmt.Sprint(node["compiled_code"]), modelName, assetColumns)
		}
		modelColumnNameSet := make(map[string]struct{}, len(modelColumnNames))
		for _, columnName := range modelColumnNames {
			modelColumnNameSet[columnName] = struct{}{}
		}
		unresolved = append(unresolved, lineage.UnresolvedReferences...)
		for _, columnLineage := range lineage.ColumnLineage {
			if _, ok := modelColumnNameSet[columnLineage.OutputColumn]; !ok {
				modelColumnNames = append(modelColumnNames, columnLineage.OutputColumn)
				modelColumnNameSet[columnLineage.OutputColumn] = struct{}{}
			}
			outputColumnName := modelAssetName + "." + columnLineage.OutputColumn
			if _, ok := dataColumns[outputColumnName]; !ok {
				dataColumns[outputColumnName] = map[string]any{
					"id":          "data-column:" + outputColumnName,
					"asset_name":  modelAssetName,
					"name":        outputColumnName,
					"line_number": jsonInt(node["line_number"], 1),
				}
			}
			for _, sourceColumnName := range columnLineage.SourceColumns {
				relationship := map[string]any{
					"type":        "COLUMN_DERIVES_FROM",
					"source_id":   "data-column:" + outputColumnName,
					"source_name": outputColumnName,
					"target_id":   "data-column:" + sourceColumnName,
					"target_name": sourceColumnName,
					"confidence":  0.95,
				}
				if columnLineage.TransformKind != "" {
					relationship["transform_kind"] = columnLineage.TransformKind
				}
				if columnLineage.TransformExpression != "" {
					relationship["transform_expression"] = columnLineage.TransformExpression
				}
				relationships = append(relationships, relationship)
			}
		}
		for _, dependency := range dbtMacroDependencyAssets(node, outputAssets, macroAssets) {
			relationships = append(relationships, map[string]any{
				"type":        "USES_MACRO",
				"source_id":   modelAsset["id"],
				"source_name": modelAssetName,
				"target_id":   dependency["id"],
				"target_name": dependency["name"],
				"confidence":  0.99,
			})
		}
		assetColumns[modelAssetName] = modelColumnNames
		unresolvedSummary := dbtUnresolvedReferenceSummary(lineage.UnresolvedReferences)
		analyticsModels = append(analyticsModels, map[string]any{
			"id":                               "analytics-model:" + uniqueID,
			"name":                             modelName,
			"asset_name":                       modelAssetName,
			"line_number":                      jsonInt(node["line_number"], 1),
			"path":                             defaultString(node["compiled_path"], fmt.Sprint(node["path"])),
			"compiled_path":                    defaultString(node["compiled_path"], fmt.Sprint(node["path"])),
			"materialization":                  jsonNestedString(node, "config", "materialized", "unknown"),
			"parse_state":                      ternaryState(len(lineage.UnresolvedReferences) > 0, "partial", "complete"),
			"confidence":                       ternaryFloat(len(lineage.UnresolvedReferences) > 0, 0.5, 1.0),
			"projection_count":                 lineage.ProjectionCount,
			"unresolved_reference_count":       unresolvedSummary["count"],
			"unresolved_reference_reasons":     unresolvedSummary["reasons"],
			"unresolved_reference_expressions": unresolvedSummary["expressions"],
		})
	}

	sort.Slice(analyticsModels, func(i, j int) bool {
		return analyticsModels[i]["name"].(string) < analyticsModels[j]["name"].(string)
	})
	payload["analytics_models"] = analyticsModels
	payload["data_assets"] = sortedJSONRecords(outputAssets)
	payload["data_columns"] = sortedJSONRecords(dataColumns)
	payload["data_relationships"] = sortRelationships(relationships)
	payload["data_intelligence_coverage"] = dbtCoverageSummary(analyticsModels, unresolved)
}

func dbtAssetRecord(node map[string]any) map[string]any {
	assetName := dbtAssetName(node)
	return map[string]any{
		"id":          "data-asset:" + assetName,
		"name":        assetName,
		"line_number": jsonInt(node["line_number"], 1),
		"database":    fmt.Sprint(node["database"]),
		"schema":      fmt.Sprint(node["schema"]),
		"kind":        defaultString(node["resource_type"], "asset"),
	}
}

func dbtAssetName(node map[string]any) string {
	if fmt.Sprint(node["resource_type"]) == "macro" {
		return strings.Join(nonEmptyStrings(
			fmt.Sprint(node["package_name"]),
			defaultString(node["name"], fmt.Sprint(node["unique_id"])),
		), ".")
	}
	if relationName := strings.TrimSpace(fmt.Sprint(node["relation_name"])); relationName != "" && relationName != "<nil>" {
		return relationName
	}
	return strings.Join(nonEmptyStrings(
		fmt.Sprint(node["database"]),
		fmt.Sprint(node["schema"]),
		defaultString(node["identifier"], defaultString(node["alias"], fmt.Sprint(node["name"]))),
	), ".")
}

func dbtColumnRecordsForAsset(node map[string]any, assetName string) []map[string]any {
	columns, _ := node["columns"].(map[string]any)
	keys := sortedMapKeys(columns)
	results := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		column, _ := columns[key].(map[string]any)
		columnName := defaultString(column["name"], key)
		qualifiedName := assetName + "." + columnName
		results = append(results, map[string]any{
			"id":          "data-column:" + qualifiedName,
			"asset_name":  assetName,
			"name":        qualifiedName,
			"line_number": jsonInt(node["line_number"], 1),
		})
	}
	return results
}

func dbtColumnNamesForAsset(node map[string]any) []string {
	columns, _ := node["columns"].(map[string]any)
	keys := sortedMapKeys(columns)
	results := make([]string, 0, len(keys))
	for _, key := range keys {
		column, _ := columns[key].(map[string]any)
		results = append(results, defaultString(column["name"], key))
	}
	return results
}

func dbtDependencyAssets(node map[string]any, outputAssets map[string]map[string]any, sourceAssets map[string]map[string]any, allNodes map[string]any, allSources map[string]any) []map[string]any {
	dependencies := make(map[string]map[string]any)
	dependsOn, _ := node["depends_on"].(map[string]any)
	for _, rawDependency := range jsonStringSlice(dependsOn["nodes"]) {
		asset := outputAssets[rawDependency]
		if asset == nil {
			asset = sourceAssets[rawDependency]
		}
		if asset == nil {
			if dependencyNode, ok := allNodes[rawDependency].(map[string]any); ok {
				asset = dbtAssetRecord(dependencyNode)
			} else if dependencySource, ok := allSources[rawDependency].(map[string]any); ok {
				asset = dbtAssetRecord(dependencySource)
			}
		}
		if asset == nil {
			continue
		}
		dependencies[asset["name"].(string)] = asset
	}
	return sortedJSONRecords(dependencies)
}

func dbtMacroDependencyAssets(node map[string]any, outputAssets map[string]map[string]any, macroAssets map[string]map[string]any) []map[string]any {
	dependencies := make(map[string]map[string]any)
	dependsOn, _ := node["depends_on"].(map[string]any)
	for _, rawDependency := range jsonStringSlice(dependsOn["macros"]) {
		asset := outputAssets[rawDependency]
		if asset == nil {
			asset = macroAssets[rawDependency]
		}
		if asset == nil {
			continue
		}
		dependencies[asset["name"].(string)] = asset
	}
	return sortedJSONRecords(dependencies)
}

func dbtCoverageSummary(analyticsModels []map[string]any, unresolved []map[string]string) map[string]any {
	if len(analyticsModels) == 0 {
		return map[string]any{
			"confidence":            0.0,
			"state":                 "failed",
			"unresolved_references": []any{},
		}
	}
	sum := 0.0
	for _, item := range analyticsModels {
		if confidence, ok := item["confidence"].(float64); ok {
			sum += confidence
		}
	}
	unresolvedPayload := make([]any, 0, len(unresolved))
	for _, item := range unresolved {
		unresolvedPayload = append(unresolvedPayload, map[string]any{
			"expression": item["expression"],
			"model_name": item["model_name"],
			"reason":     item["reason"],
		})
	}
	return map[string]any{
		"confidence":            float64(int((sum/float64(len(analyticsModels)))*100+0.5)) / 100,
		"state":                 ternaryState(len(unresolvedPayload) > 0, "partial", "complete"),
		"unresolved_references": unresolvedPayload,
	}
}

func dbtUnresolvedReferenceSummary(unresolved []map[string]string) map[string]any {
	reasonsSeen := make(map[string]struct{})
	expressionsSeen := make(map[string]struct{})
	reasons := make([]string, 0)
	expressions := make([]string, 0)
	for _, item := range unresolved {
		reason := strings.TrimSpace(item["reason"])
		expression := strings.TrimSpace(item["expression"])
		if reason != "" {
			if _, ok := reasonsSeen[reason]; !ok {
				reasonsSeen[reason] = struct{}{}
				reasons = append(reasons, reason)
			}
		}
		if expression != "" {
			if _, ok := expressionsSeen[expression]; !ok {
				expressionsSeen[expression] = struct{}{}
				expressions = append(expressions, expression)
			}
		}
	}
	return map[string]any{
		"count":       len(unresolved),
		"reasons":     reasons,
		"expressions": expressions,
	}
}

func jsonInt(value any, fallback int) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return fallback
	}
}

func jsonNestedString(document map[string]any, key string, nested string, fallback string) string {
	value, _ := document[key].(map[string]any)
	if value == nil {
		return fallback
	}
	return defaultString(value[nested], fallback)
}

func ternaryState(condition bool, left string, right string) string {
	if condition {
		return left
	}
	return right
}

func ternaryFloat(condition bool, left float64, right float64) float64 {
	if condition {
		return left
	}
	return right
}
