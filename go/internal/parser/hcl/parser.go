package hcl

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/eshu-hq/eshu/go/internal/terraformschema"

	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// Parse extracts Terraform and Terragrunt payload buckets from one HCL file.
func Parse(
	path string,
	isDependency bool,
	options shared.Options,
) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL(source, path)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse hcl file %q: %s", path, diags.Error())
	}

	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("parse hcl file %q: unsupported body type %T", path, file.Body)
	}

	payload := hclBasePayload(path, isDependency)
	if isTerraformLockFile(path) {
		for _, row := range parseTerraformLockProviders(body, source, path) {
			shared.AppendBucket(payload, "terraform_lock_providers", row)
		}
	} else if strings.EqualFold(filepath.Base(path), "terragrunt.hcl") {
		shared.AppendBucket(payload, "terragrunt_configs", parseTerragruntConfig(body, source, path))
		for _, row := range parseTerragruntModuleSources(body, source, path) {
			shared.AppendBucket(payload, "terraform_modules", row)
		}
		for _, row := range parseTerragruntDependencies(body, source, path) {
			shared.AppendBucket(payload, "terragrunt_dependencies", row)
		}
		for _, row := range parseTerragruntLocals(body, source, path) {
			shared.AppendBucket(payload, "terragrunt_locals", row)
		}
		for _, row := range parseTerragruntInputs(body, source, path) {
			shared.AppendBucket(payload, "terragrunt_inputs", row)
		}
		for _, row := range parseTerragruntRemoteStates(body, source, path) {
			shared.AppendBucket(payload, "terragrunt_remote_states", row)
		}
		includeRows, includeWarnings := resolveTerragruntRemoteStateFromIncludes(body, path)
		for _, row := range includeRows {
			shared.AppendBucket(payload, "terragrunt_remote_states", row)
		}
		for _, row := range includeWarnings {
			shared.AppendBucket(payload, "terragrunt_include_warnings", row)
		}
	} else {
		parseTerraformBlocks(payload, body, source, path)
		for _, row := range parseTerraformBackends(body, source, path) {
			shared.AppendBucket(payload, "terraform_backends", row)
		}
	}

	shared.SortNamedBucket(payload, "terraform_blocks")
	shared.SortNamedBucket(payload, "terraform_resources")
	shared.SortNamedBucket(payload, "terraform_variables")
	shared.SortNamedBucket(payload, "terraform_outputs")
	shared.SortNamedBucket(payload, "terraform_modules")
	shared.SortNamedBucket(payload, "terraform_data_sources")
	shared.SortNamedBucket(payload, "terraform_providers")
	shared.SortNamedBucket(payload, "terraform_locals")
	shared.SortNamedBucket(payload, "terraform_backends")
	shared.SortNamedBucket(payload, "terraform_imports")
	shared.SortNamedBucket(payload, "terraform_moved_blocks")
	shared.SortNamedBucket(payload, "terraform_removed_blocks")
	shared.SortNamedBucket(payload, "terraform_checks")
	shared.SortNamedBucket(payload, "terraform_lock_providers")
	shared.SortNamedBucket(payload, "terragrunt_configs")
	shared.SortNamedBucket(payload, "terragrunt_dependencies")
	shared.SortNamedBucket(payload, "terragrunt_locals")
	shared.SortNamedBucket(payload, "terragrunt_inputs")
	shared.SortNamedBucket(payload, "terragrunt_remote_states")
	shared.SortNamedBucket(payload, "terragrunt_include_warnings")
	if options.IndexSource {
		payload["source"] = string(source)
	}
	return payload, nil
}

