// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import jsparser "github.com/eshu-hq/eshu/go/internal/parser/javascript"

func javaScriptExpressServerSymbols(express map[string]any) []string {
	return jsparser.ExpressServerSymbols(express)
}
