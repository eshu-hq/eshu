// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import "strings"

// FallbackString resolves a value that has both a command-line flag and an
// environment variable. A flag the operator passed always wins, even when
// empty. Otherwise a non-blank environment value wins over the flag's own
// default, which is what flagValue holds when the flag was not passed. The
// caller reads the process environment and the flag state; this function only
// orders them.
func FallbackString(flagValue string, flagPassed bool, envValue string) string {
	if flagPassed {
		return flagValue
	}
	if trimmed := strings.TrimSpace(envValue); trimmed != "" {
		return trimmed
	}
	return flagValue
}

// FallbackList is FallbackString for a repeatable flag whose environment
// variable is comma separated. Blank entries in the variable are dropped, and
// a variable with no usable entry falls through to flagValue.
func FallbackList(flagValue []string, flagPassed bool, envValue string) []string {
	if flagPassed {
		return flagValue
	}
	var fromEnv []string
	for _, field := range strings.Split(envValue, ",") {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			fromEnv = append(fromEnv, trimmed)
		}
	}
	if len(fromEnv) > 0 {
		return fromEnv
	}
	return flagValue
}
