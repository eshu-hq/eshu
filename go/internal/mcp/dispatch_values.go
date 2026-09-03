// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

func str(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func intOr(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return def
	}
}

func intString(args map[string]any, key string, def int) string {
	return strconv.Itoa(intOr(args, key, def))
}

func boolOr(args map[string]any, key string, def bool) bool {
	v, ok := args[key].(bool)
	if !ok {
		return def
	}
	return v
}

func paginationQuery(args map[string]any, defaultLimit int) map[string]string {
	return map[string]string{
		"limit":  strconv.Itoa(intOr(args, "limit", defaultLimit)),
		"offset": strconv.Itoa(intOr(args, "offset", 0)),
	}
}
