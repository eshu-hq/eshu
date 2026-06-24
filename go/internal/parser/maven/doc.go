// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package maven extracts repository-side Maven (pom.xml) dependency evidence
// for the supply-chain impact reducer.
//
// Parse decodes a single pom.xml using encoding/xml and emits one
// content_entity-shaped "variables" row per <dependency>, including entries
// under <dependencyManagement> and per-profile <dependencies>. Each row
// records the Maven coordinate as "groupId:artifactId", the declared scope
// (compile, test, runtime, provided, system, import), an optional flag, and
// a dependency_resolution_state of "resolved", "partial" (missing version),
// or "unresolved" (property reference the parser cannot satisfy from the
// file's own <properties>).
//
// The parser never executes Maven, resolves parent POMs across files, or
// performs network lookups. Multi-module repositories produce one parse per
// pom.xml; per-file truth never invents resolved values it cannot prove from
// source.
package maven
