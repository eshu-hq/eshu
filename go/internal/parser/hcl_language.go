// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	hclparser "github.com/eshu-hq/eshu/go/internal/parser/hcl"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func (e *Engine) parseHCL(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	return hclparser.Parse(path, isDependency, shared.Options{
		IndexSource:   options.IndexSource,
		VariableScope: options.VariableScope,
	})
}
