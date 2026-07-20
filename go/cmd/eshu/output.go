// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

// printTable renders a simple table to stdout.
func printTable(headers []string, rows [][]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, strings.Join(headers, "\t"))
	_, _ = fmt.Fprintln(w, strings.Repeat("-", 40))
	for _, row := range rows {
		_, _ = fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	_ = w.Flush()
}

// printJSON renders any value as formatted JSON to stdout.
func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// printError prints a styled error message.
func printError(msg string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
}

// printSuccess prints a success message.
func printSuccess(msg string) {
	fmt.Printf("OK %s\n", msg)
}

// printWarning prints a non-fatal warning to stderr. Unlike printError, a
// warning does not indicate command failure -- `eshu mcp setup`'s
// auto-detected posture fallback uses this to tell the operator detection
// could not be positively confirmed while still emitting a working config.
func printWarning(msg string) {
	fmt.Fprintf(os.Stderr, "warning: %s\n", msg)
}
