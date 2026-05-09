package yaml

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cloudformation"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"

	yamlv3 "gopkg.in/yaml.v3"
)

// Options configures one YAML parser execution.
type Options = shared.Options

// Parse reads one YAML-family file and returns the parser payload consumed by
// the parent engine.
func Parse(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	payload := yamlBasePayload(path, isDependency)
	filename := filepath.Base(path)
	if isHelmChartFile(filename) {
		if item := parseHelmChart(path, source); item != nil {
			shared.AppendBucket(payload, "helm_charts", item)
		}
		if options.IndexSource {
			payload["source"] = string(source)
		}
		return payload, nil
	}
	if isHelmValuesFile(filename) {
		if item := parseHelmValues(path, source); item != nil {
			shared.AppendBucket(payload, "helm_values", item)
		}
		if options.IndexSource {
			payload["source"] = string(source)
		}
		return payload, nil
	}
	if isHelmTemplateManifest(path) {
		if options.IndexSource {
			payload["source"] = string(source)
		}
		return payload, nil
	}

	documents, err := DecodeDocuments(SanitizeTemplating(string(source)))
	if err != nil {
		return nil, fmt.Errorf("parse yaml file %q: %w", path, err)
	}
	for _, document := range documents {
		object, ok := document.(map[string]any)
		if !ok {
			continue
		}
		appendYAMLDocument(payload, path, filename, object)
	}

	for _, bucket := range []string{
		"k8s_resources",
		"argocd_applications",
		"argocd_applicationsets",
		"crossplane_xrds",
		"crossplane_compositions",
		"crossplane_claims",
		"kustomize_overlays",
		"helm_charts",
		"helm_values",
		"cloudformation_resources",
		"cloudformation_parameters",
		"cloudformation_outputs",
		"cloudformation_conditions",
		"cloudformation_cross_stack_imports",
		"cloudformation_cross_stack_exports",
	} {
		shared.SortNamedBucket(payload, bucket)
	}
	if options.IndexSource {
		payload["source"] = string(source)
	}
	return payload, nil
}

func yamlBasePayload(path string, isDependency bool) map[string]any {
	payload := shared.BasePayload(path, "yaml", isDependency)
	payload["k8s_resources"] = []map[string]any{}
	payload["argocd_applications"] = []map[string]any{}
	payload["argocd_applicationsets"] = []map[string]any{}
	payload["crossplane_xrds"] = []map[string]any{}
	payload["crossplane_compositions"] = []map[string]any{}
	payload["crossplane_claims"] = []map[string]any{}
	payload["kustomize_overlays"] = []map[string]any{}
	payload["helm_charts"] = []map[string]any{}
	payload["helm_values"] = []map[string]any{}
	payload["cloudformation_resources"] = []map[string]any{}
	payload["cloudformation_parameters"] = []map[string]any{}
	payload["cloudformation_outputs"] = []map[string]any{}
	payload["cloudformation_conditions"] = []map[string]any{}
	payload["cloudformation_cross_stack_imports"] = []map[string]any{}
	payload["cloudformation_cross_stack_exports"] = []map[string]any{}
	return payload
}

