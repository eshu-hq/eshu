// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	dartparser "github.com/eshu-hq/eshu/go/internal/parser/dart"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func (e *Engine) parseDart(path string, isDependency bool, options Options) (map[string]any, error) {
	parser, err := e.runtime.Parser("dart")
	if err != nil {
		return nil, err
	}
	defer e.runtime.PutParser("dart", parser)

	return dartparser.ParseWithParser(path, isDependency, shared.Options{
		IndexSource:   options.IndexSource,
		VariableScope: options.VariableScope,
	}, parser)
}

func (e *Engine) preScanDart(path string) ([]string, error) {
	parser, err := e.runtime.Parser("dart")
	if err != nil {
		return nil, err
	}
	defer e.runtime.PutParser("dart", parser)

	return dartparser.PreScanWithParser(path, parser)
}
