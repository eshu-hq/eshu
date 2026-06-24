// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// ParsePipfile reads one Pipenv Pipfile and emits content_entity dependency
// rows for [packages] and [dev-packages]. Inline-table dependencies with a
// `git`, `path`, or `url` key surface as non-`dependency` config_kind so the
// supply-chain reducer cannot mis-admit them as PyPI registry consumption.
func ParsePipfile(path string) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	payload := basePayload(path, LangTOML)
	sections, err := scanTOML(string(source))
	if err != nil {
		return nil, err
	}

	rows := make([]map[string]any, 0)
	for _, section := range sections {
		switch section.Header {
		case "packages":
			rows = append(rows, parsePipfileTable(section, "packages", false)...)
		case "dev-packages":
			rows = append(rows, parsePipfileTable(section, "dev-packages", true)...)
		}
	}
	payload["variables"] = rows
	return payload, nil
}

func parsePipfileTable(section *tomlSection, sectionName string, dev bool) []map[string]any {
	rows := []map[string]any{}
	for _, key := range section.Keys {
		value := section.Values[key]
		row := poetryDependencyRow(key, value, sectionName, dev, section.StartLine)
		rows = append(rows, row)
	}
	return rows
}
