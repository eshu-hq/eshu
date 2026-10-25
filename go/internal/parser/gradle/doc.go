// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gradle extracts repository-side Gradle build-script dependency
// evidence for the supply-chain impact reducer.
//
// Parse decodes a single build.gradle (Groovy DSL) or build.gradle.kts
// (Kotlin DSL) file and emits one content_entity-shaped "variables" row per
// declared dependency it can prove from source text alone. Each row records
// the Maven coordinate "groupId:artifactId", the configuration name
// (implementation, api, runtimeOnly, compileOnly, testImplementation, ...),
// platform/enforcedPlatform BOM wrappers, and an explicit
// dependency_resolution_state of "resolved", "partial" (no version), or
// "unresolved" (a Groovy/Kotlin interpolation whose value the parser cannot
// satisfy from the same file's `def`, `val`, or `ext { }` declarations, or a
// Gradle version selector such as "1.+", "latest.release", "[1.0,2.0)",
// "1.0!!" or "1.0@jar" that names no single version; see package selector).
//
// The parser never executes Gradle, evaluates Groovy or Kotlin code, runs
// source-set resolution, or reads settings.gradle / sibling modules.
// `project(":x")`, `files(...)`, `fileTree(...)`, and Gradle-internal helpers
// are skipped so they cannot masquerade as Maven coordinates.
package gradle
