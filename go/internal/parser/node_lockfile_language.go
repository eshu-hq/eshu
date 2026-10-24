// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	nodelockfile "github.com/eshu-hq/eshu/go/internal/parser/nodelockfile"
	"github.com/eshu-hq/eshu/go/internal/parser/npmrc"
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

// parseNpmrc adapts the npmrc package into the engine contract. It lives with
// the other npm-ecosystem adapter. The package emits credential-free
// registry-URL rows only and ignores IndexSource, so the engine never echoes
// an .npmrc body into parser output.
func parseNpmrc(path string, isDependency bool, options Options) (map[string]any, error) {
	return npmrc.Parse(path, isDependency, shared.Options{
		IndexSource:   options.IndexSource,
		VariableScope: options.VariableScope,
	})
}
