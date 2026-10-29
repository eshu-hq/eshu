// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import "strings"

// phpDockerfileModeRows derives PHP server-mode entities from the normalized
// Dockerfile metadata already produced by the shared parser. Only the final
// stage is considered a runtime declaration.
func phpDockerfileModeRows(metadata map[string]any) []map[string]any {
	stages, _ := metadata["dockerfile_stages"].([]map[string]any)
	if len(stages) == 0 {
		return nil
	}
	stage := stages[0]
	for _, candidate := range stages[1:] {
		index, _ := candidate["stage_index"].(int)
		current, _ := stage["stage_index"].(int)
		if index > current {
			stage = candidate
		}
	}
	stageName, _ := stage["name"].(string)
	rows := make([]map[string]any, 0, 2)
	image, _ := stage["base_image"].(string)
	tag, _ := stage["base_tag"].(string)
	if strings.EqualFold(image, "php") || strings.HasSuffix(strings.ToLower(image), "/php") {
		mode := phpModeFromBaseTag(tag)
		if mode != "" {
			line, _ := stage["line_number"].(int)
			rows = append(rows, phpDockerfileModeRow(mode, "dockerfile_base_tag", line, stageName))
		}
	}
	installs, _ := metadata["dockerfile_installs"].([]map[string]any)
	for _, install := range installs {
		manager, _ := install["manager"].(string)
		name, _ := install["name"].(string)
		installStage, _ := install["stage"].(string)
		if manager != "a2enmod" || !isPHPApacheModule(name) || installStage != stageName {
			continue
		}
		line, _ := install["line_number"].(int)
		rows = append(rows, phpDockerfileModeRow("mod_php", "a2enmod", line, stageName))
	}
	return rows
}

func phpDockerfileModeRow(mode, source string, line int, stage string) map[string]any {
	return map[string]any{
		"name":        "php_server_mode:" + mode,
		"value":       mode,
		"server_mode": mode,
		"line_number": line,
		"config_kind": "php_server_mode",
		"source_kind": source,
		"stage":       stage,
	}
}

func isPHPApacheModule(name string) bool {
	name = strings.ToLower(name)
	if name == "php" {
		return true
	}
	version, ok := strings.CutPrefix(name, "php")
	if !ok || version == "" || version[0] < '0' || version[0] > '9' {
		return false
	}
	for _, ch := range version {
		if (ch < '0' || ch > '9') && ch != '.' {
			return false
		}
	}
	return true
}

func phpModeFromBaseTag(tag string) string {
	if tag == "" || strings.ContainsAny(tag, "${}") {
		return ""
	}
	apache, fpm := false, false
	for _, part := range strings.Split(strings.ToLower(tag), "-") {
		if part == "apache" {
			apache = true
		}
		if part == "fpm" {
			fpm = true
		}
	}
	if apache == fpm {
		return ""
	}
	if apache {
		return "mod_php"
	}
	return "php-fpm"
}
