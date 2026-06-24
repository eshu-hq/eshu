// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	gradleparser "github.com/eshu-hq/eshu/go/internal/parser/gradle"
)

// parseGradle returns the Gradle build script parser payload for one
// repository manifest (build.gradle Groovy DSL or build.gradle.kts Kotlin
// DSL). Gradle execution and source-set resolution stay out of the parser;
// unresolved version interpolations surface as partial/unresolved evidence
// so the supply-chain reducer never sees fabricated versions.
func (e *Engine) parseGradle(path string, isDependency bool, options Options) (map[string]any, error) {
	return gradleparser.Parse(path, isDependency, sharedOptions(options))
}
