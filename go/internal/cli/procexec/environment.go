// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package procexec

// MergeEnvironment layers overrides onto a name=value environment slice and
// returns a new slice; base is not modified. An entry is split on its FIRST
// '=', so a value may contain further '=' characters. An entry with no '=' at
// all carries no name and is dropped. When base repeats a name the last
// occurrence wins, matching how a process reads its own environment. An
// override to the empty string is a real assignment, not a deletion: the name
// survives with an empty value.
//
// The result is built from a map, so entry order is unspecified and differs
// between calls. Callers hand the result to exec, which does not care about
// order; a caller that does care must sort it.
func MergeEnvironment(base []string, overrides map[string]string) []string {
	merged := make(map[string]string, len(base)+len(overrides))
	for _, entry := range base {
		for i := 0; i < len(entry); i++ {
			if entry[i] == '=' {
				merged[entry[:i]] = entry[i+1:]
				break
			}
		}
	}
	for key, value := range overrides {
		merged[key] = value
	}
	env := make([]string, 0, len(merged))
	for key, value := range merged {
		env = append(env, key+"="+value)
	}
	return env
}
