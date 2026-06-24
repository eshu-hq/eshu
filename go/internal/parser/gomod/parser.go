// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomod

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"golang.org/x/mod/modfile"
)

// LanguageName is the lang tag stamped on every payload this package returns
// and registered with the parent parser engine. It is intentionally distinct
// from `"go"` so per-file Go source parsing and per-module manifest parsing
// stay independently routable.
const LanguageName = "gomod"

// PackageManager is the canonical ecosystem identifier emitted on every
// dependency row. The supply-chain reducer normalizes through this value when
// matching repository manifest evidence to package-registry identity.
const PackageManager = "go"

// Parse decodes one go.mod or go.sum file and returns the parser payload the
// parent engine expects. The returned payload always has the standard parser
// buckets present so callers do not branch on `nil` slices.
//
// Parse never invents installed-version evidence: go.mod rows carry the
// source-truth require version; replace targets surface separately as both
// metadata on the require row and a standalone replace row; go.sum rows are
// emitted with `ambiguous: true` so the consumption reducer leaves them out
// of admitted decisions. A malformed go.mod returns a payload with no
// dependency rows and a `gomod_state` envelope describing the parse error.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	filename := strings.ToLower(filepath.Base(path))
	switch filename {
	case "go.mod":
		return parseGoMod(path, isDependency, options)
	case "go.sum":
		return parseGoSum(path, isDependency, options)
	}
	return nil, fmt.Errorf("gomod parser cannot parse %q: only go.mod and go.sum are supported", filepath.Base(path))
}

func parseGoMod(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	payload := basePayload(path, isDependency)

	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	if options.IndexSource {
		payload["source"] = string(source)
	}

	parsed, err := modfile.Parse(filepath.Base(path), source, nil)
	if err != nil {
		payload["gomod_state"] = map[string]any{
			"state":       "malformed",
			"parse_error": err.Error(),
		}
		return payload, nil
	}

	rows := make([]map[string]any, 0, len(parsed.Require)+len(parsed.Replace)+len(parsed.Exclude))

	moduleRow := goModuleDeclarationRow(parsed)
	if moduleRow != nil {
		rows = append(rows, moduleRow)
	}

	for _, require := range parsed.Require {
		row := goRequireRow(require, parsed.Replace)
		if row == nil {
			continue
		}
		rows = append(rows, row)
	}
	for _, replace := range parsed.Replace {
		row := goReplaceRow(replace)
		if row == nil {
			continue
		}
		rows = append(rows, row)
	}
	for _, exclude := range parsed.Exclude {
		row := goExcludeRow(exclude)
		if row == nil {
			continue
		}
		rows = append(rows, row)
	}
	for _, retract := range parsed.Retract {
		row := goRetractRow(retract)
		if row == nil {
			continue
		}
		rows = append(rows, row)
	}

	sortGoModRows(rows)
	payload["variables"] = rows
	payload["gomod_state"] = map[string]any{
		"state":            "parsed",
		"module_path":      moduleDeclarationPath(parsed),
		"go_version":       goDirectiveVersion(parsed),
		"toolchain":        toolchainName(parsed),
		"require_count":    len(parsed.Require),
		"replace_count":    len(parsed.Replace),
		"exclude_count":    len(parsed.Exclude),
		"retract_count":    len(parsed.Retract),
		"indirect_count":   countIndirectRequires(parsed.Require),
		"replaced_modules": replacedModulePaths(parsed.Replace),
	}
	return payload, nil
}

func basePayload(path string, isDependency bool) map[string]any {
	payload := shared.BasePayload(path, LanguageName, isDependency)
	payload["modules"] = []map[string]any{}
	payload["module_inclusions"] = []map[string]any{}
	return payload
}

func goModuleDeclarationRow(parsed *modfile.File) map[string]any {
	if parsed == nil || parsed.Module == nil {
		return nil
	}
	modulePath := strings.TrimSpace(parsed.Module.Mod.Path)
	if modulePath == "" {
		return nil
	}
	row := map[string]any{
		"name":            modulePath,
		"line_number":     lineNumberFromSyntax(parsed.Module.Syntax),
		"value":           modulePath,
		"section":         "module",
		"config_kind":     "module_declaration",
		"package_manager": PackageManager,
		"lang":            LanguageName,
		"lockfile":        false,
	}
	return row
}

func goRequireRow(require *modfile.Require, replaces []*modfile.Replace) map[string]any {
	if require == nil {
		return nil
	}
	modulePath := strings.TrimSpace(require.Mod.Path)
	if modulePath == "" {
		return nil
	}
	requiredVersion := strings.TrimSpace(require.Mod.Version)
	section := "require"
	if require.Indirect {
		section = "require-indirect"
	}

	resolvedPath := modulePath
	resolvedVersion := requiredVersion
	replacementPath := ""
	replacementVersion := ""
	replaceFromVersion := ""
	if match := matchReplace(modulePath, requiredVersion, replaces); match != nil {
		replacementPath = strings.TrimSpace(match.New.Path)
		replacementVersion = strings.TrimSpace(match.New.Version)
		replaceFromVersion = strings.TrimSpace(match.Old.Version)
		if replacementVersion != "" {
			if replacementPath != "" {
				resolvedPath = replacementPath
			}
			resolvedVersion = replacementVersion
		}
	}

	row := map[string]any{
		"name":                 modulePath,
		"line_number":          lineNumberFromSyntax(require.Syntax),
		"value":                requiredVersion,
		"section":              section,
		"config_kind":          "dependency",
		"package_manager":      PackageManager,
		"lang":                 LanguageName,
		"lockfile":             false,
		"direct_dependency":    !require.Indirect,
		"indirect":             require.Indirect,
		"dependency_path":      []string{modulePath},
		"dependency_depth":     1,
		"resolved_module_path": resolvedPath,
		"resolved_version":     resolvedVersion,
	}
	if replacementPath != "" || replacementVersion != "" {
		row["replacement_path"] = replacementPath
		row["replacement_version"] = replacementVersion
	}
	if replaceFromVersion != "" {
		row["replace_from_version"] = replaceFromVersion
	}
	return row
}

