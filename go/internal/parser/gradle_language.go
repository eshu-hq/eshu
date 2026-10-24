// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"strings"

	gradleparser "github.com/eshu-hq/eshu/go/internal/parser/gradle"
	gradlecatalog "github.com/eshu-hq/eshu/go/internal/parser/gradle/catalog"
	sbtparser "github.com/eshu-hq/eshu/go/internal/parser/sbt"
)

// parseGradle returns the Gradle parser payload for one repository file. A
// build script (build.gradle Groovy DSL or build.gradle.kts Kotlin DSL) goes
// to the build-script parser; a version catalog (libs.versions.toml) goes to
// the catalog parser. Gradle execution and source-set resolution stay out of
// both; unresolved version interpolations surface as partial/unresolved
// evidence so the supply-chain reducer never sees fabricated versions.
func (e *Engine) parseGradle(path string, isDependency bool, options Options) (map[string]any, error) {
	if strings.EqualFold(filepath.Base(path), "libs.versions.toml") {
		return gradlecatalog.Parse(path, isDependency, sharedOptions(options))
	}
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

// parseJVMBuild routes the JVM build manifests that share one dependency row
// contract: sbt definitions go to the sbt scanner, every other admitted file to
// the Gradle parser.
func (e *Engine) parseJVMBuild(language, repoRoot, path string, isDependency bool, options Options) (map[string]any, error) {
	if language == "sbt" {
		return e.parseSbt(repoRoot, path, isDependency, options)
	}
	return e.parseGradle(path, isDependency, options)
}
