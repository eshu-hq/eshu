package json

import (
	"fmt"
	"sort"
	"strings"
)

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
