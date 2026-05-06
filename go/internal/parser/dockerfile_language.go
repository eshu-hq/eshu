package parser

import (
	"bufio"
	"fmt"
	"path/filepath"
	"strings"
)

func (e *Engine) parseDockerfile(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	source, err := readSource(path)
	if err != nil {
		return nil, err
	}

	payload := basePayload(path, "dockerfile", isDependency)
	for key, value := range buildDockerfilePayload(string(source)) {
		payload[key] = value
	}
	if options.IndexSource {
		payload["source"] = string(source)
	}
	return payload, nil
}

// ExtractDockerfileRuntimeMetadata returns the parser-backed Dockerfile payload
// without repository-specific metadata so read-side query code can surface the
// same stage/runtime signals the parser already proves during ingestion.
func ExtractDockerfileRuntimeMetadata(sourceText string) map[string]any {
	return buildDockerfilePayload(sourceText)
}

func buildDockerfilePayload(sourceText string) map[string]any {
	payload := map[string]any{
		"modules":           []map[string]any{},
		"module_inclusions": []map[string]any{},
		"dockerfile_stages": []map[string]any{},
		"dockerfile_ports":  []map[string]any{},
		"dockerfile_args":   []map[string]any{},
		"dockerfile_envs":   []map[string]any{},
		"dockerfile_labels": []map[string]any{},
	}

	instructions := dockerfileInstructions(sourceText)
	var currentStage map[string]any
	stageIndex := 0
	for _, instruction := range instructions {
		switch instruction.keyword {
		case "FROM":
			currentStage = parseDockerfileStage(instruction, stageIndex)
			appendBucket(payload, "dockerfile_stages", currentStage)
			stageIndex++
		case "ARG":
			if item := parseDockerfileArg(instruction, currentStage); item != nil {
				appendBucket(payload, "dockerfile_args", item)
			}
		case "ENV":
			for _, item := range parseDockerfileEnvs(instruction, currentStage) {
				appendBucket(payload, "dockerfile_envs", item)
			}
		case "EXPOSE":
			for _, item := range parseDockerfilePorts(instruction, currentStage) {
				appendBucket(payload, "dockerfile_ports", item)
			}
		case "LABEL":
			for _, item := range parseDockerfileLabels(instruction, currentStage) {
				appendBucket(payload, "dockerfile_labels", item)
			}
		case "COPY":
			annotateDockerfileCopyFrom(instruction, currentStage)
		case "WORKDIR":
			setDockerfileStageField(currentStage, "workdir", instruction.value)
		case "ENTRYPOINT":
			setDockerfileStageField(currentStage, "entrypoint", instruction.value)
		case "CMD":
			setDockerfileStageField(currentStage, "cmd", instruction.value)
		case "USER":
			setDockerfileStageField(currentStage, "user", instruction.value)
		case "HEALTHCHECK":
			setDockerfileStageField(currentStage, "healthcheck", instruction.value)
		}
	}

	sortNamedBucket(payload, "dockerfile_stages")
	sortNamedBucket(payload, "dockerfile_ports")
	sortNamedBucket(payload, "dockerfile_args")
	sortNamedBucket(payload, "dockerfile_envs")
	sortNamedBucket(payload, "dockerfile_labels")
	return payload
}

type dockerfileInstruction struct {
	keyword string
	line    int
	value   string
}

func dockerfileInstructions(source string) []dockerfileInstruction {
	scanner := bufio.NewScanner(strings.NewReader(source))
	instructions := make([]dockerfileInstruction, 0)
	var (
		buffer    strings.Builder
		startLine int
		line      int
	)

	flush := func() {
		raw := strings.TrimSpace(buffer.String())
		buffer.Reset()
		if raw == "" || strings.HasPrefix(raw, "#") {
			return
		}
		parts := strings.Fields(raw)
		if len(parts) == 0 {
			return
		}
		keyword := strings.ToUpper(parts[0])
		value := strings.TrimSpace(strings.TrimPrefix(raw, parts[0]))
		instructions = append(instructions, dockerfileInstruction{
			keyword: keyword,
			line:    startLine,
			value:   value,
		})
	}

	for scanner.Scan() {
		line++
		text := scanner.Text()
		trimmed := strings.TrimSpace(text)
		if trimmed == "" && buffer.Len() == 0 {
			continue
		}
		if buffer.Len() == 0 {
			startLine = line
		} else {
			buffer.WriteByte(' ')
		}
		buffer.WriteString(strings.TrimSpace(strings.TrimSuffix(text, "\\")))
		if strings.HasSuffix(strings.TrimSpace(text), "\\") {
			continue
		}
		flush()
	}
	flush()

	return instructions
}

