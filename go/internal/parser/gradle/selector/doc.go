// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package selector holds the one rule every Gradle reader uses to decide
// whether declared version text names a single exact version.
//
// Gradle reads much version text as a selector, not a version: the prefix
// form "1.+", the status form "latest.release", a Maven-style range
// "[1.0,2.0)", the strict short-hand "1.0!!", and the artifact-only suffix
// "1.0@jar". A reader that marks such text resolved hands a selector to the
// reducer as if it were the installed version, and package matching and
// security-alert version comparison then compare against "1.+". Exact fails
// closed so that cannot happen. The build-script parser (package gradle) and
// the version-catalog parser share this rule and the table in
// testdata/versions.txt.
//
// The package imports only the Go standard library and emits no telemetry.
package selector
