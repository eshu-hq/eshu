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

type sharedGateCommandKey struct {
	workflow string
	job      string
	command  string
}

type sharedGateCommandResult struct {
	gateID string
	err    error
}

// executeGates runs all selected gates, accumulates results, and returns an
// error if any blocking gate failed. Advisory failures are printed but do not
// affect the exit code.
func executeGates(w io.Writer, sels []cigates.Selection, repoRoot string) error {
	anyBlockingFail := false
	sharedResults := make(map[sharedGateCommandKey]sharedGateCommandResult)
	for _, selection := range sels {
		if selection.Gate.CIOnlyReason != "" {
			_, _ = fmt.Fprintf(w, "CI-ONLY  %s: %s\n", selection.Gate.ID, selection.Gate.CIOnlyReason)
			continue
		}
		if !selection.Selected {
			_, _ = fmt.Fprintf(w, "SKIP     %s: %s\n", selection.Gate.ID, selection.Reason)
			continue
		}

		commands := localGateCommands(selection.Gate.Local)
		if len(commands) == 0 {
			// Load (go/internal/cigates/registry.go) now rejects this shape
			// at registry-load time -- a local block with neither command
			// nor test_command. This guards the other entry point: a Gate
			// constructed directly (a future caller, a test double) skips
			// Load's validation entirely. Without this, the loop below runs
			// zero times, gateFailed never becomes true by initialization,
			// and "PASS <gate>" prints for a gate that executed nothing --
			// indistinguishable from one that genuinely ran and passed
			// (#6149 follow-up item 8 review, P1).
			_, _ = fmt.Fprintf(w, "SKIP     %s: gate declares no runnable local command\n", selection.Gate.ID)
			continue
		}

		gateFailed := false
		for _, localCommand := range commands {
			key, canReuse := sharedCommandKey(selection.Gate, localCommand.command)
			result, reused := sharedResults[key]
			if canReuse && reused {
				_, _ = fmt.Fprintf(
					w,
					"REUSE   %s: %s (result from %s)\n",
					selection.Gate.ID,
					localCommand.command,
					result.gateID,
				)
			} else {
				action := "RUN"
				if localCommand.label == "test_command" {
					action = "TEST"
				}
				_, _ = fmt.Fprintf(w, "%-8s %s: %s\n", action, selection.Gate.ID, localCommand.command)
				result = sharedGateCommandResult{
					gateID: selection.Gate.ID,
					err:    runShellCommand(localCommand.command, repoRoot),
				}
				if canReuse {
					sharedResults[key] = result
				}
			}
			if result.err != nil {
				gateFailed = true
				printGateFailure(w, selection.Gate, localCommand.label, result.err)
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

// sharedCommandKey mirrors RequiredGates' hosted workflow/job deduplication.
// Commands without a complete hosted owner stay independent because byte
// equality alone is not enough evidence that two registry rows are one check.
func sharedCommandKey(gate cigates.Gate, command string) (sharedGateCommandKey, bool) {
	if gate.CI.Workflow == "" || gate.CI.Job == "" {
		return sharedGateCommandKey{}, false
	}
	return sharedGateCommandKey{
		workflow: gate.CI.Workflow,
		job:      gate.CI.Job,
		command:  command,
	}, true
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
