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
