package query

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func buildDockerfileRuntimeArtifacts(file FileContent) []map[string]any {
	if !isDockerfileArtifact(file) || strings.TrimSpace(file.Content) == "" {
		return nil
	}

	metadata := parser.ExtractDockerfileRuntimeMetadata(file.Content)
	stages := mapSliceValue(metadata, "dockerfile_stages")
	if len(stages) == 0 {
		return nil
	}

	rows := make([]map[string]any, 0, len(stages))
	for _, stage := range stages {
		stageName := strings.TrimSpace(StringVal(stage, "name"))
		if stageName == "" {
			continue
		}

		row := map[string]any{
			"relative_path": file.RelativePath,
			"artifact_type": "dockerfile",
			"artifact_name": stageName,
			"signals":       dockerfileRuntimeSignals(stage, metadata),
		}
		if baseImage := strings.TrimSpace(StringVal(stage, "base_image")); baseImage != "" {
			row["base_image"] = baseImage
		}
		if baseTag := strings.TrimSpace(StringVal(stage, "base_tag")); baseTag != "" {
			row["base_tag"] = baseTag
		}
		if platform := strings.TrimSpace(StringVal(stage, "platform")); platform != "" {
			row["platform"] = platform
		}
		if copyFrom := strings.TrimSpace(StringVal(stage, "copies_from")); copyFrom != "" {
			row["copy_from"] = []string{copyFrom}
		}
		if cmd := strings.TrimSpace(StringVal(stage, "cmd")); cmd != "" {
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
		return StringVal(rows[i], "artifact_name") < StringVal(rows[j], "artifact_name")
	})
	return rows
}

func dockerfileRuntimeSignals(stage map[string]any, metadata map[string]any) []string {
	signals := make([]string, 0, 6)
	stageName := strings.TrimSpace(StringVal(stage, "name"))
	if strings.TrimSpace(StringVal(stage, "base_image")) != "" {
		signals = append(signals, "base_image")
	}
	if strings.TrimSpace(StringVal(stage, "platform")) != "" {
		signals = append(signals, "platform")
	}
	if strings.TrimSpace(StringVal(stage, "copies_from")) != "" {
		signals = append(signals, "copy_from")
	}
	if strings.TrimSpace(StringVal(stage, "entrypoint")) != "" {
		signals = append(signals, "entrypoint")
	}
	if strings.TrimSpace(StringVal(stage, "cmd")) != "" {
		signals = append(signals, "cmd")
	}
	if strings.TrimSpace(StringVal(stage, "healthcheck")) != "" {
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
	for _, row := range mapSliceValue(metadata, "dockerfile_ports") {
		if strings.TrimSpace(StringVal(row, "stage")) != stageName {
			continue
		}
		port := strings.TrimSpace(StringVal(row, "port"))
		protocol := strings.TrimSpace(StringVal(row, "protocol"))
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
	for _, row := range mapSliceValue(metadata, "dockerfile_envs") {
		if strings.TrimSpace(StringVal(row, "stage")) != stageName {
			continue
		}
		if name := strings.TrimSpace(StringVal(row, "name")); name != "" {
			values = append(values, name)
		}
	}
	return composeRuntimeValues(values)
}

func isDockerfileArtifact(file FileContent) bool {
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
