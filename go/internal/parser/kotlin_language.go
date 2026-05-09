package parser

import kotlinparser "github.com/eshu-hq/eshu/go/internal/parser/kotlin"

func (e *Engine) parseKotlin(repoRoot string, path string, isDependency bool, options Options) (map[string]any, error) {
	return kotlinparser.Parse(repoRoot, path, isDependency, sharedOptions(options))
}

func (e *Engine) preScanKotlin(repoRoot string, path string) ([]string, error) {
	return kotlinparser.PreScan(repoRoot, path)
}
