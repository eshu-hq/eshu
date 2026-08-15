// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package localsupervisor

import "time"

// The child-service shutdown budget, and the environment variables and mode
// values the supervisor and its CLI wrapper both name. `eshu graph start` reads
// its --progress and --logs flags into these variables before re-execing into
// the owner; the owner and the child supervisor read them back out.
const (
	// ShutdownTimeout bounds how long a child service gets to exit after the
	// supervisor interrupts it, before it is killed.
	ShutdownTimeout = 5 * time.Second

	// deferContentSearchIndexesEnv, when truthy, keeps the Postgres content
	// search indexes out of the bootstrap so a deferred maintainer can build
	// them after the first drain instead of holding start-up up.
	deferContentSearchIndexesEnv = "ESHU_LOCAL_AUTHORITATIVE_DEFER_CONTENT_SEARCH_INDEXES"

	// reducerExpectedSourceLocalProjectorsEnv tells the reducer child how many
	// local projectors have to report before source-local work counts as
	// drained. The owner sets it from the workspace's repo count.
	reducerExpectedSourceLocalProjectorsEnv = "ESHU_REDUCER_EXPECTED_SOURCE_LOCAL_PROJECTORS"

	// ProgressModeEnv selects the owner's own progress display. The owner reads
	// it from its process environment; unset, or set to anything unrecognised,
	// is read as ProgressModeAuto.
	ProgressModeEnv = "ESHU_LOCAL_PROGRESS_MODE"

	// LogModeEnv selects where a supervised child's stdout and stderr go. It is
	// read out of the environment built for that child rather than the owner's
	// own, which is how one owner logs its MCP server to the terminal and every
	// other child to a file. Unset is read as LogModeFile.
	LogModeEnv = "ESHU_LOCAL_LOG_MODE"

	// LogDirEnv is the directory the per-child <service>.log files are appended
	// to. It is required under LogModeFile: starting a child without it is an
	// error rather than a silent fallback. ChildOverrides fills it in with the
	// workspace log directory unless the operator already set it.
	LogDirEnv = "ESHU_LOCAL_LOG_DIR"

	// ProgressModeAuto renders the full-screen progress TUI when the progress
	// writer is a terminal, and plain snapshots when it is not. It is the
	// default, and the fallback for an unrecognised value.
	ProgressModeAuto = "auto"

	// ProgressModePlain writes plain-text snapshots and never the TUI, even on a
	// terminal — the mode for a piped or captured run.
	ProgressModePlain = "plain"

	// ProgressModeQuiet stops the owner starting the progress reporter at all,
	// so nothing is rendered and nothing polls Postgres for status. That is a
	// stronger thing than quiet is for logs, where the children still run and
	// only their output is dropped.
	ProgressModeQuiet = "quiet"

	// LogModeFile appends a child's stdout and stderr to
	// <LogDirEnv>/<service>.log. It is the default.
	LogModeFile = "file"

	// LogModeTerminal hands the child the owner's own stdin, stdout, and stderr.
	// Under `--logs terminal` that is a logging choice; for the MCP stdio child
	// it is load-bearing, because inheriting the owner's stdio is how the MCP
	// transport reaches the client.
	LogModeTerminal = "terminal"

	// logModeQuiet discards a child's stdout and stderr. The child still runs
	// and still writes; only its output is thrown away. It stays unexported
	// because nothing outside this package names it: `eshu graph start
	// --logs quiet` passes the operator's own string, and ValidateLogMode
	// checks it against this constant.
	logModeQuiet = "quiet"
)
