package dockerfile

import (
	"bufio"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Metadata is the Dockerfile runtime evidence extracted from source text.
type Metadata struct {
	Stages []Stage
	Ports  []Port
	Args   []Arg
	Envs   []Env
	Labels []Label
}

// Stage records a Dockerfile FROM stage and the runtime fields attached to it.
type Stage struct {
	Name        string
	LineNumber  int
	StageIndex  int
	BaseImage   string
	BaseTag     string
	Platform    string
	Alias       string
	Path        string
	CopiesFrom  string
	Workdir     string
	Entrypoint  string
	Cmd         string
	User        string
	Healthcheck string
}

// Arg records a Dockerfile ARG instruction.
type Arg struct {
	Name         string
	LineNumber   int
	DefaultValue string
	Stage        string
}

// Env records a Dockerfile ENV assignment.
type Env struct {
	Name       string
	Value      string
	LineNumber int
	Stage      string
}

// Port records a Dockerfile EXPOSE entry.
type Port struct {
	Name       string
	Port       string
	Protocol   string
	LineNumber int
	Stage      string
}

// Label records a Dockerfile LABEL assignment.
type Label struct {
	Name       string
	Value      string
	LineNumber int
	Stage      string
}

type instruction struct {
	keyword string
	line    int
	value   string
}

// RuntimeMetadata extracts Dockerfile stage, argument, environment, port, and
// label evidence from source text. It preserves the deterministic bucket order
// expected by the parent parser payload.
func RuntimeMetadata(sourceText string) Metadata {
	metadata := Metadata{
		Stages: []Stage{},
		Ports:  []Port{},
		Args:   []Arg{},
		Envs:   []Env{},
		Labels: []Label{},
	}

	escape := dockerfileEscapeRune(sourceText)
	instructions := instructionsFromSource(sourceText, escape)
	var currentStage *Stage
	stageIndex := 0
	for _, item := range instructions {
		switch item.keyword {
		case "FROM":
			stage := parseStage(item, stageIndex, escape)
			metadata.Stages = append(metadata.Stages, stage)
			currentStage = &metadata.Stages[len(metadata.Stages)-1]
			stageIndex++
		case "ARG":
			metadata.Args = append(metadata.Args, parseArgs(item, currentStage, escape)...)
		case "ENV":
			metadata.Envs = append(metadata.Envs, parseEnvs(item, currentStage, escape)...)
		case "EXPOSE":
			metadata.Ports = append(metadata.Ports, parsePorts(item, currentStage)...)
		case "LABEL":
			metadata.Labels = append(metadata.Labels, parseLabels(item, currentStage, escape)...)
		case "COPY":
			annotateCopyFrom(item, currentStage, escape)
		case "WORKDIR":
			setStageField(currentStage, item.value, func(stage *Stage, value string) { stage.Workdir = value })
		case "ENTRYPOINT":
			setStageField(currentStage, item.value, func(stage *Stage, value string) { stage.Entrypoint = value })
		case "CMD":
			setStageField(currentStage, item.value, func(stage *Stage, value string) { stage.Cmd = value })
		case "USER":
			setStageField(currentStage, item.value, func(stage *Stage, value string) { stage.User = value })
		case "HEALTHCHECK":
			setStageField(currentStage, item.value, func(stage *Stage, value string) { stage.Healthcheck = value })
		}
	}

	sortNamed(metadata.Stages, func(item Stage) string { return item.Name })
	sortNamed(metadata.Ports, func(item Port) string { return item.Name })
	sortNamed(metadata.Args, func(item Arg) string { return item.Name })
	sortNamed(metadata.Envs, func(item Env) string { return item.Name })
	sortNamed(metadata.Labels, func(item Label) string { return item.Name })
	return metadata
}

// Map returns the parent parser payload shape used by existing query and
// relationship callers.
func (m Metadata) Map() map[string]any {
	return map[string]any{
		"modules":           []map[string]any{},
		"module_inclusions": []map[string]any{},
		"dockerfile_stages": labelsToMaps(m.Stages, stageMap),
		"dockerfile_ports":  labelsToMaps(m.Ports, portMap),
		"dockerfile_args":   labelsToMaps(m.Args, argMap),
		"dockerfile_envs":   labelsToMaps(m.Envs, envMap),
		"dockerfile_labels": labelsToMaps(m.Labels, labelMap),
	}
}

func instructionsFromSource(source string, escape rune) []instruction {
	scanner := bufio.NewScanner(strings.NewReader(source))
	instructions := make([]instruction, 0)
	escapeText := string(escape)
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
		instructions = append(instructions, instruction{keyword: keyword, line: startLine, value: value})
	}

	for scanner.Scan() {
		line++
		text := scanner.Text()
		trimmed := strings.TrimSpace(text)
		if trimmed == "" && buffer.Len() == 0 {
			continue
		}
		if strings.HasPrefix(trimmed, "#") && buffer.Len() == 0 {
			continue
		}
		if buffer.Len() == 0 {
			startLine = line
		} else {
			buffer.WriteByte(' ')
		}
		trimmedText := strings.TrimSpace(text)
		buffer.WriteString(strings.TrimSpace(strings.TrimSuffix(text, escapeText)))
		if strings.HasSuffix(trimmedText, escapeText) {
			continue
		}
		flush()
	}
	flush()
	return instructions
}

