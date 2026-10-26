// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// requiredTimeout bounds the registry read; `go run` may compile ci-gates.
const requiredTimeout = 5 * time.Minute

// resolveRequired returns the merge-rule check contexts GREEN must see
// succeed. It asks the gate registry through ci-gates, once, so the list
// cannot drift from the live ruleset; override (a comma-separated list) skips
// the registry. An unreadable registry or an empty result is an error: a GREEN
// with nothing required is a false green.
func resolveRequired(ctx context.Context, exec Exec, goBin, dir string, environ []string, override string) ([]string, error) {
	if strings.TrimSpace(override) != "" {
		return splitList(override, ","), nil
	}
	ctx, cancel := context.WithTimeout(ctx, requiredTimeout)
	defer cancel()
	out, err := exec(ctx, Command{
		Name: goBin,
		Args: []string{"-C", "go", "run", "./cmd/ci-gates", "contexts", "--registry", "../specs/ci-gates.v1.yaml", "--ruleset-only"},
		Dir:  dir,
		Env:  withoutEnv(environ, "GOROOT"),
	})
	if err != nil {
		return nil, fmt.Errorf("read required contexts from the gate registry (is -dir the repository root?): %w", err)
	}
	list := splitList(string(out), "\n")
	if len(list) == 0 {
		return nil, fmt.Errorf("the gate registry lists no ruleset-required contexts")
	}
	return list, nil
}

// splitList splits on sep, trims, and drops empty and duplicate entries while
// keeping order.
func splitList(s, sep string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range strings.Split(s, sep) {
		item = strings.TrimSpace(item)
		if item != "" && !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

// withoutEnv returns environ minus the named variable. GOROOT is dropped
// because a leaked value from a switched toolchain breaks `go run`.
func withoutEnv(environ []string, name string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		if !strings.HasPrefix(kv, name+"=") {
			out = append(out, kv)
		}
	}
	return out
}
