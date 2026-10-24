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
	if fromEnv := splitEnvList(envValue); len(fromEnv) > 0 {
		return fromEnv
	}
	return flagValue
}

// UnionList combines a repeatable flag with its comma separated environment
// variable for a deny list. Unlike FallbackList, a passed flag never replaces
// the environment: a revoke list that a flag could empty would let a shell
// that carries the service revocations judge a component the services block.
// Flag entries come first, then environment entries the flag did not already
// name. Flag entries are trimmed like environment entries and blanks are
// dropped, because policy matching compares exactly and a padded entry would
// otherwise revoke nothing. The result never shares storage with flagValue.
func UnionList(flagValue []string, envValue string) []string {
	var merged []string
	seen := make(map[string]struct{}, len(flagValue))
	for _, raw := range flagValue {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}
		merged = append(merged, entry)
	}
	for _, entry := range splitEnvList(envValue) {
		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}
		merged = append(merged, entry)
	}
	return merged
}

// splitEnvList splits a comma separated variable, trims each entry, and drops
// blanks.
func splitEnvList(envValue string) []string {
	var entries []string
	for _, field := range strings.Split(envValue, ",") {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			entries = append(entries, trimmed)
		}
	}
	return entries
}
