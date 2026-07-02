// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

func loadRepoDependencyRetractStatementTiming(getenv func(string) string) bool {
	return loadBoolOrDefault(getenv, repoDependencyRetractStatementTimingEnv, false)
}
