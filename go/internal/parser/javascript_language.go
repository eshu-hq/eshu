package parser

import (
	jsparser "github.com/eshu-hq/eshu/go/internal/parser/javascript"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func (e *Engine) parseJavaScriptLike(
	repoRoot string,
	path string,
	runtimeLanguage string,
	outputLanguage string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	return jsparser.Parse(e.runtime.Parser, e.runtime.PutParser, repoRoot, path, runtimeLanguage, outputLanguage, isDependency, shared.Options{
		IndexSource:   options.IndexSource,
		VariableScope: options.VariableScope,
		RepositoryID:  options.RepositoryID,
		EmitDataflow:  options.EmitDataflow,
	})
}

func (e *Engine) preScanJavaScriptLike(
	repoRoot string,
	path string,
	runtimeLanguage string,
	outputLanguage string,
) ([]string, error) {
	return jsparser.PreScan(e.runtime.Parser, e.runtime.PutParser, repoRoot, path, runtimeLanguage, outputLanguage)
}
