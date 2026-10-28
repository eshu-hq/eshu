// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Metadata is the Dockerfile runtime evidence extracted from source text.
type Metadata struct {
	Stages             []Stage
	Ports              []Port
	Args               []Arg
	Envs               []Env
	Labels             []Label
	Installs           []Install
	Unparsed           []UnparsedCommand
	DroppedCredentials int
}

// Stage records a Dockerfile FROM stage and the runtime fields attached to it.
type Stage struct {
	Name       string
	LineNumber int
	StageIndex int
	BaseImage  string
	BaseTag    string
	// BaseRefTemplate is the FROM image reference as written, set only when it
	// holds an ARG reference. BaseImage and BaseTag hold the expanded value.
	BaseRefTemplate string
	// UnresolvedArgs names the ARG references in the FROM line whose value is
	// not known at parse time. When set, BaseImage and BaseTag keep the
	// placeholder text.
	UnresolvedArgs []string
	// ArgDefaults holds ARG defaults seen for UnresolvedArgs, as evidence only.
	// It is bounded: at most maxArgDefaultsPerStage entries, each cut to
	// maxArgDefaultLen bytes. It never feeds resolution.
	ArgDefaults map[string]string
	Platform    string
	Alias       string
	Path        string
	CopiesFrom  string
	Workdir     string
	Entrypoint  string
	Cmd         string
	User        string
	Healthcheck string
	// ArgDefaultsTruncated names the ArgDefaults entries that were cut to the
	// per-value cap, so a reader knows the stored text is a prefix.
	ArgDefaultsTruncated []string
	// ArgDefaultsOmitted is set when a default was left out because the stage
	// already held the per-stage maximum.
	ArgDefaultsOmitted bool
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
	keyword           string
	line              int
	value             string
	heredoc           bool
	heredocExecutable bool
	heredocComplete   bool
	heredocBody       string
}

// RuntimeMetadata extracts Dockerfile stage, argument, environment, port, and
// label evidence from source text. It preserves the deterministic bucket order
// expected by the parent parser payload.
func RuntimeMetadata(sourceText string) Metadata {
	metadata := Metadata{
		Stages:   []Stage{},
		Ports:    []Port{},
		Args:     []Arg{},
		Envs:     []Env{},
		Labels:   []Label{},
		Installs: []Install{},
		Unparsed: []UnparsedCommand{},
	}

	escape := dockerfileEscapeRune(sourceText)
	instructions := instructionsFromSource(sourceText, escape)
	var currentStage *Stage
	stageIndex := 0
	resolver := newBaseRefResolver()
	stageScopes := make(map[string]*installScope)
	var installVars *installScope
	for _, item := range instructions {
		switch item.keyword {
		case "FROM":
			stage := parseStage(item, stageIndex, escape, resolver)
			installVars = stageInstallScope(stage, stageScopes)
			metadata.Stages = append(metadata.Stages, stage)
			currentStage = &metadata.Stages[len(metadata.Stages)-1]
			stageIndex++
		case "ARG":
			decls := parseArgDecls(item, escape)
			metadata.Args = append(metadata.Args, argRows(item, decls, currentStage)...)
			if currentStage == nil {
				for _, decl := range decls {
					resolver.declare(decl)
				}
			} else {
				noteStageArgDefaults(currentStage, decls)
				for _, decl := range decls {
					installVars.declareArg(decl, resolver)
				}
			}
		case "ENV":
			envRows := parseEnvs(item, currentStage, escape)
			metadata.Envs = append(metadata.Envs, envRows...)
			for _, row := range envRows {
				installVars.declareEnv(row.Name, row.Value)
			}
		case "RUN", "ADD":
			installs, unparsed, dropped := parseInstallInstruction(item, stageName(currentStage), installVars)
			metadata.Installs = append(metadata.Installs, installs...)
			metadata.Unparsed = append(metadata.Unparsed, unparsed...)
			metadata.DroppedCredentials += dropped
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
		"modules":                        []map[string]any{},
		"module_inclusions":              []map[string]any{},
		"dockerfile_stages":              labelsToMaps(m.Stages, stageMap),
		"dockerfile_ports":               labelsToMaps(m.Ports, portMap),
		"dockerfile_args":                labelsToMaps(m.Args, argMap),
		"dockerfile_envs":                labelsToMaps(m.Envs, envMap),
		"dockerfile_labels":              labelsToMaps(m.Labels, labelMap),
		"dockerfile_installs":            installRows(m.Installs),
		"dockerfile_unparsed":            labelsToMaps(m.Unparsed, unparsedMap),
		"dockerfile_dropped_credentials": m.DroppedCredentials,
	}
}

func parseStage(item instruction, stageIndex int, escape rune, resolver *baseRefResolver) Stage {
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
	var ref baseRef
	if len(fields) > 0 {
		ref = resolver.resolve(fields[0])
		image = ref.ref
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

		BaseRefTemplate: ref.template,
		UnresolvedArgs:  ref.unresolved,
		ArgDefaults:     ref.defaults.values,
		Alias:           alias,
		Path:            filepath.Base(name),

		ArgDefaultsTruncated: ref.defaults.truncated,
		ArgDefaultsOmitted:   ref.defaults.omitted,
	}
}

// parseArgDecls splits an ARG instruction into its name[=default] pairs.
func parseArgDecls(item instruction, escape rune) []argDecl {
	tokens := splitDockerfileWords(item.value, escape)
	decls := make([]argDecl, 0, len(tokens))
	for _, token := range tokens {
		name, value, hasDefault := strings.Cut(token, "=")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		decls = append(decls, argDecl{name: name, value: strings.TrimSpace(value), hasDefault: hasDefault})
	}
	return decls
}

func argRows(item instruction, decls []argDecl, currentStage *Stage) []Arg {
	rows := make([]Arg, 0, len(decls))
	for _, decl := range decls {
		rows = append(rows, Arg{
			Name:         decl.name,
			LineNumber:   item.line,
			DefaultValue: decl.value,
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
	addBaseRefEvidence(row, stage)
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
