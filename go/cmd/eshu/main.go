// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		var coded interface{ ExitCode() int }
		if errors.As(err, &coded) {
			os.Exit(coded.ExitCode())
		}
		os.Exit(1)
	}
}
