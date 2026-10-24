// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package wrapper reads the Gradle version a project pins in
// gradle/wrapper/gradle-wrapper.properties.
//
// The parser reads one property, distributionUrl, and keeps one thing from it:
// the version in the archive name (gradle-<version>-bin.zip or -all.zip). The
// URL itself, its host, any user:password@ userinfo, and every other property
// value are dropped and never reach the payload, not even when the caller asks
// for source indexing. See README.md for the grammar and bounds.
package wrapper
