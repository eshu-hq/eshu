package query

import (
	"fmt"
	"strings"
)

func buildEntitySemanticSummary(entity map[string]any) string {
	metadata, _ := entity["metadata"].(map[string]any)
	if len(metadata) == 0 {
		return ""
	}

	label := primaryEntityLabel(entity)
	name := StringVal(entity, "name")
	language := StringVal(entity, "language")
	if label == "" || name == "" {
		return ""
	}

	if mergeSummary := typeScriptDeclarationMergeSummary(label, name, metadata); mergeSummary != "" {
		return mergeSummary
	}

	switch label {
	case "AnalyticsModel":
		assetName, _ := metadata["asset_name"].(string)
		materialization, _ := metadata["materialization"].(string)
		parseState, _ := metadata["parse_state"].(string)
		if assetName == "" {
			return ""
		}
		summary := fmt.Sprintf("%s %s compiles to %s", label, name, assetName)
		if materialization != "" {
			summary += fmt.Sprintf(" as a %s", materialization)
		}
		if parseState != "" {
			summary += fmt.Sprintf(" and has %s lineage coverage", parseState)
		}
		return summary + "."
	case "DataAsset":
		kind, _ := metadata["kind"].(string)
		database, _ := metadata["database"].(string)
		schema, _ := metadata["schema"].(string)
		switch {
		case kind != "" && database != "" && schema != "":
			return fmt.Sprintf("%s %s is a %s in %s.%s.", label, name, kind, database, schema)
		case kind != "":
			return fmt.Sprintf("%s %s is a %s.", label, name, kind)
		}
	case "TypeAlias":
		fragments := make([]string, 0, 2)
		if typeAliasKind, _ := metadata["type_alias_kind"].(string); typeAliasKind != "" {
			fragments = append(fragments, "is a "+humanizeSemanticValue(typeAliasKind))
		}
		if params := metadataStringSlice(metadata, "type_parameters"); len(params) > 0 {
			fragments = append(fragments, "declares type parameters "+strings.Join(params, ", "))
		}
		if len(fragments) > 0 {
			return fmt.Sprintf("%s %s %s.", label, name, joinSentenceFragments(fragments))
		}
	case "TypeAnnotation":
		typeName, _ := metadata["type"].(string)
		if typeName == "" {
			return ""
		}
		annotationKind, _ := metadata["annotation_kind"].(string)
		context, _ := metadata["context"].(string)
		switch annotationKind {
		case "parameter":
			if context != "" {
				return fmt.Sprintf("%s %s is a parameter annotation for %s with type %s.", label, name, context, typeName)
			}
		case "return":
			if context != "" {
				return fmt.Sprintf("%s %s is a return annotation for %s with type %s.", label, name, context, typeName)
			}
		}
		return fmt.Sprintf("%s %s is annotated as %s.", label, name, typeName)
	case "Module":
		docstring, _ := metadata["docstring"].(string)
		if docstring != "" {
			return fmt.Sprintf("%s %s is documented as %q.", label, name, docstring)
		}
	case "TerraformBlock":
		requiredProviders, _ := metadata["required_providers"].(string)
		if requiredProviders == "" {
			return ""
		}
		return fmt.Sprintf("%s %s requires providers %s.", label, name, requiredProviders)
	case "TerraformModule":
		source, _ := metadata["source"].(string)
		if source == "" {
			return ""
		}
		return fmt.Sprintf("%s %s uses module source %s.", label, name, source)
	case "TerragruntConfig":
		terraformSource, _ := metadata["terraform_source"].(string)
		includes := metadataStringSlice(metadata, "includes")
		inputs := metadataStringSlice(metadata, "inputs")
		locals := metadataStringSlice(metadata, "locals")
		fragments := make([]string, 0, 4)
		if terraformSource != "" {
			fragments = append(fragments, "uses terraform source "+terraformSource)
		}
		if len(includes) > 0 {
			fragments = append(fragments, "includes "+strings.Join(includes, ", "))
		}
		if len(inputs) > 0 {
			fragments = append(fragments, "declares inputs "+strings.Join(inputs, ", "))
		}
		if len(locals) > 0 {
			fragments = append(fragments, "declares locals "+strings.Join(locals, ", "))
		}
		if len(fragments) == 0 {
			return ""
		}
		return fmt.Sprintf("%s %s %s.", label, name, joinSentenceFragments(fragments))
	case "TerragruntDependency":
		configPath, _ := metadata["config_path"].(string)
		if configPath == "" {
			return ""
		}
		return fmt.Sprintf("%s %s discovers config in %s.", label, name, configPath)
	case "Typedef":
		typeName, _ := metadata["type"].(string)
		if typeName == "" {
			return ""
		}
		return fmt.Sprintf("%s %s aliases %s.", label, name, typeName)
	case "Annotation":
		kind, _ := metadata["kind"].(string)
		targetKind, _ := metadata["target_kind"].(string)
		if kind == "applied" && targetKind != "" {
			return fmt.Sprintf("%s %s is applied to a %s.", label, name, humanizeSemanticValue(targetKind))
		}
		if kind == "declaration" {
			return fmt.Sprintf("%s %s declares an annotation type.", label, name)
		}
	case "Protocol":
		moduleKind, _ := metadata["module_kind"].(string)
		if moduleKind == "" {
			moduleKind = "protocol"
		}
		return fmt.Sprintf("%s %s is a %s.", label, name, strings.ReplaceAll(moduleKind, "_", " "))
	case "ImplBlock":
		kind, _ := metadata["kind"].(string)
		target, _ := metadata["target"].(string)
		trait, _ := metadata["trait"].(string)
		switch {
		case kind == "trait_impl" && trait != "" && target != "":
			return fmt.Sprintf("%s %s implements %s for %s.", label, name, trait, target)
		case kind != "" && target != "":
			return fmt.Sprintf("%s %s is a %s for %s.", label, name, strings.ReplaceAll(kind, "_", " "), target)
		}
	case "Component":
		framework, _ := metadata["framework"].(string)
		wrapperKind, _ := metadata["component_wrapper_kind"].(string)
		fragments := make([]string, 0, 3)
		if framework != "" {
			fragments = append(fragments, "is associated with the "+framework+" framework")
		}
		if wrapperKind != "" {
			fragments = append(fragments, "is wrapped by "+wrapperKind)
		}
		if boolValue(metadata["jsx_fragment_shorthand"]) {
			fragments = append(fragments, "uses JSX fragment shorthand")
		}
		if len(fragments) > 0 {
			return fmt.Sprintf("%s %s %s.", label, name, joinSentenceFragments(fragments))
		}
		return ""
	case "KustomizeOverlay":
		bases := metadataStringSlice(metadata, "bases")
		resourceRefs := metadataStringSlice(metadata, "resource_refs")
		helmRefs := metadataStringSlice(metadata, "helm_refs")
		imageRefs := metadataStringSlice(metadata, "image_refs")
		patchTargets := metadataStringSlice(metadata, "patch_targets")
		deployRefs := append([]string{}, resourceRefs...)
		deployRefs = append(deployRefs, helmRefs...)
		deployRefs = append(deployRefs, imageRefs...)
		switch {
		case len(bases) > 0 && len(deployRefs) > 0 && len(patchTargets) > 0:
			return fmt.Sprintf("%s %s references bases %s, deploys from %s, and patches %s.",
				label, name, strings.Join(bases, ", "), strings.Join(deployRefs, ", "), strings.Join(patchTargets, ", "))
		case len(deployRefs) > 0 && len(patchTargets) > 0:
			return fmt.Sprintf("%s %s deploys from %s and patches %s.",
				label, name, strings.Join(deployRefs, ", "), strings.Join(patchTargets, ", "))
		case len(deployRefs) > 0:
			return fmt.Sprintf("%s %s deploys from %s.", label, name, strings.Join(deployRefs, ", "))
		case len(bases) > 0 && len(patchTargets) > 0:
			return fmt.Sprintf("%s %s references bases %s and patches %s.",
				label, name, strings.Join(bases, ", "), strings.Join(patchTargets, ", "))
		case len(bases) > 0:
			return fmt.Sprintf("%s %s references bases %s.", label, name, strings.Join(bases, ", "))
		case len(patchTargets) > 0:
			return fmt.Sprintf("%s %s patches %s.", label, name, strings.Join(patchTargets, ", "))
		default:
			return ""
		}
	case "K8sResource":
		qualifiedName, _ := metadata["qualified_name"].(string)
		labels, _ := metadata["labels"].(string)
		switch {
		case qualifiedName != "" && labels != "":
			return fmt.Sprintf("%s %s is identified as %s and carries labels %s.", label, name, qualifiedName, labels)
		case qualifiedName != "":
			return fmt.Sprintf("%s %s is identified as %s.", label, name, qualifiedName)
		}
	case "ArgoCDApplication":
		sourceRepo, _ := metadata["source_repo"].(string)
		sourcePath, _ := metadata["source_path"].(string)
		destServer, _ := metadata["dest_server"].(string)
		destNamespace, _ := metadata["dest_namespace"].(string)
		switch {
		case sourceRepo != "" && sourcePath != "" && destServer != "" && destNamespace != "":
			return fmt.Sprintf("%s %s deploys from %s at %s and targets %s namespace %s.",
				label, name, sourceRepo, sourcePath, destServer, destNamespace)
		case sourceRepo != "" && destServer != "":
			return fmt.Sprintf("%s %s deploys from %s and targets %s.", label, name, sourceRepo, destServer)
		}
	case "ArgoCDApplicationSet":
		generatorRepos := metadataStringSlice(metadata, "generator_source_repos")
		templateRepos := metadataStringSlice(metadata, "template_source_repos")
		destServer, _ := metadata["dest_server"].(string)
		destNamespace, _ := metadata["dest_namespace"].(string)
		switch {
		case len(generatorRepos) > 0 && len(templateRepos) > 0 && destServer != "" && destNamespace != "":
			return fmt.Sprintf("%s %s discovers config in %s, deploys templates from %s, and targets %s namespace %s.",
				label, name, strings.Join(generatorRepos, ", "), strings.Join(templateRepos, ", "), destServer, destNamespace)
		case len(generatorRepos) > 0 && len(templateRepos) > 0:
			return fmt.Sprintf("%s %s discovers config in %s and deploys templates from %s.",
				label, name, strings.Join(generatorRepos, ", "), strings.Join(templateRepos, ", "))
		}
	case "CloudFormationCondition":
		expression, _ := metadata["expression"].(string)
		if expression != "" {
			return fmt.Sprintf("%s %s evaluates %s.", label, name, expression)
		}
	case "CloudFormationResource":
		resourceType, _ := metadata["resource_type"].(string)
		templateURL, _ := metadata["template_url"].(string)
		condition, _ := metadata["condition"].(string)
		switch {
		case resourceType == "AWS::CloudFormation::Stack" && templateURL != "" && condition != "":
			return fmt.Sprintf("%s %s is an AWS::CloudFormation::Stack nested stack sourced from %s and guarded by condition %s.",
				label, name, templateURL, condition)
		case resourceType == "AWS::CloudFormation::Stack" && templateURL != "":
			return fmt.Sprintf("%s %s is an AWS::CloudFormation::Stack nested stack sourced from %s.",
				label, name, templateURL)
		case resourceType != "" && condition != "":
			return fmt.Sprintf("%s %s is an %s guarded by condition %s.", label, name, resourceType, condition)
		case resourceType != "":
			return fmt.Sprintf("%s %s is an %s.", label, name, resourceType)
		}
	}

	fragments := make([]string, 0, 4)
	pythonProfile := PythonSemanticProfileFromMetadata(label, metadata)
	if pythonProfile.Lambda {
		fragments = append(fragments, "is a lambda function")
	}
	if componentAssertion, _ := metadata["component_type_assertion"].(string); componentAssertion != "" {
		fragments = append(fragments, "narrows to "+componentAssertion)
	}
	if pythonProfile.Async {
		fragments = append(fragments, "is async")
	}
	if len(pythonProfile.Decorators) > 0 {
		switch {
		case language == "python" && label == "Class" && pythonProfile.Metaclass == "":
			fragments = append(fragments, "is decorated with "+strings.Join(pythonProfile.Decorators, ", "))
		default:
			fragments = append(fragments, "uses decorators "+strings.Join(pythonProfile.Decorators, ", "))
		}
	}
	if pythonProfile.Metaclass != "" {
		fragments = append(fragments, "uses metaclass "+pythonProfile.Metaclass)
	}
	if pythonProfile.TypeAnnotation && label == "TypeAnnotation" {
		switch pythonProfile.AnnotationKind {
		case "parameter":
			if pythonProfile.Context != "" {
				fragments = append(fragments, fmt.Sprintf("is a parameter annotation for %s", pythonProfile.Context))
			}
		case "return":
			if pythonProfile.Context != "" {
				fragments = append(fragments, fmt.Sprintf("is a return annotation for %s", pythonProfile.Context))
			}
		}
	}
	if pythonProfile.TypeAnnotation && label != "TypeAnnotation" {
		switch {
		case len(pythonProfile.TypeAnnotationKinds) == 1:
			fragments = append(fragments, fmt.Sprintf("has %s type annotations", pythonProfile.TypeAnnotationKinds[0]))
		case len(pythonProfile.TypeAnnotationKinds) > 1:
			fragments = append(fragments, fmt.Sprintf(
				"has %s type annotations",
				joinSentenceFragments(pythonProfile.TypeAnnotationKinds),
			))
		case pythonProfile.TypeAnnotationCount > 0:
			fragments = append(fragments, fmt.Sprintf("has %d type annotations", pythonProfile.TypeAnnotationCount))
		}
	}
	if params := metadataStringSlice(metadata, "type_parameters"); len(params) > 0 {
		fragments = append(fragments, "declares type parameters "+strings.Join(params, ", "))
	}
	jsSemantics := ExtractJavaScriptSemantics(metadata)
	if jsSemantics.MethodKind != "" {
		if StringVal(entity, "language") == "javascript" {
			fragments = append(fragments, "has JavaScript method kind "+jsSemantics.MethodKind)
		} else {
			fragments = append(fragments, "has method kind "+jsSemantics.MethodKind)
		}
	}
	if jsSemantics.Docstring != "" {
		fragments = append(fragments, fmt.Sprintf("is documented as %q", jsSemantics.Docstring))
	}
	if boolValue(metadata["jsx_fragment_shorthand"]) {
		fragments = append(fragments, "uses JSX fragment shorthand")
	}
	if constructorKind, _ := metadata["constructor_kind"].(string); constructorKind != "" {
		fragments = append(fragments, fmt.Sprintf("is a %s constructor", strings.ReplaceAll(constructorKind, "_", " ")))
	}
	if semanticKind, _ := metadata["semantic_kind"].(string); semanticKind != "" && (!pythonProfile.Lambda || semanticKind != "lambda") {
		fragments = append(fragments, fmt.Sprintf("is a %s", strings.ReplaceAll(semanticKind, "_", " ")))
	}
	if moduleKind, _ := metadata["module_kind"].(string); moduleKind != "" {
		switch moduleKind {
		case "protocol_implementation":
			protocol, _ := metadata["protocol"].(string)
			implementedFor, _ := metadata["implemented_for"].(string)
			if protocol != "" && implementedFor != "" {
				fragments = append(fragments, fmt.Sprintf("is a protocol implementation for %s via %s", implementedFor, protocol))
			} else {
				fragments = append(fragments, "is a protocol implementation")
			}
		default:
			fragments = append(fragments, fmt.Sprintf("is a %s", strings.ReplaceAll(moduleKind, "_", " ")))
		}
	}
	if attributeKind, _ := metadata["attribute_kind"].(string); attributeKind != "" {
		fragments = append(fragments, fmt.Sprintf("is a %s", strings.ReplaceAll(attributeKind, "_", " ")))
	}

	if len(fragments) == 0 {
		return ""
	}
	if len(fragments) == 1 {
		return fmt.Sprintf("%s %s %s.", label, name, fragments[0])
	}
	return fmt.Sprintf("%s %s %s.", label, name, joinSentenceFragments(fragments))
}

