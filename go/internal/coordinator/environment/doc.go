// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package environment parses typed configuration values out of environment
// variable strings.
//
// Bool, Int, and Duration each take a getenv function and a key, returning a
// caller-supplied fallback for a missing or blank value and an error naming
// the key for an unparseable one. The coordinator root's LoadConfig and
// coordinator/semantic's worker configuration both call these functions.
package environment
