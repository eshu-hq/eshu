// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	nodelockfile "github.com/eshu-hq/eshu/go/internal/parser/nodelockfile"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// parseNodeLockfile dispatches yarn.lock and pnpm-lock.yaml files to the
// nodelockfile adapter so the parent engine never imports lockfile-specific
// helpers.
func (e *Engine) parseNodeLockfile(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	return nodelockfile.Parse(path, isDependency, shared.Options{
		IndexSource:   options.IndexSource,
		VariableScope: options.VariableScope,
	})
}
