// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"strings"

	gradleparser "github.com/eshu-hq/eshu/go/internal/parser/gradle"
	gradlecatalog "github.com/eshu-hq/eshu/go/internal/parser/gradle/catalog"
	gradlewrapper "github.com/eshu-hq/eshu/go/internal/parser/gradle/wrapper"
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

// parseGradleWrapper adapts the wrapper package into the engine contract. The
// package emits the Gradle version and the distribution type only, and it
// ignores IndexSource, so the engine never echoes a gradle-wrapper.properties
// body into parser output. It has its own entry point, not a branch of
// parseGradle, because the file is bound to the metadata content-store policy
// and must never reach the build-script parser, which echoes the body.
func (e *Engine) parseGradleWrapper(path string, isDependency bool, options Options) (map[string]any, error) {
	return gradlewrapper.Parse(path, isDependency, sharedOptions(options))
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
// contract: sbt definitions go to the sbt scanner, the gradle_wrapper
// definition to the wrapper reader, and every other admitted file to the Gradle
// parser. The wrapper is chosen by definition language, never by file name, so
// no name can reach the build-script parser through it.
func (e *Engine) parseJVMBuild(language, repoRoot, path string, isDependency bool, options Options) (map[string]any, error) {
	switch language {
	case "sbt_properties":
		return sbtparser.ParseBuildProperties(path, isDependency)
	case "sbt":
		return e.parseSbt(repoRoot, path, isDependency, options)
	case "gradle_wrapper":
		return e.parseGradleWrapper(path, isDependency, options)
	}
	return e.parseGradle(path, isDependency, options)
}
