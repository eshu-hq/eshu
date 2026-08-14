// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "fmt"

type commandExitError struct {
	message string
	code    int
}

func (e commandExitError) Error() string {
	return e.message
}

func (e commandExitError) ExitCode() int {
	if e.code == 0 {
		return 1
	}
	return e.code
}

func removedCommandError(command, guidance string) error {
	printError(fmt.Sprintf("%q has been removed from the supported Go CLI contract.", command))
	if guidance != "" {
		fmt.Println(guidance)
	}
	return fmt.Errorf("%s removed from supported Go CLI contract", command)
}

// traceBool returns parent[key] as a bool, or false when the key is missing or
// holds another type.
//
// It is here rather than next to its sibling readers in trace.go for two
// reasons worth knowing before moving it again. It was declared in
// freshness.go until that command's logic moved to internal/cli/freshness
// (#6059), and it still has callers in three other command families --
// change_plan.go, change_impact.go, and component_api.go -- so it could not
// move out with the rest. trace.go is 490 lines against a 500-line cap and
// cannot take it, and cmd/eshu is pinned at its current non-test file count by
// the directory gate, so a new file is not an option either. contract.go holds
// the plumbing every command family shares, which makes it the one file here
// that no family extraction will claim.
func traceBool(parent map[string]any, key string) bool {
	if parent == nil {
		return false
	}
	if value, ok := parent[key].(bool); ok {
		return value
	}
	return false
}
