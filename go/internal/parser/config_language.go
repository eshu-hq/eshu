// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"github.com/eshu-hq/eshu/go/internal/parser/phpconfig"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// parsePHPConfig emits only guarded structured rows from metadata-only files.
func parsePHPConfig(path string, isDependency bool) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	parsed := phpconfig.Parse(path, source)
	payload := shared.BasePayload(path, "php_config", isDependency)
	rows := make([]map[string]any, 0, len(parsed.Directives)+len(parsed.Modes))
	for _, directive := range parsed.Directives {
		rows = append(rows, map[string]any{
			"name":                  "php_directive:" + directive.Name,
			"value":                 directive.Value,
			"section":               directive.Section,
			"line_number":           directive.Line,
			"config_kind":           "php_directive",
			"source_cache_withheld": true,
		})
	}
	for _, mode := range parsed.Modes {
		rows = append(rows, map[string]any{
			"name":                  "php_server_mode:" + mode.Mode,
			"value":                 mode.Mode,
			"server_mode":           mode.Mode,
			"line_number":           mode.Line,
			"config_kind":           "php_server_mode",
			"source_cache_withheld": true,
		})
	}
	payload["variables"] = rows
	payload["php_config_dropped"] = parsed.Dropped
	payload["php_config_unparsed"] = parsed.Unparsed
	payload["php_config_skips"] = parsed.Skipped
	return payload, nil
}