func appendYAMLDocument(payload map[string]any, path string, filename string, document map[string]any) {
	lineNumber := shared.IntValue(document["__eshu_line_number"])
	delete(document, "__eshu_line_number")
	if lineNumber <= 0 {
		lineNumber = 1
	}
	if cloudformation.IsTemplate(document) {
		result := cloudformation.Parse(document, path, lineNumber, "yaml")
		payload["cloudformation_resources"] = append(payload["cloudformation_resources"].([]map[string]any), result.Resources...)
		payload["cloudformation_parameters"] = append(payload["cloudformation_parameters"].([]map[string]any), result.Params...)
		payload["cloudformation_outputs"] = append(payload["cloudformation_outputs"].([]map[string]any), result.Outputs...)
		payload["cloudformation_conditions"] = append(payload["cloudformation_conditions"].([]map[string]any), result.Conditions...)
		payload["cloudformation_cross_stack_imports"] = append(payload["cloudformation_cross_stack_imports"].([]map[string]any), result.Imports...)
		payload["cloudformation_cross_stack_exports"] = append(payload["cloudformation_cross_stack_exports"].([]map[string]any), result.Exports...)
		return
	}

	apiVersion, _ := document["apiVersion"].(string)
	kind, _ := document["kind"].(string)
	metadata, _ := document["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}

	if isKustomization(apiVersion, kind, filename) {
		shared.AppendBucket(payload, "kustomize_overlays", parseKustomization(document, path, lineNumber))
		return
	}
	if strings.TrimSpace(apiVersion) == "" || strings.TrimSpace(kind) == "" {
		return
	}
	if isArgoCDApplication(apiVersion, kind) {
		shared.AppendBucket(payload, "argocd_applications", parseArgoCDApplication(document, metadata, path, lineNumber))
		return
	}
	if isArgoCDApplicationSet(apiVersion, kind) {
		shared.AppendBucket(payload, "argocd_applicationsets", parseArgoCDApplicationSet(document, metadata, path, lineNumber))
		return
	}
	if isCrossplaneXRD(apiVersion, kind) {
		shared.AppendBucket(payload, "crossplane_xrds", parseCrossplaneXRD(document, metadata, path, lineNumber))
		return
	}
	if isCrossplaneComposition(apiVersion, kind) {
		shared.AppendBucket(payload, "crossplane_compositions", parseCrossplaneComposition(document, metadata, path, lineNumber))
		return
	}
	if isCrossplaneClaim(apiVersion) {
		shared.AppendBucket(payload, "crossplane_claims", parseCrossplaneClaim(metadata, apiVersion, kind, path, lineNumber))
		return
	}
	shared.AppendBucket(payload, "k8s_resources", parseK8sResource(document, metadata, apiVersion, kind, path, lineNumber))
}

// DecodeDocuments decodes one YAML source string into document values while
// preserving the top-level line number on map documents.
func DecodeDocuments(source string) ([]any, error) {
	decoder := yamlv3.NewDecoder(strings.NewReader(source))
	documents := make([]any, 0)
	for {
		var node yamlv3.Node
		err := decoder.Decode(&node)
		if err != nil {
			if err.Error() == "EOF" {
				return documents, nil
			}
			return nil, err
		}
		if len(node.Content) == 0 {
			continue
		}
		value := yamlNodeToAny(node.Content[0])
		if object, ok := value.(map[string]any); ok {
			object["__eshu_line_number"] = node.Content[0].Line
			documents = append(documents, object)
			continue
		}
		documents = append(documents, value)
	}
}

func yamlNodeToAny(node *yamlv3.Node) any {
	if node == nil {
		return nil
	}

	if intrinsicKey, ok := cloudFormationYAMLIntrinsicKey(node.Tag); ok {
		return map[string]any{intrinsicKey: yamlNodeValue(node)}
	}

	switch node.Kind {
	case yamlv3.DocumentNode:
		if len(node.Content) == 0 {
			return nil
		}
		return yamlNodeToAny(node.Content[0])
	case yamlv3.MappingNode:
		result := make(map[string]any, len(node.Content)/2)
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := yamlScalarString(node.Content[index])
			result[key] = yamlNodeToAny(node.Content[index+1])
		}
		return result
	case yamlv3.SequenceNode:
		result := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			result = append(result, yamlNodeToAny(child))
		}
		return result
	case yamlv3.ScalarNode:
		return yamlScalarString(node)
	default:
		return nil
	}
}

func yamlNodeValue(node *yamlv3.Node) any {
	switch node.Kind {
	case yamlv3.DocumentNode:
		if len(node.Content) == 0 {
			return nil
		}
		return yamlNodeToAny(node.Content[0])
	case yamlv3.MappingNode:
		result := make(map[string]any, len(node.Content)/2)
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := yamlScalarString(node.Content[index])
			result[key] = yamlNodeToAny(node.Content[index+1])
		}
		return result
	case yamlv3.SequenceNode:
		result := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			result = append(result, yamlNodeToAny(child))
		}
		return result
	case yamlv3.ScalarNode:
		return yamlScalarString(node)
	default:
		return nil
	}
}

func cloudFormationYAMLIntrinsicKey(tag string) (string, bool) {
	switch tag {
	case "!And":
		return "Fn::And", true
	case "!Condition":
		return "Condition", true
	case "!Equals":
		return "Fn::Equals", true
	case "!GetAtt":
		return "Fn::GetAtt", true
	case "!If":
		return "Fn::If", true
	case "!ImportValue":
		return "Fn::ImportValue", true
	case "!Join":
		return "Fn::Join", true
	case "!Or":
		return "Fn::Or", true
	case "!Ref":
		return "Ref", true
	case "!Sub":
		return "Fn::Sub", true
	default:
		return "", false
	}
}

func yamlScalarString(node *yamlv3.Node) string {
	if node == nil {
		return ""
	}
	return strings.TrimSpace(node.Value)
}

// SanitizeTemplating removes or quotes common templating forms that would
// otherwise prevent YAML decoding without evaluating those templates.
func SanitizeTemplating(source string) string {
	lines := strings.Split(source, "\n")
	sanitized := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "{%") || strings.HasPrefix(trimmed, "{#") {
			continue
		}
		replaced := line
		if prefix, expr, suffix, ok := splitTemplatedMapping(line); ok {
			replaced = prefix + `"` + expr + `"` + suffix
		}
		if prefix, expr, suffix, ok := splitTemplatedSequence(replaced); ok {
			replaced = prefix + `"` + expr + `"` + suffix
		}
		sanitized = append(sanitized, strings.ReplaceAll(replaced, "\t", "  "))
	}
	return strings.Join(sanitized, "\n")
}

func splitTemplatedMapping(line string) (string, string, string, bool) {
	index := strings.Index(line, ":")
	if index <= 0 {
		return "", "", "", false
	}
	prefix := line[:index+1]
	suffix := strings.TrimSpace(line[index+1:])
	if !strings.HasPrefix(suffix, "{{") || !strings.Contains(suffix, "}}") {
		return "", "", "", false
	}
	return prefix + " ", "__ESHU_JINJA_EXPR__", "", true
}

func splitTemplatedSequence(line string) (string, string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "- {{") || !strings.Contains(trimmed, "}}") {
		return "", "", "", false
	}
	prefix := line[:strings.Index(line, "-")+1] + " "
	return prefix, "__ESHU_JINJA_EXPR__", "", true
}
