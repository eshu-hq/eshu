// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gradle parses Gradle build scripts (build.gradle Groovy DSL and
// build.gradle.kts Kotlin DSL) into the parent parser payload contract so the
// supply-chain impact reducer can correlate repository-declared Gradle
// dependencies to package-registry identity.
//
// The parser never executes Gradle, evaluates Groovy/Kotlin, runs source-set
// resolution, or reads sibling settings.gradle files. It records the
// dependency truth provable from the file alone and preserves unresolved
// version interpolations as unresolved evidence rather than guessing.
package gradle

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// Parse decodes a build.gradle or build.gradle.kts file and returns the
// parent parser payload with content_entity-shaped "variables" rows for each
// declared dependency the parser can prove from source text alone.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	payload := shared.BasePayload(path, "gradle", isDependency)
	text := string(source)
	stripped := stripCommentsAndStringInteriorsKept(text)
	properties := collectScalarProperties(stripped)
	blocks := extractDependencyBlocks(stripped)

	rows := make([]map[string]any, 0)
	lineNumber := 1
	for _, block := range blocks {
		for _, statement := range splitDependencyStatements(block.body) {
			row, ok := parseDependencyStatement(statement, block.section, properties, lineNumber)
			if !ok {
				continue
			}
			rows = append(rows, row)
			lineNumber++
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left, _ := rows[i]["name"].(string)
		right, _ := rows[j]["name"].(string)
		return left < right
	})
	payload["variables"] = rows

	if options.IndexSource {
		payload["source"] = text
	}
	return payload, nil
}
