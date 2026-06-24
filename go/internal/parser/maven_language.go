// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	mavenparser "github.com/eshu-hq/eshu/go/internal/parser/maven"
)

// parseMaven returns the Maven (pom.xml) parser payload for one repository
// manifest. Maven build execution and parent POM resolution stay out of the
// parser; unresolved property references surface as partial/unresolved
// evidence so the supply-chain reducer never sees fabricated versions.
func (e *Engine) parseMaven(path string, isDependency bool, options Options) (map[string]any, error) {
	return mavenparser.Parse(path, isDependency, sharedOptions(options))
}
