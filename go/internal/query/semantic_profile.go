package query

// buildEntitySemanticProfile promotes the highest-signal structured semantics
// already present in parser metadata into a stable query-surface bundle.
func buildEntitySemanticProfile(entity map[string]any) map[string]any {
	metadata, _ := entity["metadata"].(map[string]any)
	if len(metadata) == 0 {
		return nil
	}

	label := primaryEntityLabel(entity)
	language := StringVal(entity, "language")

	profile := map[string]any{}
	signals := make([]string, 0, 6)

	decorators := metadataStringSlice(metadata, "decorators")
	if len(decorators) > 0 {
		profile["decorators"] = decorators
		signals = append(signals, "decorators")
	}

	if async := boolValue(metadata["async"]); async {
		profile["async"] = true
		signals = append(signals, "async")
	}
	typeParameters := metadataStringSlice(metadata, "type_parameters")
	if len(typeParameters) > 0 {
		profile["type_parameters"] = typeParameters
		signals = append(signals, "type_parameters")
	}
	if typeAliasKind, _ := metadata["type_alias_kind"].(string); typeAliasKind != "" {
		profile["type_alias_kind"] = typeAliasKind
		signals = append(signals, typeAliasKind)
	}
	if jsxFragment := boolValue(metadata["jsx_fragment_shorthand"]); jsxFragment {
		profile["jsx_fragment_shorthand"] = true
		signals = append(signals, "jsx_fragment")
	}
	if componentAssertion, _ := metadata["component_type_assertion"].(string); componentAssertion != "" {
		profile["component_type_assertion"] = componentAssertion
		signals = append(signals, "component_type_assertion")
	}
	if componentWrapper, _ := metadata["component_wrapper_kind"].(string); componentWrapper != "" {
		profile["component_wrapper_kind"] = componentWrapper
		signals = append(signals, "component_wrapper_kind")
	}
	if mergeGroup, _ := metadata["declaration_merge_group"].(string); mergeGroup != "" {
		if mergeCount := IntVal(metadata, "declaration_merge_count"); mergeCount > 1 {
			profile["declaration_merge"] = true
			profile["declaration_merge_group"] = mergeGroup
			profile["declaration_merge_count"] = mergeCount
			profile["declaration_merge_kinds"] = metadataStringSlice(metadata, "declaration_merge_kinds")
			signals = append(signals, "declaration_merge")
		}
	}
	if source, _ := metadata["source"].(string); source != "" {
		profile["source"] = source
		signals = append(signals, "source")
	}
	if terraformSource, _ := metadata["terraform_source"].(string); terraformSource != "" {
		profile["terraform_source"] = terraformSource
		signals = append(signals, "terraform_source")
	}
	if configPath, _ := metadata["config_path"].(string); configPath != "" {
		profile["config_path"] = configPath
		signals = append(signals, "config_path")
	}
	if includes := metadataStringSlice(metadata, "includes"); len(includes) > 0 {
		profile["includes"] = includes
		signals = append(signals, "includes")
	}
	if inputs := metadataStringSlice(metadata, "inputs"); len(inputs) > 0 {
		profile["inputs"] = inputs
		signals = append(signals, "inputs")
	}
	if locals := metadataStringSlice(metadata, "locals"); len(locals) > 0 {
		profile["locals"] = locals
		signals = append(signals, "locals")
	}
	if deploymentName, _ := metadata["deployment_name"].(string); deploymentName != "" {
		profile["deployment_name"] = deploymentName
		signals = append(signals, "deployment_name")
	}
	if repoName, _ := metadata["repo_name"].(string); repoName != "" {
		profile["repo_name"] = repoName
		signals = append(signals, "repo_name")
	}
	if createDeploy, _ := metadata["create_deploy"].(string); createDeploy != "" {
		profile["create_deploy"] = createDeploy
		signals = append(signals, "create_deploy")
	}
	if clusterName, _ := metadata["cluster_name"].(string); clusterName != "" {
		profile["cluster_name"] = clusterName
		signals = append(signals, "cluster_name")
	}
	if zoneID, _ := metadata["zone_id"].(string); zoneID != "" {
		profile["zone_id"] = zoneID
		signals = append(signals, "zone_id")
	}
	if deployEntryPoint, _ := metadata["deploy_entry_point"].(string); deployEntryPoint != "" {
		profile["deploy_entry_point"] = deployEntryPoint
		signals = append(signals, "deploy_entry_point")
	}

	jsSemantics := ExtractJavaScriptSemantics(metadata)
	if jsSemantics.MethodKind != "" {
		profile["method_kind"] = jsSemantics.MethodKind
		signals = append(signals, "method_kind")
	}
	if jsSemantics.Docstring != "" {
		profile["docstring"] = jsSemantics.Docstring
		signals = append(signals, "docstring")
	}

	if framework, ok := metadata["framework"].(string); ok && framework != "" {
		profile["framework"] = framework
		signals = append(signals, "framework")
	}
	if moduleKind, _ := metadata["module_kind"].(string); moduleKind == "namespace" {
		profile["namespace"] = true
		signals = append(signals, "namespace")
	}

	if language == "elixir" {
		if moduleKind, _ := metadata["module_kind"].(string); moduleKind == "protocol" {
			profile["protocol"] = true
			signals = append(signals, "protocol")
		}
		if semanticKind, _ := metadata["semantic_kind"].(string); semanticKind == "guard" {
			profile["guard"] = true
			signals = append(signals, "guard")
		}
		if attributeKind, _ := metadata["attribute_kind"].(string); attributeKind == "module_attribute" {
			profile["module_attribute"] = true
			signals = append(signals, "module_attribute")
		}
		if moduleKind, _ := metadata["module_kind"].(string); moduleKind == "protocol_implementation" {
			profile["protocol_implementation"] = true
			signals = append(signals, "protocol_implementation")
		}
	}

	if label == "Annotation" {
		if kind, _ := metadata["kind"].(string); kind == "applied" {
			profile["annotation"] = true
			signals = append(signals, "annotation")
			if targetKind, _ := metadata["target_kind"].(string); targetKind != "" {
				profile["annotation_target_kind"] = targetKind
			}
		}
	}

	pythonProfile := PythonSemanticProfileFromMetadata(label, metadata)
	if pythonProfile.Lambda {
		profile["lambda"] = true
		signals = append(signals, "lambda")
	}
	if pythonProfile.Generator {
		profile["generator"] = true
		signals = append(signals, "generator")
	}
	if pythonProfile.Metaclass != "" {
		profile["metaclass"] = pythonProfile.Metaclass
		signals = append(signals, "metaclass")
	}
	if pythonProfile.TypeAnnotationCount > 0 {
		profile["type_annotation_count"] = pythonProfile.TypeAnnotationCount
	}
	if len(pythonProfile.TypeAnnotationKinds) > 0 {
		profile["type_annotation_kinds"] = pythonProfile.TypeAnnotationKinds
	}
	if pythonProfile.AnnotationKind != "" {
		profile["annotation_kind"] = pythonProfile.AnnotationKind
		signals = append(signals, "annotation_kind")
	}
	if pythonProfile.Context != "" {
		profile["context"] = pythonProfile.Context
	}
	if pythonProfile.TypeAnnotation {
		profile["type_annotation"] = true
		signals = append(signals, "type_annotation")
	}

	if len(signals) == 0 {
		return nil
	}

	profile["surface_kind"] = semanticSurfaceKind(label, language, profile, pythonProfile)
	profile["signals"] = signals
	return profile
}

