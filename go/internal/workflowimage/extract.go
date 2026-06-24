// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflowimage

import (
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
)

const (
	// EvidenceClassImageRef means the workflow command named exactly one image ref.
	EvidenceClassImageRef = "workflow_image_ref"
	// EvidenceClassUnresolved means the workflow command named only templated or variable refs.
	EvidenceClassUnresolved = "workflow_image_unresolved"
	// EvidenceClassAmbiguous means the workflow command named multiple candidate image refs.
	EvidenceClassAmbiguous = "workflow_image_ambiguous"
)

// Evidence is one public-safe workflow image evidence row.
type Evidence struct {
	WorkflowPath  string
	JobName       string
	StepName      string
	CommandKind   string
	ImageRef      string
	ImageRefs     []string
	EvidenceClass string
	Reason        string
}

// ExtractGitHubActions returns image evidence from GitHub Actions workflow YAML.
func ExtractGitHubActions(workflowPath string, content string) []Evidence {
	documents, err := decodeYAMLMaps(content)
	if err != nil {
		return nil
	}
	var out []Evidence
	for _, document := range documents {
		jobs, ok := document["jobs"].(map[string]any)
		if !ok {
			continue
		}
		for jobName, rawJob := range jobs {
			job, ok := rawJob.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, evidenceFromReusableWorkflow(workflowPath, jobName, job)...)
			steps, ok := job["steps"].([]any)
			if !ok {
				continue
			}
			for _, rawStep := range steps {
				step, ok := rawStep.(map[string]any)
				if !ok {
					continue
				}
				stepName := strings.TrimSpace(stringValue(step["name"]))
				runCommand := strings.TrimSpace(stringValue(step["run"]))
				if runCommand == "" {
					continue
				}
				out = append(out, ExtractCommand(workflowPath, jobName, stepName, runCommand)...)
			}
		}
	}
	return dedupeEvidence(out)
}

// ExtractCommand classifies public-safe image evidence from one shell command.
func ExtractCommand(workflowPath string, jobName string, stepName string, command string) []Evidence {
	var out []Evidence
	for _, segment := range splitCommandSegments(command) {
		fields := commandFields(segment)
		if len(fields) == 0 {
			continue
		}
		out = append(out, evidenceFromFields(workflowPath, jobName, stepName, fields)...)
	}
	return dedupeEvidence(out)
}

func evidenceFromReusableWorkflow(workflowPath string, jobName string, job map[string]any) []Evidence {
	with, ok := job["with"].(map[string]any)
	if !ok {
		return nil
	}
	var out []Evidence
	for _, key := range []string{"image", "image_ref", "container_image"} {
		value := strings.TrimSpace(stringValue(with[key]))
		if value == "" {
			continue
		}
		out = append(out, classifyCandidates(
			workflowPath,
			jobName,
			"",
			"reusable_workflow_input",
			[]string{value},
		))
	}
	return out
}

func evidenceFromFields(workflowPath string, jobName string, stepName string, fields []string) []Evidence {
	commandKind, candidates := dockerImageCandidates(fields)
	if commandKind == "" {
		return nil
	}
	return []Evidence{classifyCandidates(workflowPath, jobName, stepName, commandKind, candidates)}
}

func dockerImageCandidates(fields []string) (string, []string) {
	if len(fields) < 2 || fields[0] != "docker" {
		return "", nil
	}
	switch fields[1] {
	case "build":
		return "docker_build", dockerBuildTags(fields[2:])
	case "buildx":
		if len(fields) >= 3 && fields[2] == "build" {
			return "docker_buildx", dockerBuildTags(fields[3:])
		}
	case "push":
		if len(fields) >= 3 {
			return "docker_push", []string{fields[2]}
		}
	case "tag":
		if len(fields) >= 4 {
			return "docker_tag", []string{fields[len(fields)-1]}
		}
	}
	return "", nil
}

