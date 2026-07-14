// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
		// Decode once and share the result between the base HelmValue
		// extraction and the Grafana observability extraction below — both
		// previously called DecodeDocuments independently on this same
		// source (issue #4847). A decode error or empty document set is
		// treated as "nothing to extract" by both extractors, matching the
		// prior per-call swallow-and-return-nil/no-op behavior.
		documents, _ := DecodeDocuments(string(source))
		if item := parseHelmValues(path, documents); item != nil {
			shared.AppendBucket(payload, "helm_values", item)
		}
		// Only the base values.yaml defines the chart's canonical values; emitting
		// HelmValueDefinition nodes from environment overrides (values-prod.yaml)
		// would let a template usage resolve to an override instead of the base.
		if isHelmBaseValuesFile(filename) {
			for _, row := range parseHelmValueDefinitions(source) {
				shared.AppendBucket(payload, "helm_value_definitions", row)
			}
			shared.SortNamedBucket(payload, "helm_value_definitions")
		}
		appendHelmGrafanaObservability(payload, path, documents)
		sortObservabilityBuckets(payload)
		if options.IndexSource {
			payload["source"] = string(source)
		}
		return payload, nil
	}
	if isHelmTemplateManifest(path) {
		for _, row := range parseHelmTemplateValueUsages(source) {
			shared.AppendBucket(payload, "helm_template_value_usages", row)
		}
		shared.SortNamedBucket(payload, "helm_template_value_usages")
		if options.IndexSource {
			payload["source"] = string(source)
		}
		return payload, nil
	}
	if isAtlantisConfig(filename) {
		// Atlantis configs are decoded directly from source (not the node-walked
		// document) so YAML anchors and merge keys in projects[] resolve. Both
		// buckets come from one shared unmarshal (issue #4846).
		rows, workflowRows, err := parseAtlantisFromSource(source, path)
		if err != nil {
			return nil, fmt.Errorf("parse atlantis config %q: %w", path, err)
		}
		for _, row := range rows {
			shared.AppendBucket(payload, "atlantis_projects", row)
		}
		shared.SortNamedBucket(payload, "atlantis_projects")
		for _, row := range workflowRows {
			shared.AppendBucket(payload, "atlantis_workflows", row)
		}
		shared.SortNamedBucket(payload, "atlantis_workflows")
		if options.IndexSource {
			payload["source"] = string(source)
		}
		return payload, nil
	}
	if isGitlabCIConfig(filename) {
		// GitLab CI configs are decoded directly from source (not the node-walked
		// document) so YAML anchors and merge keys in job definitions resolve.
		pipeline, jobs, err := parseGitlabCIFromSource(source, path)
		if err != nil {
			return nil, fmt.Errorf("parse gitlab ci config %q: %w", path, err)
		}
		if pipeline != nil {
			shared.AppendBucket(payload, "gitlab_pipelines", pipeline)
		}
		for _, row := range jobs {
			shared.AppendBucket(payload, "gitlab_jobs", row)
		}
		shared.SortNamedBucket(payload, "gitlab_pipelines")
		shared.SortNamedBucket(payload, "gitlab_jobs")
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
		"helm_value_definitions",
		"helm_template_value_usages",
		"cloudformation_resources",
		"cloudformation_parameters",
		"cloudformation_outputs",
		"cloudformation_conditions",
		"cloudformation_cross_stack_imports",
		"cloudformation_cross_stack_exports",
		"atlantis_projects",
		"atlantis_workflows",
		"gitlab_pipelines",
		"gitlab_jobs",
	} {
		shared.SortNamedBucket(payload, bucket)
	}
	sortObservabilityBuckets(payload)
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
	payload["helm_value_definitions"] = []map[string]any{}
	payload["helm_template_value_usages"] = []map[string]any{}
	payload["cloudformation_resources"] = []map[string]any{}
	payload["cloudformation_parameters"] = []map[string]any{}
	payload["cloudformation_outputs"] = []map[string]any{}
	payload["cloudformation_conditions"] = []map[string]any{}
	payload["cloudformation_cross_stack_imports"] = []map[string]any{}
	payload["cloudformation_cross_stack_exports"] = []map[string]any{}
	payload["atlantis_projects"] = []map[string]any{}
	payload["atlantis_workflows"] = []map[string]any{}
	payload["gitlab_pipelines"] = []map[string]any{}
	payload["gitlab_jobs"] = []map[string]any{}
	payload["variables"] = []map[string]any{}
	initObservabilityBuckets(payload)
	return payload
}

func appendYAMLDocument(payload map[string]any, path string, filename string, document map[string]any) {
	lineNumber := shared.IntValue(document["__eshu_line_number"])
	delete(document, "__eshu_line_number")
	if lineNumber <= 0 {
		lineNumber = 1
	}
	if isPubspecDependencyFile(filename) {
		appendPubspecDependencyRows(payload, filename, document, path, lineNumber)
		return
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
	if !appendAppliedObservabilityFromDocument(payload, path, document, metadata, apiVersion, kind, lineNumber) {
		appendGrafanaObservabilityFromDocument(payload, path, document, metadata, apiVersion, kind, lineNumber)
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
		value, err := yamlNodeToAny(node.Content[0])
		if err != nil {
			return nil, err
		}
		if object, ok := value.(map[string]any); ok {
			object["__eshu_line_number"] = node.Content[0].Line
			documents = append(documents, object)
			continue
		}
		documents = append(documents, value)
	}
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