func semanticSurfaceKind(
	label string,
	language string,
	profile map[string]any,
	pythonProfile PythonSemanticProfile,
) string {
	if _, ok := profile["component_wrapper_kind"].(string); ok {
		return "component_wrapper"
	}
	if _, ok := profile["component_type_assertion"].(string); ok {
		return "component_type_assertion"
	}
	if framework, ok := profile["framework"].(string); ok && framework != "" && label == "Component" {
		return "framework_component"
	}
	if aliasKind, _ := profile["type_alias_kind"].(string); aliasKind != "" {
		switch aliasKind {
		case "mapped_type":
			return "mapped_type_alias"
		case "conditional_type":
			return "conditional_type_alias"
		}
	}
	if _, ok := profile["namespace"].(bool); ok && label == "Module" {
		return "namespace_module"
	}
	if _, ok := profile["declaration_merge"].(bool); ok {
		return "declaration_merge"
	}
	if _, ok := profile["source"].(string); ok && label == "TerraformModule" {
		return "terraform_module_source"
	}
	if _, ok := profile["terraform_source"].(string); ok && label == "TerragruntConfig" {
		return "terragrunt_config"
	}
	if _, ok := profile["config_path"].(string); ok && label == "TerragruntDependency" {
		return "terragrunt_dependency"
	}
	if _, ok := profile["protocol"].(bool); ok {
		return "protocol"
	}
	if _, ok := profile["guard"].(bool); ok {
		return "guard"
	}
	if _, ok := profile["module_attribute"].(bool); ok {
		return "module_attribute"
	}
	if _, ok := profile["protocol_implementation"].(bool); ok {
		return "protocol_implementation"
	}
	if _, ok := profile["annotation"].(bool); ok && label == "Annotation" {
		return "applied_annotation"
	}
	if language == "python" && (pythonProfile.HasSignals() || pythonProfile.Docstring != "") {
		return pythonProfile.SurfaceKind()
	}
	if _, ok := profile["type_annotation"].(bool); ok {
		if kind, _ := profile["annotation_kind"].(string); kind != "" {
			return kind + "_type_annotation"
		}
		return "type_annotation"
	}
	if count, ok := profile["type_annotation_count"].(int); ok && count > 0 {
		return "type_annotation"
	}
	if _, ok := profile["type_parameters"].([]string); ok {
		return "generic_declaration"
	}
	if language == "javascript" {
		if _, ok := profile["method_kind"].(string); ok {
			return "javascript_method"
		}
	}
	if _, decorated := profile["decorators"].([]string); decorated {
		if _, async := profile["async"].(bool); async {
			return "decorated_async_entity"
		}
		return "decorated_entity"
	}
	if _, async := profile["async"].(bool); async {
		return "async_function"
	}
	if _, ok := profile["method_kind"].(string); ok {
		return "method"
	}
	if _, ok := profile["docstring"].(string); ok {
		return "documented_entity"
	}
	return "semantic_entity"
}