func parseDockerfileStage(instruction dockerfileInstruction, stageIndex int) map[string]any {
	fields := strings.Fields(instruction.value)
	image := ""
	tag := ""
	alias := ""
	if len(fields) > 0 {
		image = fields[0]
	}
	if separator := strings.Index(image, ":"); separator >= 0 {
		tag = image[separator+1:]
		image = image[:separator]
	}
	for index := 1; index+1 < len(fields); index++ {
		if strings.EqualFold(fields[index], "AS") {
			alias = fields[index+1]
			break
		}
	}
	name := alias
	if strings.TrimSpace(name) == "" {
		name = image
	}
	if strings.TrimSpace(name) == "" {
		name = fmt.Sprintf("stage_%d", stageIndex)
	}
	return map[string]any{
		"name":        name,
		"line_number": instruction.line,
		"stage_index": stageIndex,
		"base_image":  image,
		"base_tag":    tag,
		"alias":       alias,
		"path":        filepath.Base(name),
		"lang":        "dockerfile",
	}
}

func parseDockerfileArg(
	instruction dockerfileInstruction,
	currentStage map[string]any,
) map[string]any {
	name, value, _ := strings.Cut(instruction.value, "=")
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	row := map[string]any{
		"name":          name,
		"line_number":   instruction.line,
		"default_value": strings.TrimSpace(value),
	}
	if stageName := dockerfileStageName(currentStage); stageName != "" {
		row["stage"] = stageName
	}
	return row
}

func parseDockerfileEnvs(
	instruction dockerfileInstruction,
	currentStage map[string]any,
) []map[string]any {
	pairs := splitKeyValueTokens(instruction.value)
	rows := make([]map[string]any, 0, len(pairs))
	for name, value := range pairs {
		row := map[string]any{
			"name":        name,
			"value":       value,
			"line_number": instruction.line,
		}
		if stageName := dockerfileStageName(currentStage); stageName != "" {
			row["stage"] = stageName
		}
		rows = append(rows, row)
	}
	sortNamedMaps(rows)
	return rows
}

func parseDockerfilePorts(
	instruction dockerfileInstruction,
	currentStage map[string]any,
) []map[string]any {
	stageName := dockerfileStageName(currentStage)
	if stageName == "" {
		stageName = "global"
	}
	fields := strings.Fields(instruction.value)
	rows := make([]map[string]any, 0, len(fields))
	for _, field := range fields {
		port, protocol, found := strings.Cut(field, "/")
		if !found {
			protocol = "tcp"
		}
		rows = append(rows, map[string]any{
			"name":        stageName + ":" + strings.TrimSpace(port),
			"port":        strings.TrimSpace(port),
			"protocol":    strings.TrimSpace(protocol),
			"line_number": instruction.line,
			"stage":       stageName,
		})
	}
	sortNamedMaps(rows)
	return rows
}

func parseDockerfileLabels(
	instruction dockerfileInstruction,
	currentStage map[string]any,
) []map[string]any {
	pairs := splitKeyValueTokens(instruction.value)
	rows := make([]map[string]any, 0, len(pairs))
	for name, value := range pairs {
		row := map[string]any{
			"name":        name,
			"value":       strings.Trim(value, `"'`),
			"line_number": instruction.line,
		}
		if stageName := dockerfileStageName(currentStage); stageName != "" {
			row["stage"] = stageName
		}
		rows = append(rows, row)
	}
	sortNamedMaps(rows)
	return rows
}

func annotateDockerfileCopyFrom(instruction dockerfileInstruction, currentStage map[string]any) {
	if currentStage == nil {
		return
	}
	for _, field := range strings.Fields(instruction.value) {
		if strings.HasPrefix(field, "--from=") {
			currentStage["copies_from"] = strings.TrimPrefix(field, "--from=")
			return
		}
	}
}

func setDockerfileStageField(stage map[string]any, key string, value string) {
	if stage == nil {
		return
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	stage[key] = value
}

func dockerfileStageName(stage map[string]any) string {
	if stage == nil {
		return ""
	}
	name, _ := stage["name"].(string)
	return name
}

func splitKeyValueTokens(raw string) map[string]string {
	result := make(map[string]string)
	for _, field := range strings.Fields(raw) {
		name, value, found := strings.Cut(field, "=")
		if !found {
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" {
			continue
		}
		result[name] = value
	}
	return result
}
