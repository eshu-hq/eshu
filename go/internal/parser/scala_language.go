// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"slices"

	scalaparser "github.com/eshu-hq/eshu/go/internal/parser/scala"
)

func (e *Engine) parseScala(
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	parser, err := e.runtime.Parser("scala")
	if err != nil {
		return nil, err
	}
	defer e.runtime.PutParser("scala", parser)

	return scalaparser.Parse(path, isDependency, sharedOptions(options), parser)
}

func (e *Engine) preScanScala(path string) ([]string, error) {
	parser, err := e.runtime.Parser("scala")
	if err != nil {
		return nil, err
	}
	defer e.runtime.PutParser("scala", parser)

	names, err := scalaparser.PreScan(path, parser)
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	return names, nil
}
