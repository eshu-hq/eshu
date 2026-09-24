// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fake

import (
	"fmt"
	"regexp"
	"strconv"
)

var placeholderPattern = regexp.MustCompile(`\$(\d+)`)

// CheckPlaceholders reports whether a query's positional placeholders
// ($1..$N) are dense and match argCount, the number of arguments the store
// passed with it. It returns an error naming the first mismatch: the highest
// placeholder differs from argCount, or a placeholder below the highest is
// never used. Store tests call it on the Query and Args an ExecQueryer
// recorded, so a statement and its argument list cannot drift apart.
func CheckPlaceholders(query string, argCount int) error {
	matches := placeholderPattern.FindAllStringSubmatch(query, -1)
	seen := make(map[int]bool, len(matches))
	maxPlaceholder := 0
	for _, match := range matches {
		placeholder, err := strconv.Atoi(match[1])
		if err != nil {
			return fmt.Errorf("parse placeholder %q: %w", match[0], err)
		}
		seen[placeholder] = true
		if placeholder > maxPlaceholder {
			maxPlaceholder = placeholder
		}
	}
	if maxPlaceholder != argCount {
		return fmt.Errorf("query max placeholder = $%d, args = %d:\n%s", maxPlaceholder, argCount, query)
	}
	for placeholder := 1; placeholder <= maxPlaceholder; placeholder++ {
		if !seen[placeholder] {
			return fmt.Errorf("query skips placeholder $%d:\n%s", placeholder, query)
		}
	}
	return nil
}
