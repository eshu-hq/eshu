// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	groovyparser "github.com/eshu-hq/eshu/go/internal/parser/groovy"
)

func (e *Engine) parseGroovy(path string, isDependency bool, options Options) (map[string]any, error) {
	parser, err := e.runtime.Parser("groovy")
	if err != nil {
		return nil, err
	}
	defer e.runtime.PutParser("groovy", parser)

	return groovyparser.ParseWithParser(path, isDependency, sharedOptions(options), parser)
}

func (e *Engine) preScanGroovy(path string) ([]string, error) {
	parser, err := e.runtime.Parser("groovy")
	if err != nil {
		return nil, err
	}
	defer e.runtime.PutParser("groovy", parser)

	return groovyparser.PreScanWithParser(path, parser)
}

// ExtractGroovyPipelineMetadata returns the explicit Jenkins/Groovy signals
// that the parser can safely prove from source text.
func ExtractGroovyPipelineMetadata(sourceText string) map[string]any {
	return extractGroovyPipelineMetadata(sourceText)
}

func extractGroovyPipelineMetadata(sourceText string) map[string]any {
	return groovyparser.PipelineMetadata(sourceText).Map()
}
