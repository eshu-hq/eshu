// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

type localGateCommand struct {
	label   string
	command string
}

// executeGates runs all selected gates, accumulates results, and returns an
// error if any blocking gate failed. Advisory failures are printed but do not
// affect the exit code.
func executeGates(w io.Writer, sels []cigates.Selection, repoRoot string) error {
	anyBlockingFail := false
	for _, selection := range sels {
		if selection.Gate.CIOnlyReason != "" {
			_, _ = fmt.Fprintf(w, "CI-ONLY  %s: %s\n", selection.Gate.ID, selection.Gate.CIOnlyReason)
			continue
		}
		if !selection.Selected {
			_, _ = fmt.Fprintf(w, "SKIP     %s: %s\n", selection.Gate.ID, selection.Reason)
			continue
		}

		gateFailed := false
		for _, localCommand := range localGateCommands(selection.Gate.Local) {
			action := "RUN"
			if localCommand.label == "test_command" {
				action = "TEST"
			}
			_, _ = fmt.Fprintf(w, "%-8s %s: %s\n", action, selection.Gate.ID, localCommand.command)
			if err := runShellCommand(localCommand.command, repoRoot); err != nil {
				gateFailed = true
				printGateFailure(w, selection.Gate, localCommand.label, err)
				if selection.Gate.Blocking {
					anyBlockingFail = true
				}
			}
		}
		if !gateFailed {
			_, _ = fmt.Fprintf(w, "PASS     %s\n", selection.Gate.ID)
		}
	}
	if anyBlockingFail {
		return fmt.Errorf("one or more blocking gates failed")
	}
	return nil
}

// localGateCommands returns the shell commands this gate actually runs, in
// order. local.Command is intentionally OMITTED when it is the empty string,
// rather than run as an empty shell command -- a permanently local-only gate
// whose enforcement cannot be a command at all (prepr-stamp-verify-selftest:
// its guard reads the stamp of the commit about to be pushed, so running it
// here would fail every time) leaves local.command blank on purpose. An
// empty shell command always succeeds and used to print a "RUN <gate>: "
// line with nothing after the colon -- a reporting false-green: it read
// exactly like every other command line in the log but never ran anything
// (#6149 follow-up item 8 review, "verify before push").
func localGateCommands(local *cigates.Local) []localGateCommand {
	var commands []localGateCommand
	if local.Command != "" {
		commands = append(commands, localGateCommand{label: "command", command: local.Command})
	}
	if local.TestCommand != "" && local.TestCommand != local.Command {
		commands = append(commands, localGateCommand{
			label:   "test_command",
			command: local.TestCommand,
		})
	}
	return commands
}

func printGateFailure(w io.Writer, gate cigates.Gate, label string, err error) {
	disposition := "advisory"
	if gate.Blocking {
		disposition = "blocking"
	}
	if label == "test_command" {
		disposition += " test_command"
	}
	_, _ = fmt.Fprintf(w, "FAIL     %s (%s): %v\n", gate.ID, disposition, err)
}

// runShellCommand executes a shell command string via /bin/sh -c from repoRoot
// (the registry's commands are repo-root-relative) and returns any non-zero exit
// as an error.
//
// Gate commands that shell out to "bash scripts/verify-*.sh" get PATH
// steered toward a bash >= 4.4 when one is available, so the inner "bash"
// token does not silently resolve to macOS's bash 3.2. See
// resolveBash44Dir's doc comment for the full rationale (#5050).
func runShellCommand(command, repoRoot string) error {
	cmd := exec.Command("/bin/sh", "-c", command) // #nosec G204 -- command comes from the operator-controlled gate registry
	cmd.Dir = repoRoot
	cmd.Env = gateSubprocessEnv()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
