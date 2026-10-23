// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomod

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"golang.org/x/mod/modfile"
)

// parseGoWork decodes one go.work file. A workspace declares member module
// directories (use) and workspace-wide replacements; it declares no
// requirements, so it emits no config_kind=dependency rows and its
// gomod_state carries no module_path (a workspace is not a module
// declaration). Malformed input follows the go.mod convention: zero rows and
// a malformed gomod_state carrying the upstream parse error.
func parseGoWork(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	payload := basePayload(path, isDependency)

	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	if options.IndexSource {
		payload["source"] = string(source)
	}

	parsed, err := modfile.ParseWork(filepath.Base(path), source, nil)
	if err != nil {
		payload["gomod_state"] = map[string]any{
			"state":       "malformed",
			"parse_error": err.Error(),
		}
		return payload, nil
	}

	rows := make([]map[string]any, 0, len(parsed.Use)+len(parsed.Replace))
	useCount := 0
	for _, use := range parsed.Use {
		row := goWorkUseRow(use)
		if row == nil {
			continue
		}
		rows = append(rows, row)
		useCount++
	}
	for _, replace := range parsed.Replace {
		row := goReplaceRow(replace)
		if row == nil {
			continue
		}
		rows = append(rows, row)
	}

	sortGoModRows(rows)
	payload["variables"] = rows
	payload["gomod_state"] = map[string]any{
		"state":            "parsed",
		"go_version":       goWorkDirectiveVersion(parsed),
		"toolchain":        goWorkToolchainName(parsed),
		"use_count":        useCount,
		"replace_count":    len(parsed.Replace),
		"replaced_modules": replacedModulePaths(parsed.Replace),
	}
	return payload, nil
}

// goWorkUseRow builds one workspace member row. The use path stays verbatim
// (for example "./api", "../shared", or a quoted " ./a " with its spaces):
// the parser does not trim it or resolve it against the workspace directory,
// because that would read repository state beyond the single file Parse was
// given. value is always the raw path. name is the raw path too, except when
// the path is blank after trimming: `use ""` is legal and the go command
// resolves it to the workspace root, but the content writers reject a blank
// entity name, so a blank path is named by its Go-quoted spelling (`""`),
// the way the file writes it. Every use entry yields a row.
func goWorkUseRow(use *modfile.Use) map[string]any {
	if use == nil {
		return nil
	}
	name := use.Path
	if strings.TrimSpace(name) == "" {
		name = strconv.Quote(use.Path)
	}
	return map[string]any{
		"name":            name,
		"line_number":     lineNumberFromSyntax(use.Syntax),
		"value":           use.Path,
		"section":         "use",
		"config_kind":     "workspace_use",
		"package_manager": PackageManager,
		"lang":            LanguageName,
		"lockfile":        false,
	}
}

func goWorkDirectiveVersion(parsed *modfile.WorkFile) string {
	if parsed == nil || parsed.Go == nil {
		return ""
	}
	return strings.TrimSpace(parsed.Go.Version)
}

func goWorkToolchainName(parsed *modfile.WorkFile) string {
	if parsed == nil || parsed.Toolchain == nil {
		return ""
	}
	return strings.TrimSpace(parsed.Toolchain.Name)
}