func goReplaceRow(replace *modfile.Replace) map[string]any {
	if replace == nil {
		return nil
	}
	oldPath := strings.TrimSpace(replace.Old.Path)
	if oldPath == "" {
		return nil
	}
	targetPath := strings.TrimSpace(replace.New.Path)
	targetVersion := strings.TrimSpace(replace.New.Version)
	oldVersion := strings.TrimSpace(replace.Old.Version)
	row := map[string]any{
		"name":            oldPath,
		"line_number":     lineNumberFromSyntax(replace.Syntax),
		"value":           oldVersion,
		"section":         "replace",
		"config_kind":     "dependency_replace",
		"package_manager": PackageManager,
		"lang":            LanguageName,
		"lockfile":        false,
		"target_module":   targetPath,
		"target_version":  targetVersion,
	}
	if oldVersion != "" {
		row["replace_from_version"] = oldVersion
	}
	return row
}

func goExcludeRow(exclude *modfile.Exclude) map[string]any {
	if exclude == nil {
		return nil
	}
	modulePath := strings.TrimSpace(exclude.Mod.Path)
	if modulePath == "" {
		return nil
	}
	return map[string]any{
		"name":            modulePath,
		"line_number":     lineNumberFromSyntax(exclude.Syntax),
		"value":           strings.TrimSpace(exclude.Mod.Version),
		"section":         "exclude",
		"config_kind":     "dependency_exclude",
		"package_manager": PackageManager,
		"lang":            LanguageName,
		"lockfile":        false,
	}
}

func goRetractRow(retract *modfile.Retract) map[string]any {
	if retract == nil {
		return nil
	}
	low := strings.TrimSpace(retract.Low)
	high := strings.TrimSpace(retract.High)
	if low == "" && high == "" {
		return nil
	}
	value := low
	if high != "" && high != low {
		value = low + "-" + high
	}
	return map[string]any{
		"name":            "retract",
		"line_number":     lineNumberFromSyntax(retract.Syntax),
		"value":           value,
		"section":         "retract",
		"config_kind":     "dependency_retract",
		"package_manager": PackageManager,
		"lang":            LanguageName,
		"lockfile":        false,
		"retract_low":     low,
		"retract_high":    high,
		"retract_reason":  strings.TrimSpace(retract.Rationale),
	}
}

func matchReplace(path, version string, replaces []*modfile.Replace) *modfile.Replace {
	var versionMatch *modfile.Replace
	var pathMatch *modfile.Replace
	for _, candidate := range replaces {
		if candidate == nil {
			continue
		}
		if strings.TrimSpace(candidate.Old.Path) != path {
			continue
		}
		oldVersion := strings.TrimSpace(candidate.Old.Version)
		if oldVersion == "" && pathMatch == nil {
			pathMatch = candidate
			continue
		}
		if oldVersion == version {
			versionMatch = candidate
			break
		}
	}
	if versionMatch != nil {
		return versionMatch
	}
	return pathMatch
}

func lineNumberFromSyntax(line *modfile.Line) int {
	if line == nil {
		return 1
	}
	if line.Start.Line <= 0 {
		return 1
	}
	return line.Start.Line
}

func moduleDeclarationPath(parsed *modfile.File) string {
	if parsed == nil || parsed.Module == nil {
		return ""
	}
	return strings.TrimSpace(parsed.Module.Mod.Path)
}

func goDirectiveVersion(parsed *modfile.File) string {
	if parsed == nil || parsed.Go == nil {
		return ""
	}
	return strings.TrimSpace(parsed.Go.Version)
}

func toolchainName(parsed *modfile.File) string {
	if parsed == nil || parsed.Toolchain == nil {
		return ""
	}
	return strings.TrimSpace(parsed.Toolchain.Name)
}

func countIndirectRequires(requires []*modfile.Require) int {
	count := 0
	for _, require := range requires {
		if require != nil && require.Indirect {
			count++
		}
	}
	return count
}

func replacedModulePaths(replaces []*modfile.Replace) []string {
	if len(replaces) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(replaces))
	for _, replace := range replaces {
		if replace == nil {
			continue
		}
		path := strings.TrimSpace(replace.Old.Path)
		if path == "" {
			continue
		}
		seen[path] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func sortGoModRows(rows []map[string]any) {
	sort.SliceStable(rows, func(i, j int) bool {
		li, _ := rows[i]["line_number"].(int)
		lj, _ := rows[j]["line_number"].(int)
		if li != lj {
			return li < lj
		}
		ni, _ := rows[i]["name"].(string)
		nj, _ := rows[j]["name"].(string)
		if ni != nj {
			return ni < nj
		}
		si, _ := rows[i]["section"].(string)
		sj, _ := rows[j]["section"].(string)
		return si < sj
	})
}