func hclBasePayload(path string, isDependency bool) map[string]any {
	payload := shared.BasePayload(path, "hcl", isDependency)
	payload["terraform_blocks"] = []map[string]any{}
	payload["terraform_resources"] = []map[string]any{}
	payload["terraform_variables"] = []map[string]any{}
	payload["terraform_outputs"] = []map[string]any{}
	payload["terraform_modules"] = []map[string]any{}
	payload["terraform_data_sources"] = []map[string]any{}
	payload["terraform_providers"] = []map[string]any{}
	payload["terraform_locals"] = []map[string]any{}
	payload["terraform_backends"] = []map[string]any{}
	payload["terraform_imports"] = []map[string]any{}
	payload["terraform_moved_blocks"] = []map[string]any{}
	payload["terraform_removed_blocks"] = []map[string]any{}
	payload["terraform_checks"] = []map[string]any{}
	payload["terraform_lock_providers"] = []map[string]any{}
	payload["terragrunt_configs"] = []map[string]any{}
	payload["terragrunt_dependencies"] = []map[string]any{}
	payload["terragrunt_locals"] = []map[string]any{}
	payload["terragrunt_inputs"] = []map[string]any{}
	payload["terragrunt_remote_states"] = []map[string]any{}
	return payload
}

func parseTerraformBlocks(payload map[string]any, body *hclsyntax.Body, source []byte, path string) {
	providerMetadata := collectRequiredProviders(body, source)

	for _, block := range body.Blocks {
		switch block.Type {
		case "resource":
			if len(block.Labels) < 2 {
				continue
			}
			row := map[string]any{
				"name":          block.Labels[0] + "." + block.Labels[1],
				"line_number":   block.TypeRange.Start.Line,
				"resource_type": block.Labels[0],
				"resource_name": block.Labels[1],
				"path":          path,
				"lang":          "hcl",
			}
			addTerraformTypeClassification(row, block.Labels[0])
			if countAttr := block.Body.Attributes["count"]; countAttr != nil {
				count := strings.TrimSpace(sourceRange(source, countAttr.Expr.Range()))
				if count != "" {
					row["count"] = count
				}
			}
			if forEachAttr := block.Body.Attributes["for_each"]; forEachAttr != nil {
				forEach := strings.TrimSpace(sourceRange(source, forEachAttr.Expr.Range()))
				if forEach != "" {
					row["for_each"] = forEach
				}
			}
			if known, unknown := extractResourceAttributes(block, source); len(known) > 0 || len(unknown) > 0 {
				if len(known) > 0 {
					row["attributes"] = known
				}
				if len(unknown) > 0 {
					row["unknown_attributes"] = unknown
				}
			}
			shared.AppendBucket(payload, "terraform_resources", row)
		case "variable":
			if len(block.Labels) == 0 {
				continue
			}
			shared.AppendBucket(payload, "terraform_variables", map[string]any{
				"name":        block.Labels[0],
				"line_number": block.TypeRange.Start.Line,
				"var_type":    attributeValue(block.Body.Attributes["type"], source),
				"default":     attributeValue(block.Body.Attributes["default"], source),
				"description": attributeValue(block.Body.Attributes["description"], source),
				"path":        path,
				"lang":        "hcl",
			})
		case "output":
			if len(block.Labels) == 0 {
				continue
			}
			shared.AppendBucket(payload, "terraform_outputs", map[string]any{
				"name":        block.Labels[0],
				"line_number": block.TypeRange.Start.Line,
				"description": attributeValue(block.Body.Attributes["description"], source),
				"value":       attributeValue(block.Body.Attributes["value"], source),
				"path":        path,
				"lang":        "hcl",
			})
		case "module":
			if len(block.Labels) == 0 {
				continue
			}
			row := map[string]any{
				"name":            block.Labels[0],
				"line_number":     block.TypeRange.Start.Line,
				"source":          attributeValue(block.Body.Attributes["source"], source),
				"version":         attributeValue(block.Body.Attributes["version"], source),
				"deployment_name": attributeValue(block.Body.Attributes["name"], source),
				"repo_name":       attributeValue(block.Body.Attributes["repo_name"], source),
				"create_deploy":   attributeValue(block.Body.Attributes["create_deploy"], source),
				"cluster_name":    attributeValue(block.Body.Attributes["cluster_name"], source),
				"zone_id":         attributeValue(block.Body.Attributes["zone_id"], source),
				"path":            path,
				"lang":            "hcl",
			}
			if deployConf := objectAttributeMap(block.Body.Attributes["deploy_conf"], source); len(deployConf) > 0 {
				row["deploy_entry_point"] = deployConf["ENTRY_POINT"]
			}
			shared.AppendBucket(payload, "terraform_modules", row)
		case "data":
			if len(block.Labels) < 2 {
				continue
			}
			row := map[string]any{
				"name":        block.Labels[0] + "." + block.Labels[1],
				"line_number": block.TypeRange.Start.Line,
				"data_type":   block.Labels[0],
				"data_name":   block.Labels[1],
				"path":        path,
				"lang":        "hcl",
			}
			addTerraformTypeClassification(row, block.Labels[0])
			shared.AppendBucket(payload, "terraform_data_sources", row)
		case "provider":
			if len(block.Labels) == 0 {
				continue
			}
			metadata := providerMetadata[block.Labels[0]]
			shared.AppendBucket(payload, "terraform_providers", map[string]any{
				"name":        block.Labels[0],
				"line_number": block.TypeRange.Start.Line,
				"source":      metadata["source"],
				"version":     metadata["version"],
				"alias":       attributeValue(block.Body.Attributes["alias"], source),
				"region":      attributeValue(block.Body.Attributes["region"], source),
				"path":        path,
				"lang":        "hcl",
			})
		case "locals":
			for _, item := range sortedAttributes(block.Body.Attributes) {
				name := item.name
				attribute := item.attribute
				shared.AppendBucket(payload, "terraform_locals", map[string]any{
					"name":        name,
					"line_number": attribute.NameRange.Start.Line,
					"value":       expressionText(attribute.Expr, source),
					"path":        path,
					"lang":        "hcl",
				})
			}
		case "terraform":
			requiredProviders, requiredProviderSources, requiredProviderCount := collectRequiredProviderSummaries(providerMetadata)
			shared.AppendBucket(payload, "terraform_blocks", map[string]any{
				"name":                      "terraform",
				"line_number":               block.TypeRange.Start.Line,
				"required_providers":        requiredProviders,
				"required_provider_sources": requiredProviderSources,
				"required_provider_count":   requiredProviderCount,
				"path":                      path,
				"lang":                      "hcl",
			})
		case "import":
			shared.AppendBucket(payload, "terraform_imports", parseTerraformImportBlock(block, source, path))
		case "moved":
			shared.AppendBucket(payload, "terraform_moved_blocks", parseTerraformMovedBlock(block, source, path))
		case "removed":
			shared.AppendBucket(payload, "terraform_removed_blocks", parseTerraformRemovedBlock(block, source, path))
		case "check":
			if len(block.Labels) == 0 {
				continue
			}
			shared.AppendBucket(payload, "terraform_checks", parseTerraformCheckBlock(block, path))
		}
	}
}