func attachSemanticSummary(result map[string]any) {
	if result == nil {
		return
	}
	metadata, _ := result["metadata"].(map[string]any)
	if len(metadata) == 0 {
		return
	}

	entity := map[string]any{
		"metadata": metadata,
	}
	if name := StringVal(result, "name"); name != "" {
		entity["name"] = name
	} else if name := StringVal(result, "entity_name"); name != "" {
		entity["name"] = name
	}
	if labels := StringSliceVal(result, "labels"); len(labels) > 0 {
		entity["labels"] = labels
	} else if entityType := StringVal(result, "entity_type"); entityType != "" {
		entity["labels"] = []string{entityType}
	}
	if language := StringVal(result, "language"); language != "" {
		entity["language"] = language
	}

	if summary := buildEntitySemanticSummary(entity); summary != "" {
		result["semantic_summary"] = summary
	}
	if profile := buildEntitySemanticProfile(entity); len(profile) > 0 {
		result["semantic_profile"] = profile
	}
	if StringVal(entity, "language") == "python" {
		if pythonSemantics := PythonSemanticProfileFromMetadata(primaryEntityLabel(entity), metadata); pythonSemantics.Present() {
			result["python_semantics"] = pythonSemantics.Fields()
		}
	}
	if jsSemantics := ExtractJavaScriptSemantics(metadata); jsSemantics.Present() {
		result["javascript_semantics"] = jsSemantics.Fields()
	}
	if language := StringVal(entity, "language"); language == "typescript" || language == "tsx" {
		if tsSemantics := TypeScriptSemanticProfileFromMetadata(metadata); tsSemantics.Present() {
			result["typescript_semantics"] = tsSemantics.Fields()
		}
	}
	if story := buildEntityStory(result); story != "" {
		result["story"] = story
	}
}

func primaryEntityLabel(entity map[string]any) string {
	labels := StringSliceVal(entity, "labels")
	if len(labels) == 0 {
		return ""
	}
	return labels[0]
}

func joinSentenceFragments(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
	}
}

func humanizeSemanticValue(value string) string {
	if value == "" {
		return ""
	}
	return strings.ReplaceAll(value, "_", " ")
}
