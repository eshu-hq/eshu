// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "fmt"

// ensureFullHistory makes every commit reachable from the base readable, which
// the absolute invariant needs: W sums the waivers over the whole history. CI
// checks the merge ref out shallow (depth 2). A complete checkout returns at
// once. A shallow one is unshallowed with one blobless fetch from origin; only
// commits and trees arrive, so the cost is small. There is no commit-count limit.
// The only failure is the fetch itself failing, which is never read as a pass.
func ensureFullHistory(g gitRepo) error {
	shallow, err := g.line("rev-parse", "--is-shallow-repository")
	if err != nil {
		return fmt.Errorf("check for a shallow checkout: %w", err)
	}
	if shallow != "true" {
		return nil
	}
	if _, err := g.output("fetch", "--no-tags", "--quiet", "--filter=blob:none", "--unshallow", "origin"); err != nil {
		return fmt.Errorf("unshallow the checkout (git fetch --filter=blob:none --unshallow origin) to read waiver trailers: %w", err)
	}
	return nil
}
