// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import "strings"

// DockerfileInstallNamesFromParsed extracts index names from the parser's
// normalized dockerfile_installs rows. It accepts both in-process parser maps
// and JSON-decoded fact rows, without interpreting Dockerfile source again.
func DockerfileInstallNamesFromParsed(parsed map[string]any) []string {
	var rows []any
	switch value := parsed["dockerfile_installs"].(type) {
	case []map[string]any:
		rows = make([]any, 0, len(value))
		for _, row := range value {
			rows = append(rows, row)
		}
	case []any:
		rows = value
	default:
		return nil
	}
	names := make([]string, 0, len(rows))
	for _, item := range rows {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, ok := row["name"].(string)
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}
