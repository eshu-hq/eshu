// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package change holds the logic behind `eshu change impact` and
// `eshu change plan`: deriving a changed-file set, checking the flags that
// describe it, building the two request bodies, and turning the API's answer
// into what an operator sees and what the process exits with.
//
// The work splits into four groups. GitDiffNameStatus, ParseNameStatusDiff,
// NormalizeStatus, ModifiedFiles, ChangedPaths, and CleanValues turn either a
// local git diff or an explicit --file list into []FileChange, preserving
// renames and copies as two-endpoint rows so the API can follow evidence
// across a move. Validate rejects the flag combinations the API would reject
// anyway. ImpactRequestBody and PlanRequestBody build the bodies for
// ImpactRoute and PlanRoute. FinishImpact and FinishPlan render the answer,
// either as the canonical envelope in JSON or as a short human summary.
//
// Failures come back as a Failure carrying a FailureKind -- KindInvalidArgument,
// KindEnvelope, KindFreshness, or KindIncomplete, which Kinds returns as a
// slice so the caller can prove it handled all of them -- and never as an exit
// code.
// The caller owns that mapping, and it is not a formality: the CLI's shared
// code-to-exit-code table answers 1 for a still-building index, while both
// change commands have always exited 4. ErrorCodeFromTransport classifies a
// failed HTTP call, reading the status through internal/cli/apierr because the
// concrete error type lives in go/cmd/eshu's package main and cannot be
// imported. Its message checks run before its status switch, so an error whose
// text names a refused connection classifies as backend_unavailable even when
// it also carries a status.
//
// Nothing here reads a cobra flag, the process environment, or os.Stdout, and
// nothing here opens a file. Every function takes plain values and an
// io.Writer the caller supplies. go/cmd/eshu/change_impact.go is the cobra
// wrapper that resolves that process state and maps a Failure to an exit code.
// The split is mechanical rather than a design preference: cmd/eshu is
// package main, so nothing can import it, and any symbol that reads cobra
// flags, the environment, or the exit-code contract has to stay there.
package change