func parseStage(item instruction, stageIndex int, escape rune) Stage {
	fields := splitDockerfileWords(item.value, escape)
	platform := ""
	for len(fields) > 0 && strings.HasPrefix(fields[0], "--") {
		flag := strings.TrimPrefix(fields[0], "--")
		fields = fields[1:]
		name, value, hasValue := strings.Cut(flag, "=")
		if !hasValue && len(fields) > 0 {
			value = fields[0]
			fields = fields[1:]
		}
		if strings.EqualFold(name, "platform") {
			platform = value
		}
	}

	image := ""
	tag := ""
	alias := ""
	if len(fields) > 0 {
		image = fields[0]
	}
	image, tag = splitImageTag(image)
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
	return Stage{
		Name:       name,
		LineNumber: item.line,
		StageIndex: stageIndex,
		BaseImage:  image,
		BaseTag:    tag,
		Platform:   platform,
		Alias:      alias,
		Path:       filepath.Base(name),
	}
}

func splitImageTag(image string) (string, string) {
	if strings.Contains(image, "@") {
		return image, ""
	}
	tagIndex := strings.LastIndex(image, ":")
	slashIndex := strings.LastIndex(image, "/")
	if tagIndex < 0 || tagIndex < slashIndex {
		return image, ""
	}
	return image[:tagIndex], image[tagIndex+1:]
}

func parseArgs(item instruction, currentStage *Stage, escape rune) []Arg {
	tokens := splitDockerfileWords(item.value, escape)
	rows := make([]Arg, 0, len(tokens))
	for _, token := range tokens {
		name, value, _ := strings.Cut(token, "=")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		rows = append(rows, Arg{
			Name:         name,
			LineNumber:   item.line,
			DefaultValue: strings.TrimSpace(value),
			Stage:        stageName(currentStage),
		})
	}
	return rows
}

func parseEnvs(item instruction, currentStage *Stage, escape rune) []Env {
	pairs := splitEnvTokens(item.value, escape)
	rows := make([]Env, 0, len(pairs))
	for _, pair := range pairs {
		rows = append(rows, Env{
			Name:       pair.name,
			Value:      pair.value,
			LineNumber: item.line,
			Stage:      stageName(currentStage),
		})
	}
	sortNamed(rows, func(item Env) string { return item.Name })
	return rows
}

func splitEnvTokens(raw string, escape rune) []keyValueToken {
	fields := splitDockerfileWords(raw, escape)
	if len(fields) < 2 || strings.Contains(fields[0], "=") {
		return splitKeyValueTokens(raw, escape)
	}
	return []keyValueToken{{
		name:  strings.TrimSpace(fields[0]),
		value: strings.TrimSpace(strings.Join(fields[1:], " ")),
	}}
}