func addTerraformTypeClassification(row map[string]any, terraformType string) {
	provider, _, ok := strings.Cut(strings.TrimSpace(terraformType), "_")
	if !ok || provider == "" {
		return
	}
	row["provider"] = provider
	if service := terraformschema.ClassifyResourceService(terraformType); service != "" {
		row["resource_service"] = service
	}
	row["resource_category"] = terraformschema.ClassifyResourceCategory(terraformType)
}

func collectRequiredProviders(body *hclsyntax.Body, source []byte) map[string]map[string]string {
	result := make(map[string]map[string]string)
	for _, block := range body.Blocks {
		if block.Type != "terraform" {
			continue
		}
		for _, child := range block.Body.Blocks {
			if child.Type != "required_providers" {
				continue
			}
			for _, item := range sortedAttributes(child.Body.Attributes) {
				result[item.name] = objectAttributeMap(item.attribute, source)
			}
		}
	}
	return result
}

func collectRequiredProviderSummaries(metadata map[string]map[string]string) (string, string, int) {
	if len(metadata) == 0 {
		return "", "", 0
	}

	names := make([]string, 0, len(metadata))
	sources := make([]string, 0, len(metadata))
	for name, provider := range metadata {
		names = append(names, name)
		if source := strings.TrimSpace(provider["source"]); source != "" {
			sources = append(sources, fmt.Sprintf("%s=%s", name, source))
		}
	}
	sort.Strings(names)
	sort.Strings(sources)
	return strings.Join(names, ","), strings.Join(sources, ","), len(metadata)
}