func dockerBuildTags(fields []string) []string {
	var tags []string
	for index := 0; index < len(fields); index++ {
		field := fields[index]
		switch {
		case field == "-t" || field == "--tag":
			if index+1 < len(fields) {
				tags = append(tags, fields[index+1])
				index++
			}
		case strings.HasPrefix(field, "-t="):
			tags = append(tags, strings.TrimPrefix(field, "-t="))
		case strings.HasPrefix(field, "--tag="):
			tags = append(tags, strings.TrimPrefix(field, "--tag="))
		}
	}
	return tags
}

func classifyCandidates(
	workflowPath string,
	jobName string,
	stepName string,
	commandKind string,
	candidates []string,
) Evidence {
	exact := make([]string, 0, len(candidates))
	unresolved := false
	for _, candidate := range candidates {
		trimmed := strings.Trim(strings.TrimSpace(candidate), `"'`)
		if trimmed == "" {
			continue
		}
		if isUnresolvedCandidate(trimmed) {
			unresolved = true
			continue
		}
		if ref := doctruth.NormalizeContainerImageRefClaim(trimmed); ref != "" {
			exact = append(exact, ref)
		}
	}
	exact = uniqueSorted(exact)
	base := Evidence{
		WorkflowPath: workflowPath,
		JobName:      jobName,
		StepName:     stepName,
		CommandKind:  commandKind,
	}
	switch {
	case len(exact) == 1 && !unresolved:
		base.ImageRef = exact[0]
		base.EvidenceClass = EvidenceClassImageRef
		return base
	case len(exact) > 1:
		base.ImageRefs = exact
		base.EvidenceClass = EvidenceClassAmbiguous
		base.Reason = "multiple_image_refs_in_command"
		return base
	case unresolved:
		base.EvidenceClass = EvidenceClassUnresolved
		base.Reason = "image_ref_contains_unresolved_expression"
		return base
	default:
		base.EvidenceClass = EvidenceClassUnresolved
		base.Reason = "image_ref_missing_or_not_explicit"
		return base
	}
}

func isUnresolvedCandidate(value string) bool {
	return strings.Contains(value, "${") ||
		strings.Contains(value, "$") ||
		strings.Contains(value, "{{") ||
		strings.Contains(value, "}}")
}

func splitCommandSegments(command string) []string {
	replacer := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n")
	parts := strings.Split(replacer.Replace(command), "\n")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		segments = append(segments, trimmed)
	}
	return segments
}

func commandFields(command string) []string {
	command = strings.ReplaceAll(command, "\n", " ")
	command = strings.ReplaceAll(command, "\t", " ")
	fields := strings.Fields(command)
	for i := range fields {
		fields[i] = strings.Trim(fields[i], `"'`)
	}
	return fields
}

func decodeYAMLMaps(content string) ([]map[string]any, error) {
	decoder := yaml.NewDecoder(strings.NewReader(content))
	documents := make([]map[string]any, 0)
	for {
		var document map[string]any
		err := decoder.Decode(&document)
		if err != nil {
			if err == io.EOF {
				return documents, nil
			}
			return nil, err
		}
		if len(document) == 0 {
			continue
		}
		documents = append(documents, document)
	}
}

func dedupeEvidence(rows []Evidence) []Evidence {
	if len(rows) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(rows))
	out := make([]Evidence, 0, len(rows))
	for _, row := range rows {
		key := strings.Join([]string{
			row.WorkflowPath,
			row.JobName,
			row.StepName,
			row.CommandKind,
			row.ImageRef,
			strings.Join(row.ImageRefs, ","),
			row.EvidenceClass,
			row.Reason,
		}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].WorkflowPath != out[j].WorkflowPath {
			return out[i].WorkflowPath < out[j].WorkflowPath
		}
		if out[i].JobName != out[j].JobName {
			return out[i].JobName < out[j].JobName
		}
		if out[i].StepName != out[j].StepName {
			return out[i].StepName < out[j].StepName
		}
		if out[i].CommandKind != out[j].CommandKind {
			return out[i].CommandKind < out[j].CommandKind
		}
		return out[i].ImageRef < out[j].ImageRef
	})
	return out
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	out := values[:0]
	for _, value := range values {
		if value == "" {
			continue
		}
		if len(out) > 0 && out[len(out)-1] == value {
			continue
		}
		out = append(out, value)
	}
	return out
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
