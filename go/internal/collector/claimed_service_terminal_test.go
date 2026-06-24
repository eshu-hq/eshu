// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

type terminalCollectFailure struct {
	err error
}

func (e terminalCollectFailure) Error() string { return e.err.Error() }

func (e terminalCollectFailure) Unwrap() error { return e.err }

func (e terminalCollectFailure) FailureClass() string { return "non_retryable" }

func (e terminalCollectFailure) TerminalFailure() bool { return true }
