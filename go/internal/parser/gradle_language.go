// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	gradleparser "github.com/eshu-hq/eshu/go/internal/parser/gradle"
	sbtparser "github.com/eshu-hq/eshu/go/internal/parser/sbt"
)

// parseGradle returns the Gradle build script parser payload for one
// repository manifest (build.gradle Groovy DSL or build.gradle.kts Kotlin
// DSL). Gradle execution and source-set resolution stay out of the parser;
// unresolved version interpolations surface as partial/unresolved evidence
// so the supply-chain reducer never sees fabricated versions.
func (e *Engine) parseGradle(path string, isDependency bool, options Options) (map[string]any, error) {
	return gradleparser.Parse(path, isDependency, sharedOptions(options))
}

// parseSbt returns the sbt build definition parser payload for one build.sbt.
// It lives beside the Gradle wrapper because both are JVM build manifests that
// emit the same dependency row contract. sbt is a Scala DSL, so the parser
// reads literal string forms only and reports non-literal declarations as
// bounded counts instead of evaluating Scala or resolving settings across
// files. repoRoot decides the meta-build flag from the path relative to the
// repository.
func (e *Engine) parseSbt(repoRoot, path string, isDependency bool, options Options) (map[string]any, error) {
	return sbtparser.Parse(repoRoot, path, isDependency, sharedOptions(options))
}
