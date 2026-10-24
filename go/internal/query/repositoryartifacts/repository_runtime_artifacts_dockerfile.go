// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func buildDockerfileRuntimeArtifacts(file querycontract.FileContent) []map[string]any {
	if !isDockerfileArtifact(file) || strings.TrimSpace(file.Content) == "" {
		return nil
	}

	metadata := parser.ExtractDockerfileRuntimeMetadata(file.Content)
	stages := querycontract.MapSliceValue(metadata, "dockerfile_stages")
	if len(stages) == 0 {
		return nil
	}

	rows := make([]map[string]any, 0, len(stages))
	for _, stage := range stages {
		stageName := strings.TrimSpace(querycontract.StringVal(stage, "name"))
		if stageName == "" {
			continue
		}

		row := map[string]any{
			"relative_path": file.RelativePath,
			"artifact_type": "dockerfile",
			"artifact_name": stageName,
			"signals":       dockerfileRuntimeSignals(stage, metadata),
		}
		if baseImage := strings.TrimSpace(querycontract.StringVal(stage, "base_image")); baseImage != "" {
			row["base_image"] = baseImage
		}
		if baseTag := strings.TrimSpace(querycontract.StringVal(stage, "base_tag")); baseTag != "" {
			row["base_tag"] = baseTag
		}
		addDockerfileBaseRefEvidence(row, stage)
		if platform := strings.TrimSpace(querycontract.StringVal(stage, "platform")); platform != "" {
			row["platform"] = platform
		}
		if copyFrom := strings.TrimSpace(querycontract.StringVal(stage, "copies_from")); copyFrom != "" {
			row["copy_from"] = []string{copyFrom}
		}
		if cmd := strings.TrimSpace(querycontract.StringVal(stage, "cmd")); cmd != "" {
			row["cmd"] = cmd
		}
		if ports := dockerfileStagePorts(metadata, stageName); len(ports) > 0 {
			row["ports"] = ports
		}
		if environment := dockerfileStageEnvironment(metadata, stageName); len(environment) > 0 {
			row["environment"] = environment
		}
		rows = append(rows, row)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return querycontract.StringVal(rows[i], "artifact_name") < querycontract.StringVal(rows[j], "artifact_name")
	})
	return rows
}

// addDockerfileBaseRefEvidence copies the ARG substitution evidence of a FROM
// reference onto the artifact row. base_tag already holds the expanded tag when
// every variable resolved; base_ref_unresolved is true when it still holds a
// placeholder, with the bounded ARG defaults the parser saw as evidence.
func addDockerfileBaseRefEvidence(row map[string]any, stage map[string]any) {
	if template := strings.TrimSpace(querycontract.StringVal(stage, "base_ref_template")); template != "" {
		row["base_ref_template"] = template
	}
	if names := stringsFromAny(stage["base_ref_unresolved_args"]); len(names) > 0 {
		row["base_ref_unresolved"] = true
		row["base_ref_unresolved_args"] = names
	}
	if raw, ok := stage["base_ref_arg_defaults"].(map[string]any); ok && len(raw) > 0 {
		defaults := make(map[string]string, len(raw))
		for name, value := range raw {
			if text, isText := value.(string); isText {
				defaults[name] = text
			}
		}
		if len(defaults) > 0 {
			row["base_ref_arg_defaults"] = defaults
		}
	}
	// The parser bounds the defaults it keeps. These two keys say what that
	// bound left out, so a reader does not take a cut default for the whole one.
	if names := stringsFromAny(stage["base_ref_arg_defaults_truncated"]); len(names) > 0 {
		row["base_ref_arg_defaults_truncated"] = names
	}
	if omitted, _ := stage["base_ref_arg_defaults_omitted"].(bool); omitted {
		row["base_ref_arg_defaults_omitted"] = true
	}
}

func stringsFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && text != "" {
				out = append(out, text)
			}
		}
		return out
	}
	return nil
}

func dockerfileRuntimeSignals(stage map[string]any, metadata map[string]any) []string {
	signals := make([]string, 0, 6)
	stageName := strings.TrimSpace(querycontract.StringVal(stage, "name"))
	if strings.TrimSpace(querycontract.StringVal(stage, "base_image")) != "" {
		signals = append(signals, "base_image")
	}
	if strings.TrimSpace(querycontract.StringVal(stage, "platform")) != "" {
		signals = append(signals, "platform")
	}
	if strings.TrimSpace(querycontract.StringVal(stage, "copies_from")) != "" {
		signals = append(signals, "copy_from")
	}
	if strings.TrimSpace(querycontract.StringVal(stage, "entrypoint")) != "" {
		signals = append(signals, "entrypoint")
	}
	if strings.TrimSpace(querycontract.StringVal(stage, "cmd")) != "" {
		signals = append(signals, "cmd")
	}
	if strings.TrimSpace(querycontract.StringVal(stage, "healthcheck")) != "" {
		signals = append(signals, "healthcheck")
	}
	if len(dockerfileStagePorts(metadata, stageName)) > 0 {
		signals = append(signals, "ports")
	}
	if len(dockerfileStageEnvironment(metadata, stageName)) > 0 {
		signals = append(signals, "environment")
	}
	return signals
}

func dockerfileStagePorts(metadata map[string]any, stageName string) []string {
	values := make([]string, 0)
	for _, row := range querycontract.MapSliceValue(metadata, "dockerfile_ports") {
		if strings.TrimSpace(querycontract.StringVal(row, "stage")) != stageName {
			continue
		}
		port := strings.TrimSpace(querycontract.StringVal(row, "port"))
		protocol := strings.TrimSpace(querycontract.StringVal(row, "protocol"))
		if port == "" {
			continue
		}
		if protocol == "" {
			protocol = "tcp"
		}
		values = append(values, port+"/"+protocol)
	}
	return composeRuntimeValues(values)
}

func dockerfileStageEnvironment(metadata map[string]any, stageName string) []string {
	values := make([]string, 0)
	for _, row := range querycontract.MapSliceValue(metadata, "dockerfile_envs") {
		if strings.TrimSpace(querycontract.StringVal(row, "stage")) != stageName {
			continue
		}
		if name := strings.TrimSpace(querycontract.StringVal(row, "name")); name != "" {
			values = append(values, name)
		}
	}
	return composeRuntimeValues(values)
}

func isDockerfileArtifact(file querycontract.FileContent) bool {
	if strings.EqualFold(file.ArtifactType, "dockerfile") {
		return true
	}

	base := strings.ToLower(strings.TrimSpace(file.RelativePath))
	if base == "" {
		return false
	}
	return strings.HasSuffix(base, "/dockerfile") ||
		strings.HasSuffix(base, "/dockerfile.dev") ||
		strings.HasSuffix(base, "/dockerfile.prod") ||
		base == "dockerfile"
}