func parseTerragruntConfig(body *hclsyntax.Body, source []byte, path string) map[string]any {
	row := map[string]any{
		"name":        strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		"line_number": 1,
		"path":        path,
		"lang":        "hcl",
	}

	includeNames := make([]string, 0)
	localNames := make([]string, 0)
	for _, block := range body.Blocks {
		switch block.Type {
		case "terraform":
			row["terraform_source"] = attributeValue(block.Body.Attributes["source"], source)
		case "include":
			includeNames = append(includeNames, block.Labels...)
		case "locals":
			for name := range block.Body.Attributes {
				localNames = append(localNames, name)
			}
		}
	}
	sort.Strings(includeNames)
	sort.Strings(localNames)
	row["includes"] = strings.Join(includeNames, ",")
	row["locals"] = strings.Join(localNames, ",")
	row["inputs"] = strings.Join(objectAttributeKeys(body.Attributes["inputs"], source), ",")
	helperPaths := parseTerragruntHelperPaths(source, path)
	if len(helperPaths.includePaths) > 0 {
		row["include_paths"] = strings.Join(helperPaths.includePaths, ",")
	}
	if len(helperPaths.readConfigPaths) > 0 {
		row["read_config_paths"] = strings.Join(helperPaths.readConfigPaths, ",")
	}
	if len(helperPaths.findInParentFoldersPaths) > 0 {
		row["find_in_parent_folders_paths"] = strings.Join(helperPaths.findInParentFoldersPaths, ",")
	}
	if len(helperPaths.localConfigAssetPaths) > 0 {
		row["local_config_asset_paths"] = strings.Join(helperPaths.localConfigAssetPaths, ",")
	}
	return row
}

func parseTerragruntModuleSources(body *hclsyntax.Body, source []byte, path string) []map[string]any {
	rows := make([]map[string]any, 0, 1)
	for _, block := range body.Blocks {
		if block.Type != "terraform" {
			continue
		}
		moduleSource := attributeValue(block.Body.Attributes["source"], source)
		if moduleSource == "" {
			continue
		}
		rows = append(rows, map[string]any{
			"name":        strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
			"line_number": block.TypeRange.Start.Line,
			"source":      moduleSource,
			"path":        path,
			"lang":        "hcl",
		})
	}
	return rows
}

func parseTerragruntDependencies(body *hclsyntax.Body, source []byte, path string) []map[string]any {
	rows := make([]map[string]any, 0)
	for _, block := range body.Blocks {
		if block.Type != "dependency" || len(block.Labels) == 0 {
			continue
		}
		row := map[string]any{
			"name":        block.Labels[0],
			"line_number": block.TypeRange.Start.Line,
			"path":        path,
			"lang":        "hcl",
		}
		if configPath := attributeValue(block.Body.Attributes["config_path"], source); configPath != "" {
			row["config_path"] = configPath
		}
		rows = append(rows, row)
	}
	return rows
}

func parseTerragruntLocals(body *hclsyntax.Body, source []byte, path string) []map[string]any {
	rows := make([]map[string]any, 0)
	for _, block := range body.Blocks {
		if block.Type != "locals" {
			continue
		}
		for _, item := range sortedAttributes(block.Body.Attributes) {
			rows = append(rows, map[string]any{
				"name":        item.name,
				"line_number": item.attribute.NameRange.Start.Line,
				"value":       expressionText(item.attribute.Expr, source),
				"path":        path,
				"lang":        "hcl",
			})
		}
	}
	return rows
}

func parseTerragruntInputs(body *hclsyntax.Body, source []byte, path string) []map[string]any {
	inputs := body.Attributes["inputs"]
	if inputs == nil {
		return nil
	}

	objectExpr, ok := inputs.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil
	}

	rows := make([]map[string]any, 0, len(objectExpr.Items))
	for _, item := range objectExpr.Items {
		name := strings.Trim(expressionText(item.KeyExpr, source), `"`)
		if strings.TrimSpace(name) == "" {
			continue
		}
		rows = append(rows, map[string]any{
			"name":        name,
			"line_number": item.KeyExpr.Range().Start.Line,
			"value":       expressionText(item.ValueExpr, source),
			"path":        path,
			"lang":        "hcl",
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		left, _ := rows[i]["name"].(string)
		right, _ := rows[j]["name"].(string)
		return left < right
	})
	return rows
}

type namedAttribute struct {
	name      string
	attribute *hclsyntax.Attribute
}
