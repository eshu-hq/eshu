// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser //nolint:dirgate // per-language engine entrypoint beside the cabal sub-package, like gomod_language.go

import (
	cabalparser "github.com/eshu-hq/eshu/go/internal/parser/cabal"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// parseCabal dispatches .cabal parsing through the cabal sub-package.
// The cabal language is metadata-only: it never reaches the Haskell parser.
func (e *Engine) parseCabal(path string, isDependency bool, options Options) (map[string]any, error) {
	return cabalparser.Parse(path, isDependency, shared.Options{
		IndexSource:   options.IndexSource,
		VariableScope: options.VariableScope,
	})
}