func parsePorts(item instruction, currentStage *Stage) []Port {
	name := stageName(currentStage)
	if name == "" {
		name = "global"
	}
	fields := strings.Fields(item.value)
	rows := make([]Port, 0, len(fields))
	for _, field := range fields {
		port, protocol, found := strings.Cut(field, "/")
		if !found {
			protocol = "tcp"
		}
		port = strings.TrimSpace(port)
		rows = append(rows, Port{
			Name:       name + ":" + port,
			Port:       port,
			Protocol:   strings.TrimSpace(protocol),
			LineNumber: item.line,
			Stage:      name,
		})
	}
	sortNamed(rows, func(item Port) string { return item.Name })
	return rows
}

func parseLabels(item instruction, currentStage *Stage, escape rune) []Label {
	pairs := splitKeyValueTokens(item.value, escape)
	rows := make([]Label, 0, len(pairs))
	for _, pair := range pairs {
		rows = append(rows, Label{
			Name:       pair.name,
			Value:      pair.value,
			LineNumber: item.line,
			Stage:      stageName(currentStage),
		})
	}
	sortNamed(rows, func(item Label) string { return item.Name })
	return rows
}

func annotateCopyFrom(item instruction, currentStage *Stage, escape rune) {
	if currentStage == nil {
		return
	}
	for _, field := range splitDockerfileWords(item.value, escape) {
		if strings.HasPrefix(field, "--from=") {
			currentStage.CopiesFrom = strings.TrimPrefix(field, "--from=")
			return
		}
	}
}

func setStageField(currentStage *Stage, raw string, assign func(*Stage, string)) {
	if currentStage == nil {
		return
	}
	value := strings.TrimSpace(raw)
	if value == "" {
		return
	}
	assign(currentStage, value)
}

func stageName(stage *Stage) string {
	if stage == nil {
		return ""
	}
	return stage.Name
}

func sortNamed[T any](values []T, name func(T) string) {
	slices.SortFunc(values, func(a, b T) int {
		return strings.Compare(name(a), name(b))
	})
}

func labelsToMaps[T any](values []T, convert func(T) map[string]any) []map[string]any {
	rows := make([]map[string]any, 0, len(values))
	for _, value := range values {
		rows = append(rows, convert(value))
	}
	return rows
}

func stageMap(stage Stage) map[string]any {
	row := map[string]any{
		"name":        stage.Name,
		"line_number": stage.LineNumber,
		"stage_index": stage.StageIndex,
		"base_image":  stage.BaseImage,
		"base_tag":    stage.BaseTag,
		"alias":       stage.Alias,
		"path":        stage.Path,
		"lang":        "dockerfile",
	}
	addOptional(row, "platform", stage.Platform)
	addOptional(row, "copies_from", stage.CopiesFrom)
	addOptional(row, "workdir", stage.Workdir)
	addOptional(row, "entrypoint", stage.Entrypoint)
	addOptional(row, "cmd", stage.Cmd)
	addOptional(row, "user", stage.User)
	addOptional(row, "healthcheck", stage.Healthcheck)
	return row
}

func argMap(arg Arg) map[string]any {
	row := map[string]any{
		"name":          arg.Name,
		"line_number":   arg.LineNumber,
		"default_value": arg.DefaultValue,
	}
	addOptional(row, "stage", arg.Stage)
	return row
}

func envMap(env Env) map[string]any {
	row := map[string]any{"name": env.Name, "value": env.Value, "line_number": env.LineNumber}
	addOptional(row, "stage", env.Stage)
	return row
}

func portMap(port Port) map[string]any {
	return map[string]any{
		"name":        port.Name,
		"port":        port.Port,
		"protocol":    port.Protocol,
		"line_number": port.LineNumber,
		"stage":       port.Stage,
	}
}

func labelMap(label Label) map[string]any {
	row := map[string]any{"name": label.Name, "value": label.Value, "line_number": label.LineNumber}
	addOptional(row, "stage", label.Stage)
	return row
}

func addOptional(row map[string]any, key string, value string) {
	if strings.TrimSpace(value) != "" {
		row[key] = value
	}
}
