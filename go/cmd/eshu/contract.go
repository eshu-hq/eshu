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

// The bool envelope reader (traceBool) lived here until the component family
// moved to go/internal/cli/component (#6139) and took its last caller in this
// package with it -- the change families' callers had already left with
// #6126. Its three surviving copies are boolValue in
// go/internal/cli/change/envelope.go, go/internal/cli/freshness/values.go,
// and go/internal/cli/component/values.go; TestEnvelopeReaderParity pins
// those three to each other. It pins the other readers the same way, across
// the internal/cli copies that declare them. None of them is declared in
// trace.go any more: the trace extraction (#6059) removed the last of the
// originals from this package.
