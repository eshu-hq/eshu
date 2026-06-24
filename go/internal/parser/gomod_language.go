// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	gomodparser "github.com/eshu-hq/eshu/go/internal/parser/gomod"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// parseGoModule dispatches go.mod and go.sum parsing through the gomod
// sub-package. The wrapper exists so the parent engine keeps the same
// per-language entrypoint shape used by every other parser and so the gomod
// package can stay free of parent-engine imports.
func (e *Engine) parseGoModule(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	return gomodparser.Parse(path, isDependency, shared.Options{
		IndexSource:   options.IndexSource,
		VariableScope: options.VariableScope,
	})
}
