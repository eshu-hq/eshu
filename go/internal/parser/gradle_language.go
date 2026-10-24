// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"strings"

	gradleparser "github.com/eshu-hq/eshu/go/internal/parser/gradle"
	gradlecatalog "github.com/eshu-hq/eshu/go/internal/parser/gradle/catalog"
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
